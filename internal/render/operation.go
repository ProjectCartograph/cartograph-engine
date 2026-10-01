package render

import (
	"context"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
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
