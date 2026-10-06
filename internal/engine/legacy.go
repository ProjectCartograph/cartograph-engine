package engine

import (
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/sentence"
)

// rewriteLegacyFields mutates a parsed manifest document in place for one
// release's worth of backward compatibility with names two earlier schemas
// retired: Goal's level value "team" (the first one's retired name, and its
// predecessor "functional", the second one's retired name) both become
// "strategic"; a Goal at "strategic" with no parent (the first one's root level,
// before the second split it into Pillar and Strategic) becomes "pillar"; and the
// KeyResult/KPI baseline and target date fields ("asOf"/"by", now plain
// "date" on both, since the parent object already names the grouping). The
// admitted-unknown baseline shape ({unknownReason, expectedBy}) is
// untouched: there the date means something different (when the baseline
// will be set), and neither earlier schema ever renamed it.
//
// Returns one human-readable note per field actually rewritten (empty when
// the document was already in the current shape), so ImportDir can print
// what changed, once per file.
// normalizeLegacyYAML applies rewriteLegacyFields to one stored manifest's
// bytes, returning the rewritten YAML when anything actually changed and
// the original bytes otherwise.
//
// The rewrite used to run only on import, which was enough when importing
// was how manifests arrived. A vault is a directory the person also edits
// by hand and syncs with whatever their organisation already uses, so a
// file in last release's shape can appear at any moment, and every reader
// (the interface, the checks, the charter) has to cope. The file on disk
// is left alone until the next save writes it back in the current shape.
func (e *Engine) normalizeLegacy(kind string, yamlBytes []byte) []byte {
	var doc map[string]any
	if err := e.codec.DecodeInto(yamlBytes, &doc); err != nil {
		return yamlBytes
	}
	if len(rewriteLegacyFields(kind, doc)) == 0 {
		return yamlBytes
	}
	out, err := e.codec.Encode(doc)
	if err != nil {
		return yamlBytes
	}
	return out
}

