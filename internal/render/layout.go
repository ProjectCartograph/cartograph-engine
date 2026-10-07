package render

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// The frame around a charter's sections: document control, the contents,
// the readiness of each part, the components it depends on, the version
// history, and what is kept elsewhere.

type docControl struct {
	Reference, Classification, Owner, Sponsor, Status string
}

// control writes the document control table under the title, as a
// charter template opens (reference, version, date, classification,
// owner, status).
func (d *doc) control(vers engine.Version, c docControl) {
	// The version line under the title says what this table says.
	if i := bytes.LastIndex(d.b.Bytes(), []byte(`<p class="version subtitle">`)); i >= 0 {
		rest := d.b.Bytes()[i:]
		if j := bytes.Index(rest, []byte("</p>\n")); j >= 0 {
			tail := append([]byte{}, rest[j+5:]...)
			d.b.Truncate(i)
			d.b.Write(tail)
		}
	}
	version, date := "Working draft", "Not yet saved as a version"
	if vers.Number > 0 {
		version = fmt.Sprintf("Version %d", vers.Number)
		date = vers.On.Format("2 January 2006")
	}
	d.b.WriteString("<table class=\"control\">\n")
	row := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			d.b.WriteString("<tr><th>" + esc(k) + "</th><td>" + esc(v) + "</td></tr>\n")
		}
	}
	row("Reference", c.Reference)
	row("Version", version)
	row("Date", date)
	row("Classification", c.Classification)
	row("Sponsor", c.Sponsor)
	row("Document owner", c.Owner)
	row("Status", c.Status)
	d.b.WriteString("</table>\n")
}

// contents marks where the table of contents goes; end fills it from the
// sections written.
func (d *doc) contents() { d.b.WriteString(tocMark) }

const tocMark = "<!--contents-->"

func (d *doc) lead(s string) {
	d.flush()
	d.b.WriteString("<p class=\"lead\">" + esc(s) + "</p>\n")
}

// readinessAreas group the walk's steps the way a charter template's
// readiness assessment does.
var readinessAreas = []struct {
	label string
	steps []string
}{
	{"Authority and alignment", []string{"goals"}},
	{"Problem and beneficiaries", []string{"beneficiaries", "aim"}},
	{"Objectives and measures", []string{"measures"}},
	{"Governance, resources and stakeholders", []string{"resources", "stakeholders"}},
	{"Scope and deliverables", []string{"scope", "deliverables"}},
	{"Schedule and milestones", []string{"timeline"}},
	{"Data", []string{"data"}},
	{"Risks and dependencies", []string{"risks"}},
	{"Success criteria and handover", []string{"success", "landing"}},
	{"Approval and conditions", []string{"approval"}},
}

// readiness prints the state of each part of the definition from the
// checks Cartograph runs on it: ready, open, or blocking. A charter
// template asks for this as a hand-filled assessment; here it is read.
func (d *doc) readiness(items []engine.ProjectCheckItem) {
	if len(items) == 0 {
		return
	}
	var rows [][]string
	for _, a := range readinessAreas {
		state, ok := "ready", 0
		var open []string
		for _, it := range items {
			if !hasString(a.steps, it.Section) {
				continue
			}
			switch it.State {
			case "ok":
				ok++
			case "block":
				state = "blocking"
				open = append(open, it.Message)
			default:
				if state != "blocking" {
					state = "open"
				}
				open = append(open, it.Message)
			}
		}
		if ok+len(open) == 0 {
			continue
		}
		evidence := fmt.Sprintf("%d of %d checks met.", ok, ok+len(open))
		if len(open) > 0 {
			evidence += " " + strings.Join(open, " ")
		}
		rows = append(rows, []string{a.label, chip("readiness", state), evidence})
	}
	d.h3("Definition readiness")
	d.table([]string{"Part of the definition", "Status", "Evidence"}, rows)
}

