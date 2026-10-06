package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Check is one thing a manifest still needs, in a shape every kind
// shares: what it is, whether it is met, what it says, and the section of
// the editor where it is fixed. State is ok, warn, or block (a project's
// blocking checks stop a handoff).
type Check struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Message string `json:"message"`
	Section string `json:"section,omitempty"`
	// Path is the field the check is about, as a JSON pointer, where it is
	// one field: what a record must have before it can be saved as a
	// version.
	Path string `json:"path,omitempty"`
}

// Open reports whether a check is not yet met.
func (c Check) Open() bool { return c.State != checkOK }

// ChecksOf computes the checks for a manifest's text, saved or not: every
// check the kind has, as the editor shows them. Kinds without checks of
// their own have none; their schema and rules are what validation reports.
func (e *Engine) ChecksOf(ctx context.Context, kind, id string, text []byte) ([]Check, error) {
	var doc map[string]any
	if err := e.codec.DecodeInto(text, &doc); err != nil {
		return nil, fmt.Errorf("parse %s/%s: %w", kind, id, err)
	}
	out := []Check{}
	var advisory func(context.Context, string, map[string]any) ([]ProgrammeCheck, error)
	switch kind {
	case "Programme":
		advisory = e.programmeChecksOf
	case "Operation":
		advisory = e.operationChecksOf
	case "Gap":
		advisory = e.gapChecksOf
	case "StakeholderMap":
		advisory = e.stakeholderMapChecksOf
	case "KPI":
		advisory = e.kpiChecksOf
	case "Purpose":
		advisory = purposeChecksOf
	case "Goal", "Project":
	default:
		return out, nil
	}
	if advisory != nil {
		checks, err := advisory(ctx, id, doc)
		if err != nil {
			return nil, err
		}
		for _, c := range checks {
			out = append(out, Check{ID: c.ID, State: c.State, Message: c.Message, Section: c.Section})
		}
		return out, nil
	}
	if kind == "Goal" {
		checks, err := e.goalChecksOf(ctx, id, doc)
		if err != nil {
			return nil, err
		}
		for _, c := range checks {
			section := ""
			if c.Fix != nil {
				section = c.Fix.Section
			}
			out = append(out, Check{ID: c.ID, State: c.State, Message: c.Message, Section: section})
		}
		return out, nil
	}
	rewriteLegacyFields("Project", doc)
	pc, err := e.projectChecksOf(ctx, id, doc)
	if err != nil {
		return nil, err
	}
	for _, c := range pc.Items {
		out = append(out, Check{ID: c.ID, State: c.State, Message: c.Message, Section: c.Fix.Section})
	}
	return out, nil
}

// DraftChecks computes the checks for a manifest as it stands: its draft
// where there is one, else its latest version.
func (e *Engine) DraftChecks(ctx context.Context, kind, id string) ([]Check, error) {
	if text, ok := inPlay(ctx, kind, id); ok {
		return e.ChecksOf(e.withInPlay(ctx), kind, id, text)
	}
	text, found, err := e.manifests.GetWorking(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	if !found {
		v, ok, err := e.manifests.GetCurrent(ctx, kind, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, kind, id)
		}
		text = v.YAML
	}
	return e.ChecksOf(ctx, kind, id, text)
}

