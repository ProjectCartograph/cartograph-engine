package render

import (
	"context"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// ProgrammeCharter renders a programme as the document its definition adds
// up to: what it is for, what is wrong, what it serves, how it believes the
// change happens (its theory of change), what is inside it, and who runs it.
// The same five-part skeleton a project's charter uses, with a programme's
// own sections (MSP programme brief, PMI program charter).
//
// Read-only and derived, like every other view: nothing is stored, so there
// is nothing to keep in step.
func ProgrammeCharter(ctx context.Context, e *engine.Engine, id string) ([]byte, error) {
	vers, err := e.Get(ctx, "Programme", id)
	if err != nil {
		return nil, fmt.Errorf("get programme: %w", err)
	}
	name, spec, err := manifestOf(e, vers, id)
	if err != nil {
		return nil, err
	}
	n := loadNames(ctx, e)
	inside := components(ctx, e, id, spec)
	graph, _ := e.Components(ctx)
	self := engine.Ref{Kind: "Programme", ID: id}

	// The programme's checks, read as the readiness of each part.
	var items []engine.ProjectCheckItem
	if pcs, err := e.ProgrammeChecks(ctx, id); err == nil {
		for _, c := range pcs {
			items = append(items, engine.ProjectCheckItem{ID: c.ID, Section: c.Section, State: c.State, Message: c.Message})
		}
	}

	var d doc
	d.head(name, "Programme charter", vers)
	d.control(vers, docControl{
		Reference: aliasOf(e, vers),
		Sponsor:   n.of("Resource", str(spec["sponsor"])),
		Owner:     n.of("Resource", str(spec["manager"])),
		Status:    readinessWord(items),
	})
	d.contents()

	aim := obj(spec["aim"])
	d.h2("At a glance")
	if c := str(aim["change"]); c != "" {
		d.lead(capital(c))
	}
	d.facts(
		field{"Sponsor", n.of("Resource", str(spec["sponsor"]))},
		field{"Programme manager", n.of("Resource", str(spec["manager"]))},
		field{"Business change manager", n.of("Resource", str(spec["businessChangeManager"]))},
		field{"Lead team", n.of("Team", str(spec["leadTeam"]))},
		field{"Components", plural2(len(inside), "component", "components")},
		field{"Goals", strings.Join(n.all("Goal", strs(spec["goals"])), "; ")},
		field{"Source", str(spec["source"])},
	)
	d.readinessOf(items, programmeAreas)

	if str(aim["change"])+str(aim["gain"]) != "" {
		d.h2("Aim")
		d.fields(
			field{"Intended change", capital(str(aim["change"]))},
			field{"Benefit", capital(str(aim["gain"]))},
		)
	}
	d.sections(spec, "aim")
	d.problems(n, list(spec["problems"]), "/spec/problems")
	d.sections(spec, "problems")

	// The benefits it is judged on (MSP): the goals, and the measures with
	// where they start and where they should get to.
	d.h2("Benefits")
	d.list("Goals", n.all("Goal", strs(spec["goals"])))
	d.table([]string{"Indicator", "Baseline", "Target", "Data source", "Frequency"},
		measures(ctx, e, n, strs(spec["kpis"])))
	d.sections(spec, "alignment")

	// The pathway, read the way it is authored: the outcome first, then
	// what has to hold before it.
	steps := list(spec["pathway"])
	if len(steps) > 0 {
		d.h2("Theory of change")
		for i := len(steps) - 1; i >= 0; i-- {
			sm := steps[i]
			d.h3named(n.of("Goal", str(sm["outcome"])))
			d.p(str(sm["because"]))
			d.fields(
				field{"Preconditions", strings.Join(n.all("Goal", strs(sm["from"])), "; ")},
				field{"Assumptions", strings.Join(n.all("Assumption", strs(sm["assumes"])), "; ")},
			)
		}
	}
	d.sections(spec, "pathway")

	// What it is made of: the work it lists and the work that names it
	// (TAXONOMY.md D46), drawn with what each depends on in turn.
	if len(inside) > 0 {
		d.h2("Components and dependencies")
		d.raw(dependencyDiagram(n, graph, self))
		d.table([]string{"Component", "Type", "Sub-components"}, inside)
		if len(graph.CriticalPath) > 1 {
			var chain []string
			for _, r := range graph.CriticalPath {
				chain = append(chain, n.of(r.Kind, r.ID))
			}
			d.p(fmt.Sprintf("Critical path: %s, %d months.", strings.Join(chain, " depends on "), graph.CriticalMonths))
		}
	}
	d.sections(spec, "components")

	d.risks(n, spec, list(spec["risks"]))
	d.sections(spec, "risks")

	d.h2("Governance")
	p := newPlan(n, spec)
	d.fields(
		field{"Lead team", n.of("Team", str(spec["leadTeam"]))},
		field{"Supporting teams", strings.Join(n.all("Team", strs(spec["supportingTeams"])), ", ")},
		field{"Escalation route", escalation(p, spec)},
	)
	d.mandate(n, spec, list(spec["mandate"]))
	d.stakeholders(stakeholders(ctx, e, n, "Programme", id))
	d.sections(spec, "governance")

	sponsor := "Sponsor"
	if s := str(spec["sponsor"]); s != "" {
		sponsor += ": " + n.of("Resource", s)
	}
	roles := []string{sponsor}
	for _, r := range []struct{ label, field string }{{"Programme manager", "manager"}, {"Business change manager", "businessChangeManager"}} {
		if s := str(spec[r.field]); s != "" {
			roles = append(roles, r.label+": "+n.of("Resource", s))
		}
	}
	d.signOff(append(roles, "Lead team: "+n.of("Team", str(spec["leadTeam"]))))
	printed := map[string]bool{"aim": true, "problems": true, "alignment": true, "pathway": true, "components": true, "risks": true, "governance": true}
	d.otherSections(spec, printed)
	d.history(ctx, e, "Programme", id)
	d.heldElsewhere(spec)
	return d.end(), nil
}

// programmeAreas group a programme's checks the way its walk does.
var programmeAreas = []readinessArea{
	{"Aim and problems", []string{"aim", "problems"}},
	{"Outcomes and measures", []string{"alignment", "measures", "evidence"}},
	{"Components", []string{"components"}},
	{"Theory of change", []string{"pathway"}},
	{"Risks", []string{"risks"}},
	{"Governance", []string{"governance"}},
}

func plural2(n int, one, many string) string {
	if n == 0 {
		return ""
	}
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// components lists the projects and operations that name this programme,
// with each project's own components beside it.
func components(ctx context.Context, e *engine.Engine, programme string, spec map[string]any) [][]string {
	children := map[string][]string{}
	each(ctx, e, "Project", func(_, cname string, spec map[string]any) {
		if parent := str(obj(spec["alignment"])["partOf"]); parent != "" {
			children[parent] = append(children[parent], cname)
		}
	})
	var out [][]string
	for _, kind := range []string{"Project", "Operation"} {
		each(ctx, e, kind, func(id, name string, spec map[string]any) {
			named := strs(spec["programmes"])
			named = append(named, strs(obj(spec["alignment"])["programmes"])...)
			for _, p := range named {
				if p == programme {
					label := "Project"
					if kind == "Operation" {
						label = "Operation"
					}
					out = append(out, []string{name, label, strings.Join(children[id], "; ")})
					return
				}
			}
		})
	}
	// What the programme lists itself (TAXONOMY.md D46), where nothing
	// already named it.
	n := loadNames(ctx, e)
	for _, cm := range list(spec["components"]) {
		kind, cid := str(cm["kind"]), str(cm["id"])
		if kind == "" || cid == "" {
			continue
		}
		name := n.of(kind, cid)
		listed := false
		for _, row := range out {
			listed = listed || row[0] == name
		}
		if !listed {
			out = append(out, []string{name, kind, strings.Join(children[cid], "; ")})
		}
	}
	return out
}

/** Gap citations name the gap and, where the work reaches part of it,
 * which part. The charter prints the gap. */
func gapIDs(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		switch c := it.(type) {
		case string:
			out = append(out, c)
		case map[string]any:
			if g, ok := c["gap"].(string); ok && g != "" {
				out = append(out, g)
			}
		}
	}
	return out
}