func rewriteLegacyFields(kind string, doc map[string]any) []string {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil
	}

	var notes []string

	// The name is metadata.name, as Kubernetes keeps it. spec.name held it a
	// second time, and the two drifted when only one was edited; it is
	// folded in, the metadata's winning where both are set (2.6.0).
	if n, ok := spec["name"].(string); ok {
		meta, _ := doc["metadata"].(map[string]any)
		if meta == nil {
			meta = map[string]any{}
			doc["metadata"] = meta
		}
		if m, _ := meta["name"].(string); m == "" && n != "" {
			meta["name"] = n
		}
		delete(spec, "name")
		notes = append(notes, "spec.name folded into metadata.name, which now holds the name alone")
	}

	// A gap may be watched in several data sources, and an indicator read
	// from several jointly; the single fields are folded into the lists
	// (2.7.0, ADR 0021).
	for _, f := range []struct{ kind, old, list string }{{"Gap", "measuredBy", "dataSources"}, {"KPI", "source", "sources"}} {
		if kind != f.kind {
			continue
		}
		if one, ok := spec[f.old].(string); ok {
			list, _ := spec[f.list].([]any)
			found := false
			for _, v := range list {
				if v == one {
					found = true
				}
			}
			if !found && one != "" {
				list = append([]any{one}, list...)
			}
			spec[f.list] = list
			delete(spec, f.old)
			notes = append(notes, "spec."+f.old+" folded into spec."+f.list)
		}
	}

	if kind == "Goal" {
		if level, ok := spec["level"].(string); ok {
			switch level {
			case "team":
				// The older "team" level always becomes "strategic"
				spec["level"] = "strategic"
				notes = append(notes, `spec.level: "team" rewritten to "strategic"`)
			case "functional":
				// The older "functional" (now called "strategic") becomes "strategic",
				// but only if it has no parent or its parent is a "pillar" (or has no parent in old files).
				// The current "functional" (child of strategic) should not be rewritten.
				// Heuristic: if functional has a parent that looks like it could be strategic (not a pillar
				// root and not missing), assume it's new format and keep it as functional.
				// If it has no parent, it's old format and should become strategic.
				// In old format, a functional with parent would be impossible, so any functional
				// with parent is new format.
				if _, hasParent := spec["parent"]; !hasParent {
					// Old format: functional with no parent becomes strategic
					spec["level"] = "strategic"
					notes = append(notes, `spec.level: "functional" rewritten to "strategic"`)
				}
				// else: new format, keep as functional
			case "strategic":
				if _, hasParent := spec["parent"]; !hasParent {
					spec["level"] = "pillar"
					notes = append(notes, `spec.level: "strategic" (with no parent) rewritten to "pillar"`)
				}
			}
		}
		// The levels took their standard names on 2026-09-30 (TAXONOMY.md
		// D25): goal, objective, outcome. "functional" named an
		// organisation's scope, not a result, and "pillar" is a vault's
		// label for a goal, not a level of the hierarchy.
		if level, ok := spec["level"].(string); ok {
			if next, renamed := map[string]string{"pillar": "goal", "strategic": "objective", "functional": "outcome"}[level]; renamed {
				spec["level"] = next
				notes = append(notes, fmt.Sprintf("spec.level %q renamed to %q", level, next))
			}
		}
		if krs, ok := spec["keyResults"].([]any); ok {
			for i, kr := range krs {
				if m, ok := kr.(map[string]any); ok {
					notes = append(notes, rewriteBaselineTargetDate(fmt.Sprintf("spec.keyResults[%d]", i), m)...)
				}
			}
		}
	}

	if kind == "KPI" {
		notes = append(notes, rewriteBaselineTargetDate("spec", spec)...)
	}

	if kind == "Programme" {
		notes = append(notes, splitStoredSentences(spec, "spec")...)
		notes = append(notes, splitStoredAim(spec)...)
		if summary, ok := spec["problems"].([]any); ok {
			notes = append(notes, assignIDs(summary, "spec.problems", []string{"situation"}, "problem-")...)
		}
	}

	if kind == "Project" {
		notes = append(notes, splitStoredSentences(spec, "spec.summary")...)
	}
	notes = append(notes, rewriteGapCitations(kind, spec)...)
	notes = append(notes, liftAssumptionRisks(spec)...)
	if kind == "KPI" {
		notes = append(notes, dropFreeTextDisaggregations(spec)...)
		notes = append(notes, rewriteFreeTextUnit(spec)...)
	}

	// Every kind, before anything reads a value: the passes below compare
	// against the current identifiers, so an old file has to be speaking
	// them by the time they run.
	notes = append(notes, renameEnums(kind, spec)...)

	if kind == "Project" {
		notes = append(notes, dropStakeholderRoles(spec)...)
		notes = append(notes, rewriteDeliverableAcceptance(spec)...)
		notes = append(notes, dropAccountableForSections(spec)...)
		// Order matters: the role references resolve against the ids the
		// line above it assigns.
		notes = append(notes, assignLocalIDs(spec)...)
		notes = append(notes, rewriteRoleRefs(spec)...)
		// Last of the three: both passes above still read the title a role
		// used to carry, one to seed its id and one to resolve the string
		// references that named it.
		notes = append(notes, dropRoleTitles(spec)...)
		notes = append(notes, liftStakeholderScores(spec)...)
		notes = append(notes, dropFreeTextFunding(spec)...)
	}

	return notes
}

