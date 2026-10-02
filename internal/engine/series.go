package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// A series is an array under spec that a kind's schema marks
// x-cartograph-series, naming the field that keys its items: a KPI's
// readings, keyed by period. The current series lives whole in the
// manifest (copy-on-write: charts and rules read it whole). Every item
// recorded is also kept as an append-only row, from which a period's
// value at any time is merged on read (docs/adr/0013).

// seriesRule is one series of a kind.
type seriesRule struct {
	field string // the property under spec
	key   string // the item field that keys it
}

func (r seriesRule) pointer() string { return "/spec/" + r.field }

// ErrNoSeries is a series a kind does not have.
var ErrNoSeries = errors.New("no such series")

// collectSeriesRules reads the series a kind's schema marks. Series are
// properties of spec itself; the schemas put none deeper.
func collectSeriesRules(raw map[string]map[string]any, schemaFile string) []seriesRule {
	props, _ := dig(raw[schemaFile], "properties", "spec", "properties").(map[string]any)
	var out []seriesRule
	for name, p := range props {
		pm, _ := p.(map[string]any)
		if key, ok := pm["x-cartograph-series"].(string); ok {
			out = append(out, seriesRule{field: name, key: key})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].field < out[j].field })
	return out
}

func dig(v any, path ...string) any {
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}

// keyOf is an item's key as text, or false when it has none.
func keyOf(item any, field string) (string, bool) {
	m, ok := item.(map[string]any)
	if !ok {
		return "", false
	}
	switch k := m[field].(type) {
	case string:
		return k, k != ""
	case nil:
		return "", false
	default:
		return fmt.Sprint(k), true
	}
}

// storedDoc is the document a store that keeps series items keeps for a
// version: the manifest without its series, which live in their rows. A
// store without series items keeps the whole manifest.
func (e *Engine) storedDoc(kind string, doc map[string]any) []byte {
	rules := e.seriesRules[kind]
	if _, ok := e.manifests.(store.SeriesStore); !ok || len(rules) == 0 {
		return docJSON(doc)
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return docJSON(doc)
	}
	trimmedSpec := make(map[string]any, len(spec))
	for k, v := range spec {
		trimmedSpec[k] = v
	}
	for _, r := range rules {
		delete(trimmedSpec, r.field)
	}
	trimmed := make(map[string]any, len(doc))
	for k, v := range doc {
		trimmed[k] = v
	}
	trimmed["spec"] = trimmedSpec
	return docJSON(trimmed)
}

