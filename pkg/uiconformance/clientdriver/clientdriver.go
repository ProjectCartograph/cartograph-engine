// Package clientdriver is the reference Driver: it does what a person
// does, through a client.Client alone, walking the contract's flow. Run
// over the in-process client it proves the scenarios describe behaviour
// the engine guarantees; run over a transport it proves the transport
// carries that behaviour unchanged. When an interface's own driver
// fails a scenario this one passes, the interface is wrong, not the
// scenario.
package clientdriver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/pkg/client"
	"github.com/ProjectCartograph/cartograph-engine/v2/pkg/uiconformance"
)

// Driver holds one open flow.
type Driver struct {
	C client.Client

	kind, id string
	steps    []step
	stepIdx  int
	doc      map[string]any // the form as the person has filled it in
	problems []uiconformance.Problem
	opened   int
}

type step struct {
	key    string
	fields []string // paths asked for at this step
}

var _ uiconformance.Driver = (*Driver)(nil)

// New returns a driver over c.
func New(c client.Client) *Driver {
	return &Driver{C: c}
}

func (d *Driver) Open(ctx context.Context, kind, id string) (string, error) {
	d.kind, d.id, d.stepIdx, d.problems = kind, id, 0, nil
	d.steps = nil
	flow, found, err := d.C.Flow(ctx, kind)
	if err != nil {
		return "", err
	}
	if found {
		spec, _ := flow["spec"].(map[string]any)
		raw, _ := spec["steps"].([]any)
		for _, s := range raw {
			sm, _ := s.(map[string]any)
			st := step{key: fmt.Sprint(sm["key"])}
			fields, _ := sm["fields"].([]any)
			for _, f := range fields {
				fm, _ := f.(map[string]any)
				st.fields = append(st.fields, fmt.Sprint(fm["path"]))
			}
			d.steps = append(d.steps, st)
		}
	}
	if d.id == "" {
		d.opened++
		d.id = fmt.Sprintf("conformance-%d-%d", time.Now().UnixNano()%100000, d.opened)
	}
	d.doc = map[string]any{"apiVersion": "cartograph/v1", "kind": kind, "metadata": map[string]any{"id": d.id}, "spec": map[string]any{}}
	return d.id, nil
}

func (d *Driver) Step(_ context.Context, key string) error {
	if d.steps == nil {
		return fmt.Errorf("%s has no flow; it is a sheet with one step", d.kind)
	}
	for i, s := range d.steps {
		if s.key == key {
			d.stepIdx = i
			return nil
		}
	}
	return fmt.Errorf("no step %q in the %s flow", key, d.kind)
}

func (d *Driver) Set(_ context.Context, field uiconformance.Field, value any) error {
	path := string(field)
	if d.steps != nil && !d.fieldInFlow(path) {
		return fmt.Errorf("field %s is not asked for anywhere in the %s flow", path, d.kind)
	}
	return setPath(d.doc, path, value)
}

func (d *Driver) fieldInFlow(path string) bool {
	for _, s := range d.steps {
		for _, f := range s.fields {
			if path == f || strings.HasPrefix(path, f+"/") {
				return true
			}
		}
	}
	return path == "/metadata/name"
}

func (d *Driver) Act(ctx context.Context, action uiconformance.Action, reason string) error {
	switch action {
	case uiconformance.ActNext, uiconformance.ActBack:
		if action == uiconformance.ActNext && d.stepIdx < len(d.steps)-1 {
			d.stepIdx++
		}
		if action == uiconformance.ActBack && d.stepIdx > 0 {
			d.stepIdx--
		}
		return nil
	case uiconformance.ActSave:
		return d.outcome(d.C.SaveWorking(ctx, d.kind, d.id, d.doc))
	case uiconformance.ActSaveVersion:
		_, err := d.C.SaveVersion(ctx, d.kind, d.id, d.doc, reason)
		return d.outcome(err)
	case uiconformance.ActDiscard:
		return fmt.Errorf("discard: not carried by the client port yet")
	case uiconformance.ActApply:
		return fmt.Errorf("apply: not carried by the client port yet")
	}
	return fmt.Errorf("unknown action %q", action)
}

// outcome turns the client's answer into what an interface shows: a
// clean save clears the problems; a refusal shows them at their fields.
// Anything else is the driver failing, not the person being refused.
func (d *Driver) outcome(err error) error {
	var refused *client.Refused
	switch {
	case err == nil:
		d.problems = nil
		return nil
	case errors.As(err, &refused):
		d.problems = d.problems[:0]
		for _, p := range refused.Problems {
			d.problems = append(d.problems, uiconformance.Problem{Field: uiconformance.Field(p.Path), Message: p.Message})
		}
		return nil
	default:
		return err
	}
}

func (d *Driver) Problems(context.Context) ([]uiconformance.Problem, error) {
	return append([]uiconformance.Problem(nil), d.problems...), nil
}

func (d *Driver) Where(context.Context) (uiconformance.Location, error) {
	loc := uiconformance.Location{Kind: d.kind, ID: d.id}
	if d.steps != nil {
		loc.Step = d.steps[d.stepIdx].key
	}
	return loc, nil
}

func (d *Driver) Close(context.Context) error { return nil }

// setPath sets value at a JSON pointer in doc, making the objects on the
// way. A segment written {key} names the item of a list whose id is key,
// as a scenario addresses items, and adds the item when it is new.
func setPath(doc map[string]any, path string, value any) error {
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var node any = doc
	for i, seg := range segs {
		last := i == len(segs)-1
		seg = strings.NewReplacer("~1", "/", "~0", "~").Replace(seg)
		obj, ok := node.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: %s is not an object", path, strings.Join(segs[:i], "/"))
		}
		if last {
			obj[seg] = value
			return nil
		}
		next := segs[i+1]
		if strings.HasPrefix(next, "{") && strings.HasSuffix(next, "}") {
			key := next[1 : len(next)-1]
			list, _ := obj[seg].([]any)
			var item map[string]any
			for _, e := range list {
				if m, ok := e.(map[string]any); ok && fmt.Sprint(m["id"]) == key {
					item = m
				}
			}
			if item == nil {
				item = map[string]any{"id": key}
				obj[seg] = append(list, item)
			}
			if i+1 == len(segs)-1 {
				return fmt.Errorf("%s: a list item is set field by field", path)
			}
			return setPath(item, "/"+strings.Join(segs[i+2:], "/"), value)
		}
		child, ok := obj[seg].(map[string]any)
		if !ok {
			child = map[string]any{}
			obj[seg] = child
		}
		node = child
	}
	return nil
}