// rewriteDeliverableAcceptance turns a deliverable's single acceptance
// sentence into the list of criteria it is now: acceptance used to be one
// string, which left no room to say who verifies each part, and a
// deliverable signed off by two different roles had nowhere to put the
// second one. The old sentence becomes one criterion with no verifier
// named, which is exactly what it always was.
func rewriteDeliverableAcceptance(spec map[string]any) []string {
	deliverables, ok := spec["deliverables"].([]any)
	if !ok {
		return nil
	}
	var notes []string
	for i, d := range deliverables {
		dm, ok := d.(map[string]any)
		if !ok {
			continue
		}
		text, ok := dm["acceptance"].(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(text) == "" {
			delete(dm, "acceptance")
			notes = append(notes, fmt.Sprintf("spec.deliverables[%d].acceptance: empty text dropped", i))
			continue
		}
		dm["acceptance"] = []any{map[string]any{"outcome": text}}
		notes = append(notes, fmt.Sprintf("spec.deliverables[%d].acceptance rewritten from one sentence to a list of criteria", i))
	}
	return notes
}

// dropAccountableForSections removes a resource role's accountableFor
// list. Cartograph used to ask which sections of the definition each
// accountable role answered for, which no charter template asks and which
// does not survive contact with how accountability actually works: a RACI
// names who is accountable for a deliverable or a decision, which is a
// planning-level judgement about work, not about the paragraphs of a
// document. Where it mattered, the deliverable's own acceptance criteria
// already name the role that verifies each one.
func dropAccountableForSections(spec map[string]any) []string {
	resources, ok := spec["resources"].([]any)
	if !ok {
		return nil
	}
	var notes []string
	for i, r := range resources {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if _, present := rm["accountableFor"]; !present {
			continue
		}
		delete(rm, "accountableFor")
		notes = append(notes, fmt.Sprintf("spec.resources[%d].accountableFor dropped: accountability is named per deliverable, not per section", i))
	}
	return notes
}

// rewriteBaselineTargetDate renames baseline.asOf and target.by to
// baseline.date and target.date on one {baseline, target} holder (a
// KeyResult or a KPI's own spec), in place.
func rewriteBaselineTargetDate(prefix string, holder map[string]any) []string {
	var notes []string
	if b, ok := holder["baseline"].(map[string]any); ok {
		if v, has := b["asOf"]; has {
			b["date"] = v
			delete(b, "asOf")
			notes = append(notes, fmt.Sprintf("%s.baseline.asOf rewritten to %s.baseline.date", prefix, prefix))
		}
	}
	if t, ok := holder["target"].(map[string]any); ok {
		if v, has := t["by"]; has {
			t["date"] = v
			delete(t, "by")
			notes = append(notes, fmt.Sprintf("%s.target.by rewritten to %s.target.date", prefix, prefix))
		}
	}
	return notes
}

// localIDLists names every list inside a Project whose items gained a
// stable id on 2026-09-28, with the field each item's id is derived from.
// Deliverables, success criteria and risks already carried ids and are not
// listed here.
var localIDLists = []struct {
	list, prefix string
	// The fields to seed an id from, in order. A role is seeded from the
	// catalogue entry it names since 2026-09-29, and from the free-text
	// title before it, so reading an old file still gives ids somebody can
	// recognise rather than role-1, role-2. An id is only ever assigned
	// once, so a manifest that already has ids keeps them.
	from []string
}{
	{list: "resources", prefix: "role-", from: []string{"resource", "title"}},
	{list: "objectives", prefix: "objective-", from: []string{"objective"}},
}

// assignLocalIDs back-fills the id on every list item that has none, so
// references have something stable to point at. The id is derived from the
// item's own text when that text is a name, because a reader opening the
// YAML should be able to tell what a role is without counting rows; a
// field holding a sentence, and a text two items slug the same way, fall
// back to a numbered id. Existing ids are never touched: an id that
// something already points at must not move.
func assignLocalIDs(spec map[string]any) []string {
	var notes []string
	for _, l := range localIDLists {
		if items, ok := spec[l.list].([]any); ok {
			notes = append(notes, assignIDs(items, "spec."+l.list, l.from, l.prefix)...)
		}
	}
	if tl, ok := spec["timeline"].(map[string]any); ok {
		if phases, ok := tl["phases"].([]any); ok {
			notes = append(notes, assignIDs(phases, "spec.timeline.phases", []string{"name"}, "phase-")...)
		}
	}
	if summary, ok := spec["summary"].(map[string]any); ok {
		if problems, ok := summary["problems"].([]any); ok {
			notes = append(notes, assignIDs(problems, "spec.summary.problems", []string{"situation"}, "problem-")...)
		}
	}
	return notes
}

// splitStoredSentences turns a problem or a change written as one
// sentence into the parts the manifest now holds.
//
// The sentence used to be the stored value and the parts were recovered
// by splitting it every time somebody opened the step, which put the
// subject in two places and got the split wrong twice in one day. The
// parts are the inputs now. The subject
// cannot be separated from the situation here, since that needs the
// group names and this pass sees one document; it stays where it was
// written, and composition leaves a clause that names its own subject
// alone.
func splitStoredSentences(spec map[string]any, path string) []string {
	var notes []string
	holder := spec
	if path == "spec.summary" {
		var ok bool
		if holder, ok = spec["summary"].(map[string]any); !ok {
			return nil
		}
	}
	problems, ok := holder["problems"].([]any)
	if !ok {
		return nil
	}
	for i, it := range problems {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := m["problem"].(string); ok {
			situation, cause := sentence.SplitProblem(text)
			parts := map[string]any{"situation": situation}
			if cause != "" {
				parts["cause"] = cause
			}
			m["problem"] = parts
			notes = append(notes, fmt.Sprintf("%s.problems[%d].problem split into the parts it was built from", path, i))
		}
		if text, ok := m["change"].(string); ok {
			what, gain := sentence.SplitChange(text)
			parts := map[string]any{"what": what}
			if gain != "" {
				parts["gain"] = gain
			}
			m["change"] = parts
			notes = append(notes, fmt.Sprintf("%s.problems[%d].change split into the parts it was built from", path, i))
		}
	}
	return notes
}

// splitStoredAim does the same for a programme's aim. An aim quoting a
// published objective has no second clause and becomes change alone,
// which composes back to exactly what was written.
func splitStoredAim(spec map[string]any) []string {
	text, ok := spec["aim"].(string)
	if !ok {
		return nil
	}
	change, gain := sentence.SplitAim(text)
	parts := map[string]any{"change": change}
	if gain != "" {
		parts["gain"] = gain
	}
	spec["aim"] = parts
	return []string{"spec.aim split into the parts it was built from"}
}

// assignIDs gives every item in one list an id it does not already have,
// derived from the named field and falling back to a numbered prefix.
func assignIDs(items []any, path string, from []string, prefix string) []string {
	var notes []string
	taken := map[string]bool{}
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			if id, _ := m["id"].(string); id != "" {
				taken[id] = true
			}
		}
	}
	for i, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := m["id"].(string); id != "" {
			continue
		}
		// The first field that says something is the seed.
		text := ""
		for _, field := range from {
			if v, _ := m[field].(string); strings.TrimSpace(v) != "" {
				text = v
				break
			}
		}
		id := uniqueSlug(slugify(text), fmt.Sprintf("%s%d", prefix, i+1), taken)
		taken[id] = true
		m["id"] = id
		notes = append(notes, fmt.Sprintf("%s[%d].id assigned %q", path, i, id))
	}
	return notes
}

