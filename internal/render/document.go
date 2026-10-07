package render

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"strings"
	"unicode"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// One charter, three kinds (TAXONOMY.md D21).
//
// Every founding document the standards describe has the same five parts:
// the problem and the end state, what is in and out, why it is being done,
// who is accountable, and how success is judged. A project's charter, a
// programme's and an operation's are that skeleton with their own sections
// in it, so they share this writer.
//
// Three rules the writer enforces, because the old charter broke all three:
// a reference prints the thing's name, never its id; an enum prints a plain
// label, never its identifier; and an empty row or section is not printed.
// Parts are shown as parts (D19): a problem is its labelled fields, not a
// sentence assembled from them.

// names resolves the ids a charter prints into the names people use.
type names map[string]map[string]string

// charterKinds are the registers a charter can point into.
var charterKinds = []string{
	"Goal", "KPI", "BeneficiaryGroup", "Team", "Gap", "Assumption", "Programme",
	"Project", "Operation", "DataSource", "Resource", "ReportingCycle",
	"FundingSource", "Unit", "Segment",
}

func loadNames(ctx context.Context, e *engine.Engine) names {
	n := names{}
	for _, kind := range charterKinds {
		byID := map[string]string{}
		if summaries, err := e.List(ctx, kind, engine.Filter{}, false); err == nil {
			for _, s := range summaries {
				byID[s.ID] = s.Name
			}
		}
		n[kind] = byID
	}
	return n
}

// of returns the name of a kind's item, or the id when the register does
// not hold it: a dangling id is printed as it is written so the gap shows.
func (n names) of(kind, id string) string {
	if id == "" {
		return ""
	}
	if name := n[kind][id]; name != "" {
		return name
	}
	return id
}

