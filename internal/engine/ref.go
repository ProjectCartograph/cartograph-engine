package engine

import "strings"

// A reference is the one shape Cartograph uses everywhere a definition points at
// something else: kind+id for another manifest, local+id for an item inside
// the same manifest, and external for a target the vault will never hold.
// The three forms share a struct so every reader handles all of them, and
// so one walk over a vault can compile every edge into a dependency tree.
//
// Cross-manifest references are found and checked generically by refwalk,
// off the x-cartograph-ref annotation. This type is for the readers that need
// the value itself: the checks, which ask whether a reference names
// anything, and the charter, which has to print it.
type ref struct {
	Kind     string
	Local    string
	ID       string
	External string
}

// parseRef reads a reference out of a manifest field. The second result is
// false when the field is absent or is not a reference at all, which
// includes the string a manifest in last release's shape still carries: the
// legacy rewriter converts those on read, so a string reaching here means
// the document was never normalized.
func parseRef(v any) (ref, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return ref{}, false
	}
	kind, _ := m["kind"].(string)
	local, _ := m["local"].(string)
	id, _ := m["id"].(string)
	external, _ := m["external"].(string)
	r := ref{
		Kind:     strings.TrimSpace(kind),
		Local:    strings.TrimSpace(local),
		ID:       strings.TrimSpace(id),
		External: strings.TrimSpace(external),
	}
	if r.empty() {
		return r, false
	}
	return r, true
}

// empty reports that a reference names nothing at all.
func (r ref) empty() bool {
	if r.External != "" {
		return false
	}
	return r.ID == ""
}

// outsideCartograph reports a reference to something the vault does not hold, so
// a check can say how many edges leave Cartograph without treating them as
// faults: a dependency on a the board approval is real, it is just not
// something Cartograph can resolve.
func (r ref) outsideCartograph() bool { return r.External != "" }

// label renders a reference as the text a document shows. A local
// reference resolves against the manifest it lives in, which is the only
// place its target exists; a cross-manifest one falls back to the id, since
// resolving a name needs a store the charter renderer does not have.
func (r ref) label(spec map[string]any) string {
	switch {
	case r.External != "":
		return r.External
	case r.Local != "":
		if t := localItemTitle(spec, r.Local, r.ID); t != "" {
			return t
		}
		return r.ID
	default:
		return r.ID
	}
}

// localItemTitle finds the human text of an item in one of a manifest's own
// lists, by id. The field that carries that text differs per list, so the
// lookup tries the ones that exist rather than assuming a single name.
func localItemTitle(spec map[string]any, list, id string) string {
	if spec == nil || list == "" || id == "" {
		return ""
	}
	items, ok := spec[list].([]any)
	if !ok {
		// The timeline's phases are the one referenced list that is not a
		// direct child of spec.
		if list == "phases" {
			tl, _ := spec["timeline"].(map[string]any)
			if tl == nil {
				return ""
			}
			items, ok = tl["phases"].([]any)
		}
		if !ok {
			return ""
		}
	}
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := m["id"].(string); got != id {
			continue
		}
		for _, field := range []string{"title", "name", "objective", "problem", "statement", "description"} {
			if s, _ := m[field].(string); strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		return ""
	}
	return ""
}

// RefLabel renders a reference field as the text a document shows, given
// the spec it lives in so local references can resolve. Exported for the
// charter renderer, which prints references and lives in its own package.
// A field that is not a reference at all renders empty rather than
// guessing, so a stale document shows a gap instead of a wrong name.
func RefLabel(v any, spec map[string]any) string {
	r, ok := parseRef(v)
	if !ok {
		return ""
	}
	return r.label(spec)
}

// RefIsSet reports whether a reference field names anything.
func RefIsSet(v any) bool {
	_, ok := parseRef(v)
	return ok
}