// slugify reduces free text to the Slug pattern: lowercase, every run of
// anything else a single hyphen, trimmed, and short enough to read. It
// matches the interface's own slug helper so an id generated on either
// side of the wire comes out the same.
func slugify(s string) string {
	var b strings.Builder
	lastHyphen := true // leading hyphens are not allowed, so start as if one was just written
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		case !lastHyphen:
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	// An id is derived from an item's text only when that text is a name.
	// A field that holds a sentence — an objective, a problem — slugs into
	// something long and cut off mid-word ("catch-faults-at-intake-by-
	// checking-every-deliver"), which is worse than a plain numbered id at
	// the one job an id has: being recognisable. Those fall back instead.
	// The Slug pattern also needs two characters, and a one-character id
	// is not a name anybody would read.
	if len(out) < 2 || len(out) > 32 {
		return ""
	}
	return out
}

// uniqueSlug returns want when it is usable and free, the fallback when it
// is not, and a numbered variant when both are taken.
func uniqueSlug(want, fallback string, taken map[string]bool) string {
	if want != "" && !taken[want] {
		return want
	}
	if want == "" {
		want = fallback
	}
	if !taken[want] {
		return want
	}
	for n := 2; ; n++ {
		try := fmt.Sprintf("%s-%d", want, n)
		if !taken[try] {
			return try
		}
	}
}

