package render

import (
	"context"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
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

	inside := components(ctx, e, id)
	var d doc
	d.head(name, "Programme", vers)
	d.facts(
		field{"Sponsor", n.of("Resource", str(spec["sponsor"]))},
		field{"Programme manager", n.of("Resource", str(spec["manager"]))},
		field{"Business change manager", n.of("Resource", str(spec["businessChangeManager"]))},
		field{"Lead team", n.of("Team", str(spec["leadTeam"]))},
		field{"Supporting teams", strings.Join(n.all("Team", strs(spec["supportingTeams"])), ", ")},
		field{"Components", plural2(len(inside), "component", "components")},
		field{"Goals", plural2(len(strs(spec["goals"])), "goal", "goals")},
		field{"Source", str(spec["source"])},
	)

	aim := obj(spec["aim"])
	if str(aim["change"])+str(aim["gain"])+str(spec["source"]) != "" {
		d.h2("Aim")
		d.fields(
			field{"Intended change", capital(str(aim["change"]))},
			field{"Benefit", capital(str(aim["gain"]))},
		)
	}

	d.problems(n, list(spec["problems"]))

	// The benefits it is judged on (MSP): the goals, and the measures with
	// where they start and where they should get to.
	d.h2("Benefits")
	d.list("Goals", n.all("Goal", strs(spec["goals"])))
	d.table([]string{"Indicator", "Baseline", "Target", "Data source", "Frequency"},
		measures(ctx, e, n, strs(spec["kpis"])))

	// The pathway, read the way it is authored: the outcome first, then
	// what has to hold before it. The file's order is causal, so this walks
	// it backwards.
	steps := list(spec["pathway"])
	if len(steps) > 0 {
		d.h2("Theory of change")
		for i := len(steps) - 1; i >= 0; i-- {
			sm := steps[i]
			d.h3(n.of("Goal", str(sm["outcome"])))
			d.p(str(sm["because"]))
			d.fields(
				field{"Preconditions", strings.Join(n.all("Goal", strs(sm["from"])), "; ")},
				field{"Assumptions", strings.Join(n.all("Assumption", strs(sm["assumes"])), "; ")},
			)
		}
	}

	// What is inside it, read back from the work that names it rather than
	// from anything the programme stores (TAXONOMY.md D1, D2). A project that
	// is a component of another project appears under its parent.
	if len(inside) > 0 {
		d.h2("Components")
		d.table([]string{"Component", "Type", "Sub-components"}, inside)
	}

	d.risks(n, spec, list(spec["risks"]))

	d.h2("Governance")
	d.fields(
		field{"Lead team", n.of("Team", str(spec["leadTeam"]))},
		field{"Supporting teams", strings.Join(n.all("Team", strs(spec["supportingTeams"])), ", ")},
	)
	d.stakeholders(stakeholders(ctx, e, n, "Programme", id))

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
	d.heldElsewhere()
	return d.end(), nil
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
func components(ctx context.Context, e *engine.Engine, programme string) [][]string {
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
