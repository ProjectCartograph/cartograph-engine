package engine

import (
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/contract"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
)

// schemaSet compiles and holds every kind's JSON Schema, plus the raw
// (undecoded-by-the-validator) documents used by the reference walker to
// find x-cartograph-ref annotations.
type schemaSet struct {
	compiled map[string]*jsonschema.Schema // by kind name
	raw      map[string]map[string]any     // by schema file name, for the ref walker
	docs     map[string]map[string]any     // JSON Schema documents, keyed by kind name, for GET /schemas/{kind}
	// bytes holds each kind's schema exactly as it is written on disk.
	// Unmarshalling into a map loses the order the author wrote the
	// properties in, and the interface builds a sheet's columns from that
	// order — so a Gap's "what is wrong" was the last column instead of
	// the first, because measuredBy, note, source, statement is what the
	// alphabet says.
	bytes map[string][]byte // by kind name
}

func loadSchemas() (*schemaSet, error) {
	entries, err := contract.Schemas.ReadDir("schemas")
	if err != nil {
		return nil, fmt.Errorf("read embedded schemas: %w", err)
	}

	raw := map[string]map[string]any{}
	source := map[string][]byte{}
	for _, e := range entries {
		b, err := contract.Schemas.ReadFile("schemas/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read schema %s: %w", e.Name(), err)
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			return nil, fmt.Errorf("parse schema %s: %w", e.Name(), err)
		}
		raw[e.Name()] = doc
		source[e.Name()] = b
	}

	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	for name, doc := range raw {
		if err := compiler.AddResource(name, doc); err != nil {
			return nil, fmt.Errorf("add schema resource %s: %w", name, err)
		}
	}

	compiled := map[string]*jsonschema.Schema{}
	docs := map[string]map[string]any{}
	bytesByKind := map[string][]byte{}
	for _, spec := range kinds.All {
		sch, err := compiler.Compile(spec.SchemaFile)
		if err != nil {
			return nil, fmt.Errorf("compile schema for %s: %w", spec.Name, err)
		}
		compiled[spec.Name] = sch
		docs[spec.Name] = raw[spec.SchemaFile]
		bytesByKind[spec.Name] = source[spec.SchemaFile]
	}

	return &schemaSet{compiled: compiled, raw: raw, docs: docs, bytes: bytesByKind}, nil
}

func asValidationError(err error) (*jsonschema.ValidationError, bool) {
	ve, ok := err.(*jsonschema.ValidationError)
	return ve, ok
}