// rewriteRoleRefs turns the three fields that held a copy of a role's
// title into references. Until 2026-09-28 an acceptance criterion's
// verifier, and a success criterion's owner and confirmer, each stored the
// role's title as a string "exactly as spec.resources names it" — so
// renaming a role left three stale copies behind and nothing noticed. The
// title is matched case-insensitively against spec.resources[].title; what
// matches becomes a local reference, and what does not becomes an external
// one rather than being dropped, because a confirmer is often a governance
// body the vault has no manifest for. Nothing is lost silently either way:
// an external reference is visible in the interface and counted by a check.
//
// Runs after assignLocalIDs, which is what gives the roles ids to point at.
func rewriteRoleRefs(spec map[string]any) []string {
	byTitle := map[string]string{}
	if resources, ok := spec["resources"].([]any); ok {
		for _, r := range resources {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			title, _ := rm["title"].(string)
			id, _ := rm["id"].(string)
			if t := strings.ToLower(strings.TrimSpace(title)); t != "" && id != "" {
				byTitle[t] = id
			}
		}
	}

	var notes []string
	convert := func(holder map[string]any, field, path string) {
		title, ok := holder[field].(string)
		if !ok {
			return
		}
		if strings.TrimSpace(title) == "" {
			delete(holder, field)
			notes = append(notes, fmt.Sprintf("%s.%s: empty text dropped", path, field))
			return
		}
		if id, found := byTitle[strings.ToLower(strings.TrimSpace(title))]; found {
			holder[field] = map[string]any{"local": "resources", "id": id}
			notes = append(notes, fmt.Sprintf("%s.%s rewritten from the title %q to a reference to spec.resources %q", path, field, title, id))
			return
		}
		holder[field] = map[string]any{"external": title}
		notes = append(notes, fmt.Sprintf("%s.%s: %q matches no role, kept as an external reference", path, field, title))
	}

	if deliverables, ok := spec["deliverables"].([]any); ok {
		for i, d := range deliverables {
			dm, ok := d.(map[string]any)
			if !ok {
				continue
			}
			criteria, ok := dm["acceptance"].([]any)
			if !ok {
				continue
			}
			for j, cr := range criteria {
				if cm, ok := cr.(map[string]any); ok {
					convert(cm, "by", fmt.Sprintf("spec.deliverables[%d].acceptance[%d]", i, j))
				}
			}
		}
	}
	if criteria, ok := spec["successCriteria"].([]any); ok {
		for i, cr := range criteria {
			cm, ok := cr.(map[string]any)
			if !ok {
				continue
			}
			convert(cm, "owner", fmt.Sprintf("spec.successCriteria[%d]", i))
			convert(cm, "confirmedBy", fmt.Sprintf("spec.successCriteria[%d]", i))
		}
	}
	return notes
}

// liftStakeholderScores removes the power assessment from a project's own
// resource rows, where it used to sit as influence, interest and tier.
//
// A resource is declared once and used across many projects; how much
// power it holds is particular to each, so it belongs to the link rather
// than to either end, and it lives on a StakeholderMap now (Programme
// Lead, 2026-09-28).
//
// The rewriter reads and writes one document, so it cannot lift the scores
// into a map manifest of their own: there is nowhere in this function to
// put a second file. It drops them and names each one in a note instead,
// so nothing goes silently — the import report says which resource carried
// what, which is enough to write the map by hand. The role row itself
// stays, because a stakeholder is a resource the project references like
// any other; only the score moves.
func liftStakeholderScores(spec map[string]any) []string {
	resources, ok := spec["resources"].([]any)
	if !ok {
		return nil
	}
	var notes []string
	for i, r := range resources {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		var carried []string
		for _, field := range []string{"influence", "interest", "tier"} {
			if v, present := rm[field]; present {
				carried = append(carried, fmt.Sprintf("%s %v", field, v))
				delete(rm, field)
			}
		}
		if len(carried) == 0 {
			continue
		}
		who, _ := rm["resource"].(string)
		if who == "" {
			who, _ = rm["title"].(string)
		}
		notes = append(notes, fmt.Sprintf(
			"spec.resources[%d] (%s) carried %s; scores live on a StakeholderMap now and were dropped here",
			i, who, strings.Join(carried, ", ")))
	}
	return notes
}

// dropStakeholderRoles removes resource rows whose position was
// "stakeholder". A project's resources are what it draws on to do the
// work; somebody with an interest in the outcome is a different relation
// and is held on the StakeholderMap scoped to the work, whether or not the
// work also draws on them.
//
// The rewriter reads and writes one document, so it cannot move the row
// onto a map: it names each one in a note instead, which is enough to
// write the entry by hand.
func dropStakeholderRoles(spec map[string]any) []string {
	resources, ok := spec["resources"].([]any)
	if !ok {
		return nil
	}
	var kept []any
	var notes []string
	for i, r := range resources {
		rm, ok := r.(map[string]any)
		if !ok {
			kept = append(kept, r)
			continue
		}
		if role, _ := rm["role"].(string); role != "stakeholder" {
			kept = append(kept, r)
			continue
		}
		who, _ := rm["resource"].(string)
		if who == "" {
			who, _ = rm["title"].(string)
		}
		notes = append(notes, fmt.Sprintf(
			"spec.resources[%d] (%s) was a stakeholder row; stakeholders are held on a StakeholderMap now and it was dropped here",
			i, who))
	}
	if len(notes) > 0 {
		spec["resources"] = kept
	}
	return notes
}