// recordSeries records, in the save's transaction, every series item the
// save adds, changes or removes: one row each, whatever the length of
// the series.
func (e *Engine) recordSeries(ctx context.Context, tx store.ManifestStore, v Version, doc map[string]any) error {
	rules := e.seriesRules[v.Kind]
	ss, ok := tx.(store.SeriesStore)
	if !ok || len(rules) == 0 {
		return nil
	}
	spec, _ := doc["spec"].(map[string]any)
	var items []store.SeriesItem
	for _, r := range rules {
		was, err := ss.SeriesAsOf(ctx, v.Kind, r.pointer(), []string{v.ID}, v.On)
		if err != nil {
			return err
		}
		before := map[string][]byte{}
		for _, it := range was[v.ID] {
			before[it.Key] = it.Item
		}
		now := map[string]bool{}
		arr, _ := spec[r.field].([]any)
		for _, el := range arr {
			key, ok := keyOf(el, r.key)
			if !ok {
				continue
			}
			now[key] = true
			item := docJSON(el.(map[string]any))
			if prev, ok := before[key]; ok && sameDoc(prev, item) {
				continue
			}
			items = append(items, store.SeriesItem{Kind: v.Kind, ID: v.ID, Series: r.pointer(), Key: key,
				Item: item, RecordedAt: v.On, RecordedBy: v.Actor, Reason: v.Reason})
		}
		for key := range before {
			if !now[key] {
				items = append(items, store.SeriesItem{Kind: v.Kind, ID: v.ID, Series: r.pointer(), Key: key,
					RecordedAt: v.On, RecordedBy: v.Actor, Reason: v.Reason})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return ss.RecordSeries(ctx, items)
}

// sameDoc reports whether two JSON documents hold the same value.
func sameDoc(a, b []byte) bool {
	x, errA := decodeAny(a)
	y, errB := decodeAny(b)
	return errA == nil && errB == nil && reflect.DeepEqual(x, y)
}

// withSeries puts back the series a stored document left out, as they
// stood at the version's time. A document that still holds a series (one
// saved before its items were kept) is left as it is.
func (e *Engine) withSeries(ctx context.Context, v Version, doc map[string]any) error {
	rules := e.seriesRules[v.Kind]
	ss, ok := e.manifests.(store.SeriesStore)
	if !ok || len(rules) == 0 {
		return nil
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil
	}
	for _, r := range rules {
		if _, has := spec[r.field]; has {
			continue
		}
		got, err := ss.SeriesAsOf(ctx, v.Kind, r.pointer(), []string{v.ID}, v.On)
		if err != nil {
			return err
		}
		if len(got[v.ID]) == 0 {
			continue
		}
		arr := make([]any, 0, len(got[v.ID]))
		for _, it := range got[v.ID] {
			item, err := decodeAny(it.Item)
			if err != nil {
				return err
			}
			arr = append(arr, item)
		}
		spec[r.field] = arr
	}
	return nil
}

// AppendSeriesItem records one item of a series and commits the manifest
// with it, on its latest committed version: a new key in key order, or
// in place of the item with the same key. The caller sends one item, not
// the series.
func (e *Engine) AppendSeriesItem(ctx context.Context, kind, id, series string, item map[string]any, actor, reason string) (Version, error) {
	var rule *seriesRule
	for _, r := range e.seriesRules[kind] {
		if r.field == series {
			rule = &r
			break
		}
	}
	if rule == nil {
		return Version{}, fmt.Errorf("%w: %s has no series %q", ErrNoSeries, kind, series)
	}
	key, ok := keyOf(item, rule.key)
	if !ok {
		return Version{}, &ValidationError{Problems: []Problem{{Path: "/item/" + rule.key, Message: "An item needs its " + rule.key + "."}}}
	}
	latest, err := latestNumber(ctx, e.manifests, kind, id)
	if err != nil {
		return Version{}, err
	}
	if latest == 0 {
		return Version{}, fmt.Errorf("%w: %s/%s has no saved version to record into", ErrNotFound, kind, id)
	}
	v, err := e.GetVersion(ctx, kind, id, latest)
	if err != nil {
		return Version{}, err
	}
	doc, err := e.codec.Decode(v.YAML)
	if err != nil {
		return Version{}, err
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		spec = map[string]any{}
		doc["spec"] = spec
	}
	arr, _ := spec[rule.field].([]any)
	placed := false
	for i, el := range arr {
		if k, ok := keyOf(el, rule.key); ok && k == key {
			arr[i], placed = item, true
			break
		}
	}
	if !placed {
		at := len(arr)
		for i, el := range arr {
			if k, ok := keyOf(el, rule.key); ok && k > key {
				at = i
				break
			}
		}
		arr = append(arr[:at], append([]any{item}, arr[at:]...)...)
	}
	spec[rule.field] = arr
	text, err := e.codec.Encode(doc)
	if err != nil {
		return Version{}, err
	}
	return e.Commit(ctx, kind, id, text, actor, reason)
}

// SeriesAsOf returns a manifest's series items as they stood at at (the
// zero time for now), for every manifest of the kind when ids is nil.
// On a store without series items, the items are read out of the current
// manifests, recorded as of their version.
func (e *Engine) SeriesAsOf(ctx context.Context, kind, series string, ids []string, at time.Time) (map[string][]store.SeriesItem, error) {
	var rule *seriesRule
	for _, r := range e.seriesRules[kind] {
		if r.field == series {
			rule = &r
			break
		}
	}
	if rule == nil {
		return nil, fmt.Errorf("%w: %s has no series %q", ErrNoSeries, kind, series)
	}
	ss, hasRows := e.manifests.(store.SeriesStore)
	if hasRows && !at.IsZero() {
		// A past time: only the rows know it.
		return ss.SeriesAsOf(ctx, kind, rule.pointer(), ids, at)
	}
	// Now: the values in force are the current manifests' (copy-on-write,
	// and right for a series saved before its items were kept); the rows
	// say who recorded each and when, where they agree.
	var recorded map[string][]store.SeriesItem
	if hasRows {
		var err error
		if recorded, err = ss.SeriesAsOf(ctx, kind, rule.pointer(), ids, time.Time{}); err != nil {
			return nil, err
		}
	}
	var versions []Version
	var err error
	if ids == nil {
		versions, err = currentOfKind(ctx, e.manifests, kind)
	} else {
		versions, err = e.GetMany(ctx, kind, ids)
	}
	if err != nil {
		return nil, err
	}
	out := map[string][]store.SeriesItem{}
	for _, v := range versions {
		doc, err := e.codec.Decode(v.YAML)
		if err != nil {
			continue
		}
		spec, _ := doc["spec"].(map[string]any)
		arr, _ := spec[rule.field].([]any)
		for _, el := range arr {
			key, ok := keyOf(el, rule.key)
			if !ok {
				continue
			}
			it := store.SeriesItem{Kind: kind, ID: v.ID, Series: rule.pointer(), Key: key,
				Item: docJSON(el.(map[string]any)), RecordedAt: v.On, RecordedBy: v.Actor, Reason: v.Reason}
			for _, r := range recorded[v.ID] {
				if r.Key == key && sameDoc(r.Item, it.Item) {
					it.RecordedAt, it.RecordedBy, it.Reason = r.RecordedAt, r.RecordedBy, r.Reason
				}
			}
			out[v.ID] = append(out[v.ID], it)
		}
		sort.Slice(out[v.ID], func(i, j int) bool { return out[v.ID][i].Key < out[v.ID][j].Key })
	}
	return out, nil
}

// Events returns what happened after a cursor, oldest first, from the
// store's event log. A store without one has none to give.
func (e *Engine) Events(ctx context.Context, after int64, limit int) ([]store.Event, error) {
	el, ok := e.manifests.(store.EventLog)
	if !ok {
		return []store.Event{}, nil
	}
	return el.Events(ctx, after, max(1, min(limit, 1000)))
}

// decodeAny reads a JSON document.
func decodeAny(b []byte) (any, error) {
	var v any
	err := json.Unmarshal(b, &v)
	return v, err
}

// ReconcileSeries brings the series items of manifests in line with
// their latest saved versions, recording what the rows lack: the whole
// series of a manifest saved before its items were kept, and what a
// replica of an earlier release saved during a rolling upgrade. ids
// limits it to some manifests of each kind; nil is all of them. It
// returns how many items it recorded.
func (e *Engine) ReconcileSeries(ctx context.Context, ids map[string][]string) (int, error) {
	if _, ok := e.manifests.(store.SeriesStore); !ok {
		return 0, nil
	}
	total := 0
	for kind, rules := range e.seriesRules {
		if len(rules) == 0 {
			continue
		}
		want := ids[kind]
		if ids != nil && len(want) == 0 {
			continue
		}
		if want == nil {
			all, err := e.manifests.ListIDs(ctx, kind)
			if err != nil {
				return total, err
			}
			want = all
		}
		for _, id := range want {
			latest, err := latestNumber(ctx, e.manifests, kind, id)
			if err != nil || latest == 0 {
				continue
			}
			v, err := e.GetVersion(ctx, kind, id, latest)
			if err != nil {
				return total, err
			}
			doc, err := e.codec.Decode(v.YAML)
			if err != nil {
				continue
			}
			counted := countingSeries{ManifestStore: e.manifests, SeriesStore: e.manifests.(store.SeriesStore)}
			if err := e.recordSeries(ctx, &counted, v, doc); err != nil {
				return total, err
			}
			total += counted.n
		}
	}
	return total, nil
}

// countingSeries counts the items recorded through it.
type countingSeries struct {
	store.ManifestStore
	store.SeriesStore
	n int
}

func (c *countingSeries) RecordSeries(ctx context.Context, items []store.SeriesItem) error {
	c.n += len(items)
	return c.SeriesStore.RecordSeries(ctx, items)
}

// SeriesKinds are the kinds that have a series.
func (e *Engine) SeriesKinds() []string {
	var out []string
	for kind, rules := range e.seriesRules {
		if len(rules) > 0 {
			out = append(out, kind)
		}
	}
	sort.Strings(out)
	return out
}