func (n names) all(kind string, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if s := n.of(kind, id); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ref prints any reference field: a plain slug of the given kind, kind+id,
// a local reference into the manifest's own lists, or an external name. A
// local reference into resources prints the role's person-free title from
// the Resource catalogue, which is what a reader expects to see.
func (n names) ref(v any, kind string, spec map[string]any) string {
	switch r := v.(type) {
	case string:
		return n.of(kind, r)
	case map[string]any:
		if ext, _ := r["external"].(string); ext != "" {
			return ext
		}
		id, _ := r["id"].(string)
		if k, _ := r["kind"].(string); k != "" {
			return n.of(k, id)
		}
		if local, _ := r["local"].(string); local == "resources" {
			for _, it := range list(spec["resources"]) {
				if it["id"] == id {
					if res, _ := it["resource"].(string); res != "" {
						return n.of("Resource", res)
					}
				}
			}
		}
		return engine.RefLabel(v, spec)
	}
	return ""
}

// labels are the plain words for the contract's enum values. Anything not
// listed is split at its capitals, which is right for most and keeps a new
// value readable until it gets a better word.
var labels = map[string]map[string]string{
	"role": {
		"sponsor": "Sponsor", "manager": "Project manager", "teamMember": "Team member",
		"userRepresentative": "User representative", "technicalLead": "Technical lead",
		"dataOwner": "Data owner", "dataSteward": "Data steward", "dataCustodian": "Data custodian",
		"projectSupport": "Project support", "serviceOwner": "Service owner",
	},
	"when": {
		"atClosing":        "At closure",
		"atLanding":        "At handover",
		"postClosingCycle": "After handover",
	},
	"metric": {
		"efficiency": "Efficiency", "customer": "Impact on the customer",
		"team": "Impact on the team", "business": "Business success",
		"future": "Preparation for the future", "compliance": "Compliance",
	},
	"riskType": {
		"risk": "Risk", "issue": "Issue", "dependency": "Dependency",
		"constraint": "Constraint",
	},
	"approach": {
		"manageClosely": "Manage closely", "keepSatisfied": "Keep satisfied",
		"keepInformed": "Keep informed", "monitor": "Monitor",
	},
	"priority": {"primary": "Primary", "secondary": "Secondary"},
	"status": {
		"notStarted": "Not started", "inProgress": "In progress", "met": "Met",
		"notApplicable": "Does not apply", "approved": "Approved",
		"requested": "Requested", "unfunded": "Not funded",
	},
	"mandate": {
		"decision": "Decision", "policy": "Policy",
		"lawOrRegulation": "Law or regulation", "contract": "Contract",
		"businessCase": "Business case", "request": "Request",
	},
	"handoff": {
		"apiOrFeed": "Automatic feed", "fileTransfer": "File transfer",
		"sharedDatabase": "Shared database", "manualReentry": "Manual re-entry",
		"paper": "Paper",
	},
	"output": {
		"newDataSource": "New data source", "recordsInExistingSource": "Records in existing source",
		"documentsOrFiles": "Documents or files", "extractOrReport": "Extract or report",
	},
	"refresh": {
		"daily": "Daily", "weekly": "Weekly", "monthly": "Monthly",
		"quarterly": "Quarterly", "annual": "Annual", "adHoc": "Ad hoc",
	},
	"personal":  {"none": "None", "personal": "Personal", "sensitive": "Sensitive"},
	"direction": {"increase": "Increase", "decrease": "Decrease", "maintain": "Maintain", "reach": "Reach"},
}

func label(group, value string) string {
	if value == "" {
		return ""
	}
	if l := labels[group][value]; l != "" {
		return l
	}
	return humanise(value)
}

// humanise turns an identifier into words: "inProgress" reads "In progress".
func humanise(id string) string {
	var b strings.Builder
	for i, r := range id {
		switch {
		case r == '-' || r == '_':
			b.WriteRune(' ')
		case unicode.IsUpper(r) && i > 0:
			b.WriteRune(' ')
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// doc writes the document. Every string goes through html.EscapeString:
// definitions are typed by people, and a charter is opened by others.
//
// A heading is held until something is written under it, so a section
// with nothing in it disappears instead of printing an empty title.
type doc struct {
	b       bytes.Buffer
	pending string
}

func (d *doc) flush() {
	if d.pending != "" {
		// The step of a project's walk that defines the section, so an
		// interface showing the charter beside the walk can open it there.
		if step := sectionSteps[d.pending]; step != "" {
			d.b.WriteString(`<h2 data-step="` + step + `">` + esc(d.pending) + "</h2>\n")
		} else {
			d.b.WriteString("<h2>" + esc(d.pending) + "</h2>\n")
		}
		d.pending = ""
	}
}

// sectionSteps is the step of a project's walk each charter section is
// written in.
var sectionSteps = map[string]string{
	"Problem statement":              "aim",
	"Scope":                          "scope",
	"Objectives and key results":     "measures",
	"Deliverables":                   "deliverables",
	"Strategic alignment":            "goals",
	"Components":                     "goals",
	"Governance and roles":           "resources",
	"Budget":                         "resources",
	"Stakeholders":                   "stakeholders",
	"Success criteria":               "success",
	"Milestones":                     "timeline",
	"Risks, issues and dependencies": "risks",
	"Data requirements":              "data",
}

func esc(s string) string { return html.EscapeString(strings.TrimSpace(s)) }

func (d *doc) h2(s string) { d.pending = s }
func (d *doc) h3(s string) {
	d.flush()
	d.b.WriteString("<h3>" + esc(s) + "</h3>\n")
}

func (d *doc) p(s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	d.flush()
	d.b.WriteString("<p>" + esc(s) + "</p>\n")
}

// field is one labelled value. A field with no value is not printed.
type field struct{ label, value string }

// pathed is a field whose value is one field's text as written, with that
// field's JSON pointer, so an interface showing the charter can edit it in
// place.
type pathed struct{ label, value, path string }

func (d *doc) fields(fs ...field) {
	var kept []field
	for _, f := range fs {
		if strings.TrimSpace(f.value) != "" {
			kept = append(kept, f)
		}
	}
	if len(kept) == 0 {
		return
	}
	d.flush()
	d.b.WriteString("<dl>\n")
	for _, f := range kept {
		d.b.WriteString("<dt>" + esc(f.label) + "</dt><dd>" + esc(f.value) + "</dd>\n")
	}
	d.b.WriteString("</dl>\n")
}

// fieldsAt is fields with each value's pointer on it.
func (d *doc) fieldsAt(fs ...pathed) {
	var kept []pathed
	for _, f := range fs {
		if strings.TrimSpace(f.value) != "" {
			kept = append(kept, f)
		}
	}
	if len(kept) == 0 {
		return
	}
	d.flush()
	d.b.WriteString("<dl>\n")
	for _, f := range kept {
		if f.path == "" {
			d.b.WriteString("<dt>" + esc(f.label) + "</dt><dd>" + esc(f.value) + "</dd>\n")
			continue
		}
		d.b.WriteString("<dt>" + esc(f.label) + `</dt><dd data-field="` + esc(f.path) + `">` + esc(f.value) + "</dd>\n")
	}
	d.b.WriteString("</dl>\n")
}

// list prints a heading and its items, or nothing when there are none.
func (d *doc) list(heading string, items []string) {
	var kept []string
	for _, it := range items {
		if strings.TrimSpace(it) != "" {
			kept = append(kept, it)
		}
	}
	if len(kept) == 0 {
		return
	}
	d.flush()
	if heading != "" {
		d.b.WriteString("<p class=\"label\">" + esc(heading) + "</p>\n")
	}
	d.b.WriteString("<ul>\n")
	for _, it := range kept {
		d.b.WriteString("<li>" + esc(it) + "</li>\n")
	}
	d.b.WriteString("</ul>\n")
}

// table prints the rows that have anything in them. A table with no such
// rows is not printed at all.
func (d *doc) table(headers []string, rows [][]string) {
	var kept [][]string
	for _, r := range rows {
		for _, c := range r {
			if strings.TrimSpace(c) != "" {
				kept = append(kept, r)
				break
			}
		}
	}
	if len(kept) == 0 {
		return
	}
	// A column nobody filled in is dropped too: "Issued by" with nothing
	// under it is noise.
	var cols []int
	for i := range headers {
		for _, r := range kept {
			if i < len(r) && strings.TrimSpace(r[i]) != "" {
				cols = append(cols, i)
				break
			}
		}
	}
	headers = pick(headers, cols)
	for i, r := range kept {
		kept[i] = pick(r, cols)
	}
	d.flush()
	d.b.WriteString("<table>\n<tr>")
	for _, h := range headers {
		d.b.WriteString("<th>" + esc(h) + "</th>")
	}
	d.b.WriteString("</tr>\n")
	for _, r := range kept {
		d.b.WriteString("<tr>")
		for _, c := range r {
			if group, value, ok := isChip(c); ok {
				d.b.WriteString("<td class=\"fit\">" + chipHTML(group, value) + "</td>")
				continue
			}
			d.b.WriteString("<td>" + esc(c) + "</td>")
		}
		d.b.WriteString("</tr>\n")
	}
	d.b.WriteString("</table>\n")
}

func pick(row []string, cols []int) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if c < len(row) {
			out = append(out, row[c])
		} else {
			out = append(out, "")
		}
	}
	return out
}

// head writes everything above the first section. kindLine says what the
// document is: "Project", "Component of …", "Programme", "Operation".
func (d *doc) head(name, kindLine string, vers engine.Version) {
	d.b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>`)
	d.b.WriteString(esc(name))
	d.b.WriteString(`</title>
<style>
:root { --fg: #1f2328; --muted: #5b636e; --line: #d8dde3; --soft: #f5f7f9; --accent: #2f6f5e; }
@media (prefers-color-scheme: dark) {
  :root { --fg: #e6e8eb; --muted: #9aa3ad; --line: #3a4048; --soft: #22262b; --accent: #7cc2ad; }
  body { background: #181a1d; }
}
* { box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", sans-serif;
  line-height: 1.55; color: var(--fg); margin: 0 auto; padding: 2rem 1.25rem 4rem; max-width: 860px; }
header { border-bottom: 2px solid var(--line); padding-bottom: 1rem; margin-bottom: 1rem; }
.kind { text-transform: uppercase; letter-spacing: .06em; font-size: .75rem; color: var(--accent); font-weight: 600; margin: 0; }
h1 { font-size: 1.9rem; line-height: 1.25; margin: .25rem 0 .5rem; }
.version { color: var(--muted); font-size: .85rem; margin: 0; }
h2 { font-size: 1.3rem; margin: 2.25rem 0 .5rem; padding-top: .75rem; border-top: 1px solid var(--line); }
header + h2 { border-top: 0; padding-top: 0; }
h3 { font-size: 1.02rem; margin: 1.5rem 0 .4rem; }
p { margin: .4rem 0; }
.label { font-weight: 600; margin: 1rem 0 .25rem; }
dl { display: grid; grid-template-columns: minmax(9rem, 12rem) 1fr; gap: .35rem 1rem; margin: .5rem 0 1rem; }
dt { color: var(--muted); }
dd { margin: 0; }
@media (max-width: 560px) { dl { grid-template-columns: 1fr; } dd { margin-bottom: .5rem; } }
table { width: 100%; border-collapse: collapse; margin: .75rem 0 1.25rem; font-size: .93rem; }
th, td { border: 1px solid var(--line); padding: .45rem .6rem; text-align: left; vertical-align: top; }
th { background: var(--soft); font-weight: 600; }
ul { margin: .3rem 0 .8rem; padding-left: 1.4rem; }
li { margin: .2rem 0; }
.held { color: var(--muted); }
dl.facts { display: grid; grid-template-columns: repeat(auto-fill, minmax(11rem, 1fr)); gap: .75rem 1.25rem;
  background: var(--soft); border: 1px solid var(--line); border-radius: 10px; padding: 1rem 1.1rem; margin: 1.25rem 0 .5rem; }
dl.facts div { display: flex; flex-direction: column; gap: .1rem; }
dl.facts dt { font-size: .75rem; text-transform: uppercase; letter-spacing: .04em; }
dl.facts dd { font-weight: 600; }
table.signoff td { height: 2.4rem; }
.chip { display: inline-flex; align-items: center; gap: .3rem; white-space: nowrap; border: 1px solid var(--line);
  border-radius: 999px; padding: .05rem .5rem .05rem .4rem; font-size: .82rem; line-height: 1.5; }
.chip svg { flex: none; }
td.fit { width: 1%; white-space: nowrap; }
table.signoff td:first-child { width: 38%; }
@page { size: A4; margin: 16mm 14mm; }
@media print {
  body { padding: 0; max-width: none; color: #000; background: #fff; }
  :root { --fg: #000; --muted: #444; --line: #bbb; --soft: #f3f3f3; --accent: #1f5a4b; }
  h2 { break-after: avoid; } table, dl, tr { break-inside: avoid; }
}
</style>
</head>
<body>
<header>
`)
	if kindLine != "" {
		d.b.WriteString("<p class=\"kind\">" + esc(kindLine) + "</p>\n")
	}
	d.b.WriteString("<h1>" + esc(name) + "</h1>\n<p class=\"version\">")
	if vers.Number > 0 {
		d.b.WriteString(fmt.Sprintf("Version %d, saved %s", vers.Number, vers.On.Format("2 January 2006")))
	} else {
		d.b.WriteString("Working draft, not yet saved as a version")
	}
	d.b.WriteString("</p>\n</header>\n\n")
}

func (d *doc) end() []byte {
	d.b.WriteString("\n</body>\n</html>\n")
	return d.b.Bytes()
}

// problems prints the problems a project or a programme answers, each as
// its parts: who, what is wrong, why, what will be different, and what
// they will then be able to do.
func (d *doc) problems(n names, items []map[string]any, base string) {
	if len(items) == 0 {
		return
	}
	d.h2("Problem statement")
	for i, pm := range items {
		if len(items) > 1 {
			d.h3(fmt.Sprintf("Problem %d", i+1))
		}
		problem, _ := pm["problem"].(map[string]any)
		change, _ := pm["change"].(map[string]any)
		at := fmt.Sprintf("%s/%d", base, i)
		d.fieldsAt(
			pathed{"Affected groups", strings.Join(n.all("BeneficiaryGroup", strs(pm["groups"])), ", "), ""},
			pathed{"Problem", capital(str(problem["situation"])), at + "/problem/situation"},
			pathed{"Potential cause", capital(str(problem["cause"])), at + "/problem/cause"},
			pathed{"Intended change", capital(str(change["what"])), at + "/change/what"},
			pathed{"Benefit", capital(str(change["gain"])), at + "/change/gain"},
			pathed{"Evidence", strings.Join(n.all("Gap", gapIDs(pm["gaps"])), "; "), ""},
		)
	}
}

// risks prints the risk list a project and a programme share. An
// escalated line says whose decision it needs and what the decision is:
// a bare "escalated" told a reader neither.
// risks prints the register. Each row names its owner (TAXONOMY.md D41);
// an unowned row is the manager's, so it prints the manager's role, or
// "Manager" when none is named.
func (d *doc) risks(n names, spec map[string]any, items []map[string]any) {
	manager := strings.Join(roleNames(n, list(spec["resources"]), "manager"), ", ")
	if manager == "" {
		manager = "Manager"
	}
	rows := make([][]string, 0, len(items))
	for _, r := range items {
		owner := n.ref(r["owner"], "Resource", spec)
		if owner == "" {
			owner = manager + " (by default)"
		}
		decision := ""
		if escalation, ok := r["escalate"].(map[string]any); ok {
			if flag, _ := escalation["flag"].(bool); flag {
				who := n.ref(escalation["to"], "Resource", spec)
				if who == "" {
					who = "Above the project"
				}
				decision = who
				if reason := str(escalation["reason"]); reason != "" {
					decision += ": " + capital(reason)
				}
			}
		}
		rows = append(rows, []string{
			chip("riskType", str(r["type"])), str(r["description"]),
			chip("level", str(r["impact"])), chip("level", str(r["likelihood"])),
			owner, str(r["mitigation"]), decision,
		})
	}
	if len(rows) == 0 {
		return
	}
	d.h2("Risks, issues and dependencies")
	d.table([]string{"Type", "Description", "Impact", "Likelihood", "Owner", "Response", "Decision needed from"}, rows)
}

// mandate prints the authorisations. An issuer the workspace holds is
// named by reference (TAXONOMY.md D43); one outside it, as written.
func (d *doc) mandate(n names, spec map[string]any, items []map[string]any) {
	rows := make([][]string, 0, len(items))
	for _, m := range items {
		issuer := n.ref(m["issuer"], "Resource", spec)
		if issuer == "" {
			issuer = str(m["issuedBy"])
		}
		rows = append(rows, []string{
			label("mandate", str(m["kind"])), str(m["title"]), str(m["reference"]),
			str(m["date"]), issuer,
		})
	}
	if len(rows) > 0 {
		d.h3("Authorisation")
	}
	d.table([]string{"Type", "Title", "Reference", "Date", "Issued by"}, rows)
}

// data prints what a piece of work uses and what it produces.
func (d *doc) data(n names, data map[string]any) {
	consumes, produces := list(data["consumes"]), list(data["produces"])
	if len(consumes) == 0 && len(produces) == 0 {
		return
	}
	d.h2("Data requirements")
	var uses [][]string
	for _, c := range consumes {
		uses = append(uses, []string{
			n.of("DataSource", str(c["source"])), str(c["purpose"]),
			label("handoff", str(c["handoff"])), chip("personal", personal(str(c["personalData"]))),
		})
	}
	if len(uses) > 0 {
		d.h3("Data used")
		d.table([]string{"Source", "Purpose", "Transfer method", "Personal data"}, uses)
	}
	var made [][]string
	for _, p := range produces {
		made = append(made, []string{
			label("output", str(p["output"])), n.of("DataSource", str(p["sink"])), str(p["purpose"]),
			label("refresh", str(p["refresh"])), chip("personal", personal(str(p["personalData"]))),
		})
	}
	if len(made) > 0 {
		d.h3("Data produced")
		d.table([]string{"Output", "Data store", "Purpose", "Frequency", "Personal data"}, made)
	}
}

// Readers for the untyped YAML maps the charter walks.

func str(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case int, int64, float64:
		return fmt.Sprintf("%v", t)
	}
	return ""
}

func strs(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s := str(it); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func list(v any) []map[string]any {
	items, _ := v.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

// number prints a decoded number, which a codec hands back as an int, an
// int64 or a float64 depending on its syntax. Asserting one type only is
// how a duration prints blank.
func number(v any) string {
	switch t := v.(type) {
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	}
	return ""
}

func capital(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// manifestOf reads a version's text into its name and spec, through the
// engine's codec: render reads what the store holds and never parses a
// syntax itself.
func manifestOf(e *engine.Engine, vers engine.Version, id string) (name string, spec map[string]any, err error) {
	manifest, err := e.Codec().Decode(vers.YAML)
	if err != nil {
		return "", nil, fmt.Errorf("unmarshal manifest: %w", err)
	}
	name = id
	if md := obj(manifest["metadata"]); str(md["name"]) != "" {
		name = str(md["name"])
	}
	return name, obj(manifest["spec"]), nil
}

// each walks every manifest of a kind, for the sections read back from the
// work that points here (components, work landing in an operation).
func each(ctx context.Context, e *engine.Engine, kind string, fn func(id, name string, spec map[string]any)) {
	summaries, err := e.List(ctx, kind, engine.Filter{}, false)
	if err != nil {
		return
	}
	for _, s := range summaries {
		v, err := e.Get(ctx, kind, s.ID)
		if err != nil {
			continue
		}
		_, spec, err := manifestOf(e, v, s.ID)
		if err != nil {
			continue
		}
		fn(s.ID, s.Name, spec)
	}
}

// heldElsewhere says, once and briefly, what a charter traditionally
// carries that Cartograph deliberately does not.
func (d *doc) heldElsewhere() {
	d.h2("Related plans")
	d.flush()
	d.b.WriteString("<p class=\"held\">Kept in the tools that manage delivery.</p>\n")
	d.fields(
		field{"Detailed plan", "Work management platform"},
		field{"Procurement", "Finance and procurement systems"},
		field{"Communications and training plan", "Work management platform"},
		field{"Change control", "Numbered versions in Cartograph"},
	)
}

// Chips: a value from a fixed list, shown as an icon and its word.
//
// Iconography first, and built for a black-and-white printer: the meaning
// is carried by the shape and by how much of it is filled, never by colour
// alone. A level is three bars, filled to its height; a status is a tick,
// a clock or a cross; a type has its own mark.

// personal leaves "none" out: a chip for the absence of personal data is
// noise in a column that exists to flag its presence.
func personal(v string) string {
	if v == "none" {
		return ""
	}
	return v
}

// chip marks a table cell as a chip; table renders it as one.
func chip(group, value string) string {
	if value == "" {
		return ""
	}
	return "\x00" + group + "\x00" + value
}

func isChip(cell string) (group, value string, ok bool) {
	if !strings.HasPrefix(cell, "\x00") {
		return "", "", false
	}
	parts := strings.SplitN(cell[1:], "\x00", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

const svgOpen = `<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">`

// levelBars draws n of three bars filled.
func levelBars(n int) string {
	var b strings.Builder
	b.WriteString(`<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">`)
	for i := 0; i < 3; i++ {
		h := 5 + i*4
		fill := "none"
		if i < n {
			fill = "currentColor"
		}
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="3.4" height="%d" rx=".6" fill="%s" stroke="currentColor" stroke-width="1"/>`, 1+i*5, 15-h, h, fill)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

var chipIcons = map[string]string{
	// risk types
	"risk":       svgOpen + `<path d="M8 2 15 14H1Z"/><path d="M8 6.5v3.5M8 12h.01"/></svg>`,
	"issue":      svgOpen + `<circle cx="8" cy="8" r="6.3"/><path d="M8 4.8v3.8M8 11.2h.01"/></svg>`,
	"dependency": svgOpen + `<path d="M6.5 9.5a3 3 0 0 0 4.2 0l2.1-2.1a3 3 0 0 0-4.2-4.2l-.7.7"/><path d="M9.5 6.5a3 3 0 0 0-4.2 0L3.2 8.6a3 3 0 0 0 4.2 4.2l.7-.7"/></svg>`,
	"constraint": svgOpen + `<rect x="3" y="7" width="10" height="7" rx="1.2"/><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2"/></svg>`,
	// statuses
	"approved":      svgOpen + `<circle cx="8" cy="8" r="6.3" fill="currentColor"/><path d="m5.2 8.2 1.9 1.9 3.8-4" stroke="#fff"/></svg>`,
	"met":           svgOpen + `<circle cx="8" cy="8" r="6.3" fill="currentColor"/><path d="m5.2 8.2 1.9 1.9 3.8-4" stroke="#fff"/></svg>`,
	"requested":     svgOpen + `<circle cx="8" cy="8" r="6.3"/><path d="M8 4.8V8l2.2 1.4"/></svg>`,
	"inProgress":    svgOpen + `<circle cx="8" cy="8" r="6.3"/><path d="M8 1.7a6.3 6.3 0 0 1 0 12.6Z" fill="currentColor"/></svg>`,
	"notStarted":    svgOpen + `<circle cx="8" cy="8" r="6.3" stroke-dasharray="2 2"/></svg>`,
	"unfunded":      svgOpen + `<circle cx="8" cy="8" r="6.3"/><path d="m5.6 5.6 4.8 4.8M10.4 5.6l-4.8 4.8"/></svg>`,
	"notApplicable": svgOpen + `<circle cx="8" cy="8" r="6.3"/><path d="M5 8h6"/></svg>`,
	// directions
	"increase": svgOpen + `<path d="M8 13V3M4 7l4-4 4 4"/></svg>`,
	"decrease": svgOpen + `<path d="M8 3v10M4 9l4 4 4-4"/></svg>`,
	"maintain": svgOpen + `<path d="M3 6h10M3 10h10"/></svg>`,
	"reach":    svgOpen + `<path d="M3 8h10M9 4l4 4-4 4"/></svg>`,
	// personal data
	"sensitive": svgOpen + `<path d="M8 1.8 13.5 4v4c0 3.3-2.4 5.6-5.5 6.4C4.9 13.6 2.5 11.3 2.5 8V4Z" fill="currentColor"/></svg>`,
	"personal":  svgOpen + `<path d="M8 1.8 13.5 4v4c0 3.3-2.4 5.6-5.5 6.4C4.9 13.6 2.5 11.3 2.5 8V4Z"/></svg>`,
	"escalated": svgOpen + `<path d="M3.5 14.5V2" /><path d="M3.5 2.5h8.5l-1.8 3 1.8 3H3.5" fill="currentColor"/></svg>`,
	// stakeholder priority
	"primary":   svgOpen + `<circle cx="8" cy="8" r="5.5" fill="currentColor"/></svg>`,
	"secondary": svgOpen + `<circle cx="8" cy="8" r="5.5"/></svg>`,
	// engagement approach (Mendelow's grid), one filled quarter per quadrant
	"manageClosely": svgOpen + `<rect x="2.5" y="2.5" width="11" height="11"/><rect x="8" y="2.5" width="5.5" height="5.5" fill="currentColor"/></svg>`,
	"keepSatisfied": svgOpen + `<rect x="2.5" y="2.5" width="11" height="11"/><rect x="2.5" y="2.5" width="5.5" height="5.5" fill="currentColor"/></svg>`,
	"keepInformed":  svgOpen + `<rect x="2.5" y="2.5" width="11" height="11"/><rect x="8" y="8" width="5.5" height="5.5" fill="currentColor"/></svg>`,
	"monitor":       svgOpen + `<rect x="2.5" y="2.5" width="11" height="11"/><rect x="2.5" y="8" width="5.5" height="5.5" fill="currentColor"/></svg>`,
	// success criterion timing
	"atClosing":        svgOpen + `<path d="M3 14V2.5h8.5l-1.6 2.8 1.6 2.7H3"/></svg>`,
	"atLanding":        svgOpen + `<path d="M2 13.5h12M3.5 11l9-3.5M5 6l1.5 3"/></svg>`,
	"postClosingCycle": svgOpen + `<path d="M13 8a5 5 0 1 1-1.5-3.6M13 2.5v2.8h-2.8"/></svg>`,
}

// chipHTML renders a chip: its icon, then its word.
func chipHTML(group, value string) string {
	icon := chipIcons[value]
	switch group {
	case "level":
		n := map[string]int{"low": 1, "medium": 2, "high": 3, "1": 1, "2": 2, "3": 3}[value]
		icon = levelBars(n)
	}
	text := label(group, value)
	if group == "level" {
		text = map[string]string{"1": "Low", "2": "Medium", "3": "High"}[value]
		if text == "" {
			text = humanise(value)
		}
	}
	return `<span class="chip chip-` + esc(group) + `">` + icon + `<span>` + esc(text) + `</span></span>`
}