// dropRoleTitles removes the free-text name a role carried beside its
// catalogue reference.
//
// Two fields named the same thing, and the vaults show they could not stay
// apart: "title" held a restatement of the position as often as a name —
// `{role: sponsor, title: "Sponsor", resource: depot-network}` — so a
// reader could not tell the catalogue entry from the local label, and the
// two would have drifted. The catalogue entry
// survives, because that is the one a second project can point at too.
//
// Nothing is lost that was said anywhere else: the position is in `role`,
// the name is the Resource's, and every reference to the row points at its
// id. A row whose title said something neither field holds leaves a note,
// so it is visible rather than silently dropped.
func dropRoleTitles(spec map[string]any) []string {
	resources, ok := spec["resources"].([]any)
	if !ok {
		return nil
	}
	var notes []string
	for _, r := range resources {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		title, _ := rm["title"].(string)
		delete(rm, "title")
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		// A title that only restated the position, or that matches the
		// catalogue entry it sits beside, said nothing twice.
		role, _ := rm["role"].(string)
		if slugify(title) == role || slugify(title) == fmt.Sprint(rm["resource"]) {
			continue
		}
		if _, named := rm["resource"].(string); !named {
			notes = append(notes, fmt.Sprintf(
				"resources: %q named no catalogue entry; its title was dropped, so the row now says only which position it is", title))
		}
	}
	return notes
}

// dropFreeTextFunding clears the two free-text boxes a funding line used to
// carry: which budget, and a code beside it.
//
// A budget is a FundingSource now, declared once and referenced, because
// the code identifies the budget rather than one project's use of it
// (TAXONOMY.md D7). A legacy line names its budget in prose, and prose
// cannot become a reference without inventing a manifest for it — so the
// text is reported rather than guessed at, the same way a role's dropped
// title is. The amount, the currency and the status are untouched: what the
// line was for survives; only the naming of the budget has to be redone
// once, in the catalogue, where every project can then point at it.
func dropFreeTextFunding(spec map[string]any) []string {
	lines, ok := spec["funding"].([]any)
	if !ok {
		return nil
	}
	var notes []string
	for i, l := range lines {
		lm, ok := l.(map[string]any)
		if !ok {
			continue
		}
		source, _ := lm["source"].(string)
		reference, _ := lm["reference"].(string)
		delete(lm, "reference")
		// A source that is already a slug is a reference, not prose.
		if source != "" && slugify(source) == source {
			continue
		}
		delete(lm, "source")
		switch {
		case strings.TrimSpace(source) == "" && strings.TrimSpace(reference) == "":
		case strings.TrimSpace(reference) == "":
			notes = append(notes, fmt.Sprintf(
				"funding[%d]: named its budget in prose (%q); declare it as a FundingSource and reference it", i, source))
		default:
			notes = append(notes, fmt.Sprintf(
				"funding[%d]: named its budget in prose (%q, %q); declare it as a FundingSource, with that code, and reference it", i, source, reference))
		}
	}
	return notes
}

// rewriteGapCitations turns a bare list of gap ids into citations.
//
// A problem used to cite gaps as ["a", "b"], which can say that a gap is
// answered and nothing more. A gap observed in four slices, answered by
// work reaching one of them, read as the whole claim. A citation is an
// object now, and the id alone still means the whole gap, so every old
// file keeps exactly the meaning it had (TAXONOMY.md D9).
func rewriteGapCitations(kind string, spec map[string]any) []string {
	var lists [][]any
	switch kind {
	case "Project":
		if summary, ok := spec["summary"].(map[string]any); ok {
			if l, ok := summary["problems"].([]any); ok {
				lists = append(lists, l)
			}
		}
	case "Programme":
		if l, ok := spec["problems"].([]any); ok {
			lists = append(lists, l)
		}
	default:
		return nil
	}

	var notes []string
	for _, problems := range lists {
		for i, p := range problems {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			cites, ok := pm["gaps"].([]any)
			if !ok {
				continue
			}
			changed := false
			for j, c := range cites {
				id, isString := c.(string)
				if !isString {
					continue
				}
				cites[j] = map[string]any{"gap": id}
				changed = true
			}
			if changed {
				notes = append(notes, fmt.Sprintf(
					"problems[%d].gaps rewritten as citations; each still names the whole gap", i))
			}
		}
	}
	return notes
}

