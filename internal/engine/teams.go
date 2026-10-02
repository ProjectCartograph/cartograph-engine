package engine

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
)

// Every write is put to the authorizer again here, with what it does to
// the manifest (docs/adr/0011): the chains of teams the manifest sits
// under before and after, and the spec fields it changes. The HTTP layer
// asked first, without the content.

// WithAuthorizer makes the engine ask z at every write, with the change
// the write makes. The HTTP layer has asked z already, without the
// content; this is where a policy that decides by content decides.
func WithAuthorizer(z identity.Authorizer) Option { return func(e *Engine) { e.authz = z } }

// teamKinds name, for each kind a team owns, the spec field naming the
// owner. A programme belongs to its lead team.
var teamKinds = map[string]string{
	"Project":    "team",
	"Programme":  "leadTeam",
	"Operation":  "team",
	"DataSource": "team",
}

// teamCacheFor is how long a replica trusts the team tree it read. Teams
// change rarely and a write on this replica refreshes it at once.
const teamCacheFor = 30 * time.Second

// teamTree is every team: its name and the team above it.
type teamTree struct {
	name   map[string]string // id to name
	parent map[string]string // id to parent id
	byName map[string]string // name to id
}

type teamCache struct {
	mu   sync.Mutex
	tree *teamTree
	at   time.Time
}

func (e *Engine) teams(ctx context.Context) (*teamTree, error) {
	e.teamCache.mu.Lock()
	defer e.teamCache.mu.Unlock()
	if e.teamCache.tree != nil && timeNow().Sub(e.teamCache.at) < teamCacheFor {
		return e.teamCache.tree, nil
	}
	// Every team in one read.
	versions, err := currentOfKind(ctx, e.manifests, "Team")
	if err != nil {
		return nil, err
	}
	t := &teamTree{name: map[string]string{}, parent: map[string]string{}, byName: map[string]string{}}
	for _, v := range versions {
		doc, err := e.codec.Decode(v.YAML)
		if err != nil {
			continue
		}
		spec, _ := doc["spec"].(map[string]any)
		meta, _ := doc["metadata"].(map[string]any)
		name, _ := meta["name"].(string)
		if name == "" {
			// A team saved before 2.6.0 may name itself in spec.name only.
			name, _ = spec["name"].(string)
		}
		t.name[v.ID] = name
		t.byName[name] = v.ID
		if p, _ := spec["parent"].(string); p != "" {
			t.parent[v.ID] = p
		}
	}
	e.teamCache.tree, e.teamCache.at = t, timeNow()
	return t, nil
}

// forgetTeams drops the cached tree after a write to a team.
func (e *Engine) forgetTeams(kind string) {
	if kind != "Team" {
		return
	}
	e.teamCache.mu.Lock()
	e.teamCache.tree = nil
	e.teamCache.mu.Unlock()
}

// chain returns team and every team above it, nearest first.
func (t *teamTree) chain(team string) []string {
	var out []string
	for team != "" && !slices.Contains(out, team) && len(out) < 64 {
		out = append(out, team)
		team = t.parent[team]
	}
	return out
}

// teamChain returns the chain of teams the manifest doc sits under: its
// owning team and every team above it. Nil for a kind no team owns, or a
// manifest that names no team.
func (e *Engine) teamChain(ctx context.Context, kind string, doc map[string]any) ([]string, error) {
	field, ok := teamKinds[kind]
	if !ok || doc == nil {
		return nil, nil
	}
	spec, _ := doc["spec"].(map[string]any)
	team, _ := spec[field].(string)
	if team == "" {
		return nil, nil
	}
	t, err := e.teams(ctx)
	if err != nil {
		return nil, err
	}
	return t.chain(team), nil
}

