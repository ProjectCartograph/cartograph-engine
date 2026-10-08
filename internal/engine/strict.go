package engine

import (
	"context"
	"fmt"
)

// unfinished are the schema keywords a draft fails only for lacking
// something not written yet: a required field, too few items, an empty
// text. A draft is built up a field at a time, so these are its checks'
// business. Every other failure is a shape the record must never hold.
var unfinished = map[string]bool{"required": true, "minItems": true, "minLength": true, "minProperties": true}

// StructuralProblems are what is built wrong in a manifest, as against
// what is not finished yet (docs/adr/0027): every failure of the kind's
// strict profile (its schema with the shapes the discipline refuses, such
// as a second objective on a project, or a person's name on a role) or of
// its schema that more writing cannot mend. An agent's draft with any is
// refused, so an improper structure is never saved, however it is sent.
func (e *Engine) StructuralProblems(_ context.Context, kind string, text []byte) ([]Problem, error) {
	var doc map[string]any
	if err := e.codec.DecodeInto(text, &doc); err != nil {
		return []Problem{{Message: "invalid yaml: " + err.Error()}}, nil
	}
	if doc == nil {
		return []Problem{{Message: "empty manifest"}}, nil
	}
	rewriteLegacyFields(kind, doc)
	schema, ok := e.schemas.strict[kind]
	if !ok {
		if schema, ok = e.schemas.compiled[kind]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
		}
	}
	err := schema.Validate(any(doc))
	if err == nil {
		return nil, nil
	}
	var out []Problem
	for _, p := range schemaProblems(err) {
		if !unfinished[p.Keyword] {
			out = append(out, p)
		}
	}
	return out, nil
}