// dropFreeTextDisaggregations clears the dimensions a KPI used to name in
// prose.
//
// What a measure is broken down by and what a gap is observed in are the
// same concept, and one of them was free text. Segments are references
// now, and prose cannot become a reference without inventing a manifest
// for it, so the text is reported rather than guessed at — the same way a
// role's dropped title and a funding line's prose budget are.
func dropFreeTextDisaggregations(spec map[string]any) []string {
	list, ok := spec["disaggregations"].([]any)
	if !ok {
		return nil
	}
	var kept []any
	var dropped []string
	for _, d := range list {
		text, isString := d.(string)
		if !isString {
			continue
		}
		// A slug is already a reference; anything else was prose.
		if slugify(text) == text {
			kept = append(kept, text)
			continue
		}
		dropped = append(dropped, text)
	}
	if len(dropped) == 0 {
		return nil
	}
	if len(kept) > 0 {
		spec["disaggregations"] = kept
	} else {
		delete(spec, "disaggregations")
	}
	return []string{fmt.Sprintf(
		"disaggregations named %v in prose; declare them as Segments and reference them", dropped)}
}

// rewriteFreeTextUnit turns the words a KPI used for its unit into a
// reference to one.
//
// Free text let one quantity be spelled several ways ("percent", "%" and
// "per cent", beside "USD", "rate" and "hours"), with nothing to say which
// of them meant the same thing.
// A unit is a manifest now, and the standard set uses the obvious slugs, so
// "percent" and "Hours" both land on one without anybody deciding anything.
// A word that slugs to no standard unit is reported rather than guessed at:
// the vault's owner declares it, and then every KPI that meant it points at
// the same thing.
func rewriteFreeTextUnit(spec map[string]any) []string {
	unit, ok := spec["unit"].(string)
	if !ok || strings.TrimSpace(unit) == "" {
		return nil
	}
	slug := slugify(unit)
	if slug == unit {
		// Already a reference.
		return nil
	}
	for _, u := range standardUnits {
		if u.ID == slug {
			spec["unit"] = slug
			return []string{fmt.Sprintf("unit %q read as the standard unit %q", unit, slug)}
		}
	}
	delete(spec, "unit")
	return []string{fmt.Sprintf(
		"unit %q is not a declared unit; declare it and point this KPI at it", unit)}
}

// liftAssumptionRisks reports risks that were typed as assumptions.
//
// "assumption" left the risk type on 2026-09-29: a risk is mitigated, an
// assumption is tested, a false assumption changes the theory rather than
// the plan, and an assumption belongs on the link it conditions rather
// than in a flat list beside things that might go wrong.
//
// The row is kept and retyped as a constraint rather than dropped —
// somebody wrote it and it says something true — and reported, so the
// vault's owner can declare it as an Assumption and point the step that
// depends on it at the new manifest. Nothing here invents that manifest:
// prose cannot become a reference without one.
func liftAssumptionRisks(spec map[string]any) []string {
	risks, ok := spec["risks"].([]any)
	if !ok {
		return nil
	}
	var notes []string
	for i, r := range risks {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := rm["type"].(string); t != "assumption" {
			continue
		}
		rm["type"] = "constraint"
		description, _ := rm["description"].(string)
		notes = append(notes, fmt.Sprintf(
			"risks[%d] was typed an assumption (%q); an assumption is now its own kind, tested rather than mitigated — declare it and point the step that depends on it at it",
			i, description))
	}
	return notes
}

// deprecatedFieldProblems refuses, in a draft being written, a field that
// is read from older manifests but never written. Accepted, it would be
// checked as what it once meant and fail later with a message about
// something else: a gap's measuredBy is read as a data source, so an
// agent that meant the KPI measuring the gap heard about a missing data
// source at propose. Saved manifests are still read and folded as before.
func deprecatedFieldProblems(kind string, doc map[string]any) []Problem {
	spec, _ := doc["spec"].(map[string]any)
	var problems []Problem
	switch kind {
	case "Gap":
		if _, ok := spec["measuredBy"]; ok {
			problems = append(problems, Problem{Path: "/spec/measuredBy",
				Message: "measuredBy is no longer written: name the KPI whose baseline and target are this gap's two states in measure, and the data sources it is watched in under dataSources"})
		}
	case "KPI":
		if _, ok := spec["source"]; ok {
			problems = append(problems, Problem{Path: "/spec/source",
				Message: "source is no longer written: list the data sources it is read from under sources"})
		}
	}
	return problems
}
