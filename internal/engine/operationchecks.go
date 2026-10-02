package engine

import (
	"context"
	"fmt"
)

// OperationChecks says whether an operation is described well enough to be
// run and measured, step by step. Advisory only, like a programme's (D6):
// an operation is a service that already runs, and nothing about how it is
// written down should stop anything.
func (e *Engine) OperationChecks(ctx context.Context, id string) ([]ProgrammeCheck, error) {
	v, found, err := e.manifests.GetCurrent(ctx, "Operation", id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: Operation/%s", ErrNotFound, id)
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		return nil, fmt.Errorf("parse Operation/%s: %w", id, err)
	}
	return e.operationChecksOf(ctx, id, doc)
}

// operationChecksOf checks a operation's document, saved or not: a draft, or
// what an agent proposes.
func (e *Engine) operationChecksOf(ctx context.Context, id string, doc map[string]any) ([]ProgrammeCheck, error) {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		spec = map[string]any{}
	}
	var out []ProgrammeCheck
	add := func(checkID, section, state, message string) {
		out = append(out, ProgrammeCheck{ID: checkID, Section: section, State: state, Message: message})
	}
	has := func(key string) bool {
		s, _ := spec[key].(string)
		return s != ""
	}
	count := func(key string) int {
		l, _ := spec[key].([]any)
		return len(l)
	}

	if has("purpose") {
		add("service-purpose", "service", programmeCheckOK, "Purpose stated.")
	} else {
		add("service-purpose", "service", programmeCheckWarn, "No purpose yet.")
	}
	// The service owner is the role accountable for the service end to end
	// (ITIL 4); the team that runs it is required by the schema already.
	if has("serviceOwner") {
		add("service-owner", "service", programmeCheckOK, "Service owner named.")
	} else {
		add("service-owner", "service", programmeCheckWarn, "No service owner yet: the role accountable for the service.")
	}
	if has("serviceWindow") {
		add("service-hours", "service", programmeCheckOK, "Service hours stated.")
	} else {
		add("service-hours", "service", programmeCheckWarn, "No service hours yet.")
	}
	if n := count("programmes"); n > 0 {
		add("alignment-programmes", "alignment", programmeCheckOK, fmt.Sprintf("Part of %d programme%s.", n, plural(n)))
	} else {
		// A programme is optional (TAXONOMY.md D25): an operation outside
		// one is not a finding, so there is no warning to give.
	}
	if n := count("kpis"); n > 0 {
		add("measures-kpis", "measures", programmeCheckOK, fmt.Sprintf("%d Indicator%s.", n, plural(n)))
	} else {
		add("measures-kpis", "measures", programmeCheckWarn, "No indicator yet.")
	}
	return out, nil
}