// DraftProblems validates a manifest as it stands, its draft where there
// is one, against its schema and rules, reading the other drafts of the
// change set on ctx as if saved: what a save or a proposal would refuse,
// said while the draft is being written rather than at the end.
func (e *Engine) DraftProblems(ctx context.Context, kind, id string) ([]Problem, error) {
	text, ok := inPlay(ctx, kind, id)
	if !ok {
		working, found, err := e.manifests.GetWorking(ctx, kind, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		text = working
	}
	return e.Validate(e.withInPlay(ctx), kind, text)
}

// proposedKey carries the manifests of a proposal being checked, so a
// check that reads other manifests also sees the ones proposed with this
// one: an outcome is closed by a gap proposed beside it.
type proposedKey struct{}

func withProposed(ctx context.Context, docs map[string]map[string]any) context.Context {
	return context.WithValue(ctx, proposedKey{}, docs)
}

func proposedDocs(ctx context.Context) map[string]map[string]any {
	docs, _ := ctx.Value(proposedKey{}).(map[string]map[string]any)
	return docs
}

// gapsClosing names the gaps whose outcomes include this outcome: saved
// ones, and any proposed with it.
func (e *Engine) gapsClosing(ctx context.Context, id string) ([]string, error) {
	var names []string
	seen := map[string]bool{}
	for key, doc := range proposedDocs(ctx) {
		if !strings.HasPrefix(key, "Gap/") {
			continue
		}
		spec, _ := doc["spec"].(map[string]any)
		outs, _ := spec["outcomes"].([]any)
		gid := strings.TrimPrefix(key, "Gap/")
		seen[gid] = true
		for _, o := range outs {
			if o == id {
				names = append(names, docName(doc, gid))
			}
		}
	}
	refs, err := e.referencing(ctx, "Goal", id)
	if err != nil {
		return nil, err
	}
	for _, r := range refs {
		if r.Kind == "Gap" && !seen[r.ID] {
			names = append(names, r.Name)
		}
	}
	return names, nil
}

type namedGoal struct{ id, name string }

// goalsUnder names the goals at level whose parent is id: saved ones, and
// any proposed with it.
func (e *Engine) goalsUnder(ctx context.Context, id, level string) ([]string, error) {
	goals, err := e.goalIDsUnder(ctx, id, level)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(goals))
	for i, g := range goals {
		out[i] = g.name
	}
	return out, nil
}

// goalIDsUnder is goalsUnder with each goal's id.
func (e *Engine) goalIDsUnder(ctx context.Context, id, level string) ([]namedGoal, error) {
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
	docs, err := l.Documents("Goal")
	if err != nil {
		return nil, err
	}
	for key, doc := range proposedDocs(ctx) {
		if gid, ok := strings.CutPrefix(key, "Goal/"); ok {
			docs[gid] = doc
		}
	}
	var out []namedGoal
	for gid, doc := range docs {
		spec, _ := doc["spec"].(map[string]any)
		if p, _ := spec["parent"].(string); p == id {
			if lv, _ := spec["level"].(string); lv == level {
				out = append(out, namedGoal{id: gid, name: docName(doc, gid)})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// withInPlay checks with every draft of the change set on ctx, as one set.
func (e *Engine) withInPlay(ctx context.Context) context.Context {
	docs := map[string]map[string]any{}
	for _, r := range inPlayRefs(ctx) {
		t, _ := inPlay(ctx, r.Kind, r.ID)
		var d map[string]any
		if e.codec.DecodeInto(t, &d) == nil {
			docs[r.Kind+"/"+r.ID] = d
		}
	}
	return withProposed(ctx, docs)
}

// purposeChecksOf asks a purpose for what every goal is judged relevant
// against: whose purpose it is, its vision and its mission. Empty, they
// show here, where they are written, rather than only as each goal's
// relevance failing.
func purposeChecksOf(_ context.Context, _ string, doc map[string]any) ([]ProgrammeCheck, error) {
	spec, _ := doc["spec"].(map[string]any)
	var out []ProgrammeCheck
	add := func(id, field, ok, warn string) {
		v, _ := spec[field].(string)
		if strings.TrimSpace(v) != "" {
			out = append(out, ProgrammeCheck{ID: id, Section: "purpose", State: programmeCheckOK, Message: ok})
		} else {
			out = append(out, ProgrammeCheck{ID: id, Section: "purpose", State: programmeCheckWarn, Message: warn})
		}
	}
	add("purpose-organisation", "organisation", "The organisation is named.", "No organisation named yet.")
	add("purpose-vision", "vision", "The vision is stated.", "No vision yet: the future the organisation works towards.")
	add("purpose-mission", "mission", "The mission is stated.", "No mission yet: what the organisation does to get there.")
	return out, nil
}