// readinessWord is the document's status, from its checks.
func readinessWord(items []engine.ProjectCheckItem) string {
	blocking, open := 0, 0
	for _, it := range items {
		switch it.State {
		case "block":
			blocking++
		case "warn":
			open++
		}
	}
	switch {
	case blocking > 0:
		return fmt.Sprintf("Draft: %d item%s block approval", blocking, plural(blocking))
	case open > 0:
		return fmt.Sprintf("Ready for approval, %d point%s to review", open, plural(open))
	}
	return "Ready for approval: every check met"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// scopeColumns prints what is in and out side by side.
func (d *doc) scopeColumns(in, out []string) {
	d.flush()
	d.b.WriteString("<div class=\"columns\">\n<div>")
	d.list("In scope", in)
	d.b.WriteString("</div>\n<div>")
	d.list("Out of scope", out)
	d.b.WriteString("</div>\n</div>\n")
}

// dependencyTable prints what this work depends on and what depends on it,
// with the critical path and the most depended on marked (TAXONOMY.md D46).
func (d *doc) dependencyTable(n names, g engine.ComponentGraph, self engine.Ref) {
	nodes := map[engine.Ref]engine.ComponentNode{}
	for _, nd := range g.Nodes {
		nodes[nd.Ref] = nd
	}
	marks := func(nd engine.ComponentNode) string {
		var m []string
		if nd.InLoop {
			m = append(m, "In a loop")
		}
		if nd.Critical {
			m = append(m, "Critical path")
		}
		if nd.MostDependedOn {
			m = append(m, "Most depended on")
		}
		return strings.Join(m, "; ")
	}
	var rows [][]string
	for _, ed := range g.Edges {
		var other engine.Ref
		var rel string
		switch {
		case ed.From == self:
			other, rel = ed.To, "Depends on"
		case ed.To == self:
			other, rel = ed.From, "Used by"
		default:
			continue
		}
		nd := nodes[other]
		months := ""
		if nd.Months > 0 {
			months = fmt.Sprintf("%d", nd.Months)
		}
		rows = append(rows, []string{rel, n.of(other.Kind, other.ID), other.Kind, ed.Why, months, marks(nd)})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	d.table([]string{"Relation", "Work", "Kind", "What it needs", "Months", "Marks"}, rows)
	if len(g.CriticalPath) > 1 {
		var chain []string
		for _, r := range g.CriticalPath {
			chain = append(chain, n.of(r.Kind, r.ID))
		}
		d.p(fmt.Sprintf("Critical path across the workspace: %s, %d months.", strings.Join(chain, " depends on "), g.CriticalMonths))
	}
	for _, loop := range g.Loops {
		var chain []string
		for _, r := range loop {
			chain = append(chain, n.of(r.Kind, r.ID))
		}
		d.p("Loop to resolve: " + strings.Join(chain, " depends on ") + ".")
	}
}

// history prints the numbered versions: document control as Cartograph
// keeps it, each with who saved it and why.
func (d *doc) history(ctx context.Context, e *engine.Engine, kind, id string) {
	vs, err := e.Versions(ctx, kind, id)
	if err != nil || len(vs) == 0 {
		return
	}
	var rows [][]string
	for i := len(vs) - 1; i >= 0 && len(rows) < 12; i-- {
		v := vs[i]
		if v.Number == 0 {
			continue
		}
		rows = append(rows, []string{fmt.Sprintf("%d", v.Number), v.On.Format("2 January 2006"), v.Actor, v.Reason})
	}
	if len(rows) == 0 {
		return
	}
	d.h2("Version history")
	d.table([]string{"Version", "Date", "Saved by", "Reason"}, rows)
}

// heldElsewhere says, once and briefly, what a charter traditionally
// carries that Cartograph deliberately leaves to the tools that manage
// delivery.
func (d *doc) heldElsewhere(spec map[string]any) {
	d.h2("Related plans")
	d.flush()
	d.b.WriteString("<p class=\"held\">Kept in the tools that manage delivery.</p>\n")
	fs := []field{{"Detailed plan", "Work management platform"}}
	if len(list(spec["procurement"])) == 0 {
		fs = append(fs, field{"Procurement", "Finance and procurement systems"})
	}
	fs = append(fs, field{"Communications and training plan", "Work management platform"},
		field{"Change control", "Numbered versions and change sets in Cartograph"})
	d.fields(fs...)
}
