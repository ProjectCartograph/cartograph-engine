package engine

import "github.com/ProjectCartograph/cartograph-engine/v2/internal/legacy"

// normalizeLegacy rewrites one stored manifest's bytes into the current
// shape (legacy.Rewrite), returning the rewritten YAML when anything
// actually changed and the original bytes otherwise.
//
// The rewrite used to run only on import, which was enough when importing
// was how manifests arrived. A vault is a directory the person also edits
// by hand and syncs with whatever their organisation already uses, so a
// file in last release's shape can appear at any moment, and every reader
// (the interface, the checks, the charter) has to cope. The file on disk
// is left alone until the next save writes it back in the current shape.
func (e *Engine) normalizeLegacy(kind string, yamlBytes []byte) []byte {
	var doc map[string]any
	if err := e.codec.DecodeInto(yamlBytes, &doc); err != nil {
		return yamlBytes
	}
	if len(legacy.Rewrite(kind, doc)) == 0 {
		return yamlBytes
	}
	out, err := e.codec.Encode(doc)
	if err != nil {
		return yamlBytes
	}
	return out
}

// rewriteLegacyFields is legacy.Rewrite, the engine's name for it.
func rewriteLegacyFields(kind string, doc map[string]any) []string { return legacy.Rewrite(kind, doc) }

// deprecatedFieldProblems are the deprecated fields a document holds, as
// problems on their paths.
func deprecatedFieldProblems(kind string, doc map[string]any) []Problem {
	var out []Problem
	for _, f := range legacy.Deprecated(kind, doc) {
		out = append(out, Problem{Path: f.Path, Message: f.Message})
	}
	return out
}