// TeamsBeneath returns teams and every team below any of them, sorted.
func (e *Engine) TeamsBeneath(ctx context.Context, teams []string) ([]string, error) {
	t, err := e.teams(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for id := range t.name {
		for _, c := range t.chain(id) {
			if slices.Contains(teams, c) {
				out = append(out, id)
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// TeamName returns a team's name, or its id when there is no such team.
func (e *Engine) TeamName(ctx context.Context, id string) string {
	if t, err := e.teams(ctx); err == nil && t.name[id] != "" {
		return t.name[id]
	}
	return id
}

// guard asks the authorizer whether the principal on ctx may change
// kind/id from before to after (nil before for a new manifest, nil after
// for a deletion). A context with no principal is the engine's own work
// (seeding, a refresh another replica asked for, the command line), which
// the request that caused it was already asked about.
func (e *Engine) guard(ctx context.Context, kind, id string, before, after map[string]any) error {
	if e.authz == nil {
		return nil
	}
	p := identity.PrincipalFrom(ctx)
	if p.Anonymous {
		return nil
	}
	tb, err := e.teamChain(ctx, kind, before)
	if err != nil {
		return err
	}
	ta, err := e.teamChain(ctx, kind, after)
	if err != nil {
		return err
	}
	change := &identity.Change{TeamsBefore: tb, TeamsAfter: ta, Fields: changedFields(before, after)}
	return e.authz.Authorize(ctx, p, identity.Action{Verb: identity.VerbWrite, Kind: kind, ID: id, Change: change})
}

// guardText is guard with the new content as text, against what is
// stored: the working copy, or the current version when there is none.
func (e *Engine) guardText(ctx context.Context, kind, id string, text []byte) error {
	if e.authz == nil || identity.PrincipalFrom(ctx).Anonymous {
		return nil
	}
	before, err := e.stored(ctx, kind, id)
	if err != nil {
		return err
	}
	var after map[string]any
	if text != nil {
		if after, err = e.codec.Decode(text); err != nil {
			// Text that does not parse cannot name a team; it is judged
			// as the manifest it replaces.
			after = before
		}
	}
	return e.guard(ctx, kind, id, before, after)
}

// guardDoc is guard with the new content already decoded (nil for a
// deletion), against what is stored.
func (e *Engine) guardDoc(ctx context.Context, kind, id string, after map[string]any) error {
	if e.authz == nil || identity.PrincipalFrom(ctx).Anonymous {
		return nil
	}
	before, err := e.stored(ctx, kind, id)
	if err != nil {
		return err
	}
	return e.guard(ctx, kind, id, before, after)
}

// guarding reports whether writes on ctx are put to the authorizer.
func (e *Engine) guarding(ctx context.Context) bool {
	return e.authz != nil && !identity.PrincipalFrom(ctx).Anonymous
}

// guardStored is guard for a write that does not change the content (a
// state move, a hand-off): the manifest as stored, on both sides.
func (e *Engine) guardStored(ctx context.Context, kind, id string) error {
	if e.authz == nil || identity.PrincipalFrom(ctx).Anonymous {
		return nil
	}
	doc, err := e.stored(ctx, kind, id)
	if err != nil {
		return err
	}
	return e.guard(ctx, kind, id, doc, doc)
}

// stored returns a manifest's content as it stands: the working copy, or
// the current version, or nil when it has neither.
func (e *Engine) stored(ctx context.Context, kind, id string) (map[string]any, error) {
	if text, found, err := e.manifests.GetWorking(ctx, kind, id); err != nil {
		return nil, err
	} else if found {
		if doc, err := e.codec.Decode(text); err == nil {
			return doc, nil
		}
	}
	v, found, err := e.manifests.GetCurrent(ctx, kind, id)
	if err != nil || !found {
		return nil, err
	}
	doc, err := e.codec.Decode(v.YAML)
	if err != nil {
		return nil, nil
	}
	return doc, nil
}

// changedFields returns the top-level spec fields whose values differ.
func changedFields(before, after map[string]any) []string {
	bs, _ := before["spec"].(map[string]any)
	as, _ := after["spec"].(map[string]any)
	var out []string
	for k, v := range as {
		if !reflect.DeepEqual(v, bs[k]) {
			out = append(out, k)
		}
	}
	for k := range bs {
		if _, ok := as[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
