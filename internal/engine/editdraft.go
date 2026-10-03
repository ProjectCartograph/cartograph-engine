package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// ErrBadEdit is a draft edit that names no field it could set: a path
// that is not a JSON pointer, or that runs through a value holding none.
var ErrBadEdit = errors.New("bad draft edit")

// EditDraft sets and clears single fields of a manifest's draft, by JSON
// pointer, and returns the draft as it then stands. Saving a whole
// manifest replaces the draft with the saver's copy, so anything someone
// else changed since that copy was read would be undone; an edit touches
// only the fields it names. With shared drafts it is made inside the
// draft's own lock, so a change a person made a moment before is kept,
// and the draft returned holds it, for the agent to build on. A manifest
// with no draft and no version yet starts as one, with only its id.
func (e *Engine) EditDraft(ctx context.Context, kind, id string, set map[string]any, unset []string, actor string) ([]byte, error) {
	apply := func(doc map[string]any) error { return applyEdit(doc, id, set, unset) }
	if e.shared != nil {
		content, err := e.shared.Update(ctx, kind, id, apply, "edited by "+actor)
		if err == nil {
			text, err := e.codec.Encode(content)
			if err != nil {
				return nil, err
			}
			return text, e.PutWorking(ctx, kind, id, text)
		}
		if !errors.Is(err, store.ErrNoDocument) {
			return nil, err
		}
	}
	var doc map[string]any
	if text, ok, err := e.GetWorking(ctx, kind, id); err != nil {
		return nil, err
	} else if ok {
		if doc, err = e.codec.Decode(text); err != nil {
			return nil, err
		}
	} else if v, err := e.Get(ctx, kind, id); err == nil {
		if doc, err = e.codec.Decode(v.YAML); err != nil {
			return nil, err
		}
	} else if errors.Is(err, ErrNotFound) {
		doc = map[string]any{"apiVersion": "cartograph/v1", "kind": kind, "metadata": map[string]any{"id": id}, "spec": map[string]any{}}
	} else {
		return nil, err
	}
	if err := apply(doc); err != nil {
		return nil, err
	}
	text, err := e.codec.Encode(doc)
	if err != nil {
		return nil, err
	}
	return text, e.SaveWorking(ctx, kind, id, text, actor)
}

// Update applies fn to a manifest's shared draft under the draft's lock,
// as one change, and returns what it then holds. store.ErrNoDocument
// when the manifest has no shared draft yet.
func (s *Shared) Update(ctx context.Context, kind, id string, fn func(map[string]any) error, message string) (map[string]any, error) {
	docID, err := s.docs.DocumentFor(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	sd, err := s.load(ctx, docID)
	if err != nil {
		return nil, err
	}
	defer sd.mu.Unlock()
	content, err := sd.doc.JSON()
	if err != nil {
		return nil, err
	}
	if err := fn(content); err != nil {
		return nil, err
	}
	changed, err := sd.doc.Reconcile(content, s.e.Shape(sd.kind), crdt.Change{Message: message})
	if err != nil {
		return nil, err
	}
	if changed {
		if err := s.store(ctx, docID, sd); err != nil {
			return nil, err
		}
	}
	return sd.doc.JSON()
}

// pointerTokens splits a JSON pointer (RFC 6901) into its tokens.
func pointerTokens(p string) ([]string, error) {
	if p == "" || p[0] != '/' {
		return nil, fmt.Errorf("%w: %q is not a JSON pointer", ErrBadEdit, p)
	}
	parts := strings.Split(p[1:], "/")
	for i, t := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(t, "~1", "/"), "~0", "~")
	}
	return parts, nil
}

// pointerSet puts v at p in doc, making the objects on the way. In a
// list, a number replaces that item and "-" appends one.
func pointerSet(doc map[string]any, p string, v any) error {
	tokens, err := pointerTokens(p)
	if err != nil {
		return err
	}
	var parent any = doc
	for i, t := range tokens {
		last := i == len(tokens)-1
		switch c := parent.(type) {
		case map[string]any:
			if last {
				c[t] = v
				return nil
			}
			next, ok := c[t]
			if !ok || next == nil {
				next = map[string]any{}
				c[t] = next
			}
			parent = next
		case []any:
			if t == "-" && last {
				return setInParent(doc, tokens[:i], append(c, v))
			}
			n, err := strconv.Atoi(t)
			if err != nil || n < 0 || n >= len(c) {
				return fmt.Errorf("%w: %s: no item %s", ErrBadEdit, p, t)
			}
			if last {
				c[n] = v
				return nil
			}
			parent = c[n]
		default:
			return fmt.Errorf("%w: %s: %s holds no fields", ErrBadEdit, p, "/"+strings.Join(tokens[:i], "/"))
		}
	}
	return nil
}

// setInParent replaces the value at tokens, for a list that grew.
func setInParent(doc map[string]any, tokens []string, v any) error {
	p := ""
	for _, t := range tokens {
		p += "/" + strings.ReplaceAll(strings.ReplaceAll(t, "~", "~0"), "/", "~1")
	}
	return pointerSet(doc, p, v)
}

// pointerUnset removes the field or list item at p; nothing there is no
// error.
func pointerUnset(doc map[string]any, p string) error {
	tokens, err := pointerTokens(p)
	if err != nil {
		return err
	}
	var parent any = doc
	for i, t := range tokens {
		last := i == len(tokens)-1
		switch c := parent.(type) {
		case map[string]any:
			if last {
				delete(c, t)
				return nil
			}
			parent = c[t]
		case []any:
			n, err := strconv.Atoi(t)
			if err != nil || n < 0 || n >= len(c) {
				return nil
			}
			if last {
				return setInParent(doc, tokens[:i], append(append([]any{}, c[:n]...), c[n+1:]...))
			}
			parent = c[n]
		default:
			return nil
		}
	}
	return nil
}

// applyEdit sets and clears fields of a manifest by JSON pointer, parents
// before children, and refuses a change of its id.
func applyEdit(doc map[string]any, id string, set map[string]any, unset []string) error {
	// Parents before children, so a field and one inside it can be
	// set in one edit.
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		return len(paths[i]) < len(paths[j]) || len(paths[i]) == len(paths[j]) && paths[i] < paths[j]
	})
	for _, p := range paths {
		if err := pointerSet(doc, p, set[p]); err != nil {
			return err
		}
	}
	for _, p := range unset {
		if err := pointerUnset(doc, p); err != nil {
			return err
		}
	}
	meta, _ := doc["metadata"].(map[string]any)
	if meta == nil || meta["id"] != id {
		return fmt.Errorf("%w: metadata.id is %s's own and cannot change", ErrBadEdit, id)
	}
	return nil
}
