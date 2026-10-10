package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// How an agent's write is applied: names read as references, formats read, lists merged, ids given, and what may be left open.

// settleOf reads a settle call: its named inputs, and any JSON pointer
// beside them or under fields as a field to set.
func settleOf(raw map[string]any) (settleIn, error) {
	var in settleIn
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, &in); err != nil {
		return in, fmt.Errorf("settle: %w; send {kind, id, set: {JSON pointer: value}, open: [{check, reason}], asked, work}", err)
	}
	if in.Set == nil {
		in.Set = map[string]any{}
	}
	if f, ok := raw["fields"].(map[string]any); ok {
		for k, v := range f {
			in.Set[k] = v
		}
	}
	var unknown []string
	for k, v := range raw {
		switch {
		case strings.HasPrefix(k, "/"):
			in.Set[k] = v
		case k == "fields" || settleKeys[k]:
		default:
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return in, fmt.Errorf("settle does not take %s: fields go under set, as JSON pointers such as /spec/summary/about", strings.Join(unknown, ", "))
	}
	return in, nil
}

// settleKeys are settle's own inputs.
var settleKeys = map[string]bool{"changeSet": true, "kind": true, "id": true, "set": true, "unset": true, "open": true, "asked": true, "assumed": true, "work": true}

// withFields adds to each refused field what goes there, from the kind's
// guide, so an agent sent "additional properties 'statement' not allowed"
// learns the fields it meant: a refusal that teaches, not only refuses.
func withFields(c call, kind string, err error) error {
	var invalid *engine.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	g, gerr := c.o.Engine.Guide(c.ctx, kind, "", "")
	if gerr != nil {
		return err
	}
	var paths []string
	for _, st := range g.Steps {
		for _, f := range st.Fields {
			paths = append(paths, f.Path)
		}
	}
	out := &engine.ValidationError{}
	for _, p := range invalid.Problems {
		var segs []string
		for _, t := range strings.Split(strings.Trim(p.Path, "/"), "/") {
			if _, num := strconv.Atoi(t); num == nil {
				t = "-"
			}
			segs = append(segs, t)
		}
		at := "/" + strings.Join(segs, "/")
		var here []string
		for _, q := range paths {
			if rest, ok := strings.CutPrefix(q, at+"/"); ok && rest != "" {
				// A child, or a list's item's child: /spec/x/-/y.
				// Two levels down at most, list items not counted, so a
				// problem's fields reach problem/situation.
				if strings.Count(strings.ReplaceAll(rest, "-/", ""), "/") <= 1 {
					here = append(here, strings.Replace(q, "/-/", "/0/", -1))
				}
			}
		}
		if len(here) > 0 {
			if len(here) > 12 {
				here = here[:12]
			}
			p.Message += ". The fields there are: " + strings.Join(here, ", ")
		}
		out.Problems = append(out.Problems, p)
	}
	return out
}

// schemaOnly reports whether every problem is the schema's: only then is
// a whole manifest built field by field, so the order of work and the
// rule against an agent's placeholder still refuse it whole.
func schemaOnly(ps []engine.Problem) bool {
	for _, p := range ps {
		if p.Keyword == "" {
			return false
		}
	}
	return len(ps) > 0
}

// applyFields writes fields on a draft as every agent write is applied
// (engine.WriteFields), lists merged, and words each refusal with the
// fields that go there.
func applyFields(c call, set, kind, id string, put map[string]any, unset []string) ([]engine.Problem, []string, error) {
	refused, created, err := c.o.Engine.WriteFields(c.ctx, set, kind, id, put, unset, true)
	if err != nil {
		return nil, nil, withFields(c, kind, err)
	}
	return taught(c, kind, refused), created, nil
}

// taught is refused problems, each with the fields that go there.
func taught(c call, kind string, refused []engine.Problem) []engine.Problem {
	if len(refused) == 0 {
		return nil
	}
	if t, ok := withFields(c, kind, &engine.ValidationError{Problems: refused}).(*engine.ValidationError); ok {
		return t.Problems
	}
	return refused
}
