package render

import (
	"context"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// OperationCharter renders an operation, a service that keeps running, as
// its service description (ITIL's service design package, cut to what Cartograph
// holds): what it does and when, who runs it, what it is part of, how it is
// measured, what it uses and produces, and which projects are changing it.
//
// The last section is the one no other view shows. A project names the
// operation it lands in (TAXONOMY.md D17); read back here, it tells the
// people who run a service what is about to be handed to them.
func OperationCharter(ctx context.Context, e *engine.Engine, id string) ([]byte, error) {
	vers, err := e.Get(ctx, "Operation", id)
	if err != nil {
		return nil, fmt.Errorf("get operation: %w", err)
	}
	name, spec, err := manifestOf(e, vers, id)
	if err != nil {
		return nil, err
	}
	n := loadNames(ctx, e)

	var d doc
	d.head(name, "Operation", vers)
	d.facts(
		field{"Status", serviceStatus(spec)},
		field{"Service owner", n.of("Resource", str(spec["serviceOwner"]))},
		field{"Team", n.of("Team", str(spec["team"]))},
		field{"Service hours", str(spec["serviceWindow"])},
		field{"Programme", strings.Join(n.all("Programme", strs(spec["programmes"])), "; ")},
	)

	d.h2("Service description")
	d.p(capital(str(spec["purpose"])))

	// Service levels (ITIL): what it is measured by, from where it starts
	// to where it should be.
	d.h2("Service levels")
	d.table([]string{"Indicator", "Baseline", "Target", "Data source", "Frequency"},
		measures(ctx, e, n, strs(spec["kpis"])))

	// What pays to run it, per period (TAXONOMY.md D39): a running cost
	// recurs, so each amount is labelled with the period it is for.
	if lines := list(spec["funding"]); len(lines) > 0 {
		d.h2("Running costs")
		rows, _ := budget(n, lines)
		for i, f := range lines {
			if per := str(f["per"]); per != "" && rows[i][0] != "" {
				rows[i][0] += " a " + per
			}
		}
		d.table([]string{"Amount", "Funding source", "Status"}, rows)
	}

	d.data(n, obj(spec["data"]))

	var landing [][]string
	each(ctx, e, "Project", func(_, pname string, pspec map[string]any) {
		if str(pspec["operation"]) != id {
			return
		}
		var objective string
		if objs := list(pspec["objectives"]); len(objs) > 0 {
			objective = capital(str(objs[0]["objective"]))
		}
		landing = append(landing, []string{pname, objective, n.of("Team", str(pspec["team"]))})
	})
	if len(landing) > 0 {
		d.h2("Projects handing over")
		d.table([]string{"Project", "Objective", "Team"}, landing)
	}

	d.mandate(list(spec["mandate"]))
	d.stakeholders(stakeholders(ctx, e, n, "Operation", id))

	owner := "Service owner"
	if s := str(spec["serviceOwner"]); s != "" {
		owner += ": " + n.of("Resource", s)
	}
	d.signOff([]string{owner})
	return d.end(), nil
}

// serviceStatus is where a service is in its life, as a charter prints it
// (TAXONOMY.md D30): running when it says none, as every service saved
// before 2.7 does.
func serviceStatus(spec map[string]any) string {
	switch str(spec["status"]) {
	case "planned":
		return "Planned"
	case "retired":
		return "Retired"
	}
	return "Running"
}

// landsIn is the service a project hands over to, as its charter names it:
// marked planned when the project is what sets it up.
func landsIn(ctx context.Context, e *engine.Engine, n names, id string) string {
	if id == "new" {
		return "A new service, not yet defined"
	}
	name := n.of("Operation", id)
	if vers, err := e.Get(ctx, "Operation", id); err == nil {
		if _, spec, err := manifestOf(e, vers, id); err == nil && str(spec["status"]) == "planned" {
			return name + " (planned, set up by this project)"
		}
	}
	return name
}
