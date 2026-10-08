package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/document"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Access by role and team (docs/adr/0011, TAXONOMY.md D27): the engine
// keeps the access list, enrols people from the directory at sign-in,
// and asks the authorizer again at every write with what the write does
// to the manifest's teams and fields.

// Directory maps an organisation's directory groups onto Cartograph's
// roles and teams. The composition root reads it from a file; the engine
// knows no syntax.
type Directory struct {
	// Roles names, for each role, the groups that grant it.
	Roles map[string][]string
	// Teams are the groups that are teams, parents before children.
	Teams []DirectoryTeam
	// Agents names the roles whose people may act through an agent
	// (docs/adr/0016). None listed, no agents.
	Agents []string
}

// DirectoryTeam is one group that is a team.
type DirectoryTeam struct {
	// Group is the group's name as the directory reports it.
	Group string
	// Name is the team's name in Cartograph; the group's when empty.
	Name string
	// Parent is the name of the team above it, if any.
	Parent string
}

// WithAccess keeps the access list in s and enrols people by d.
func WithAccess(s store.AccessStore, d Directory) Option {
	return func(e *Engine) { e.access, e.directory = s, d }
}

// ErrSelf refuses an administrator's change to their own access that
// would lock them out.
var ErrSelf = errors.New("you may not remove your own access")

// signInEvery is how stale a recorded sign-in may get before a request
// records it again; nothing else is written while nothing changes.
const signInEvery = 10 * time.Minute

// directoryGrants are the roles and teams p's groups give, by the
// mapping. Teams are ids; a mapped team that does not exist yet gives
// nothing until `cartograph access apply` creates it.
func (e *Engine) directoryGrants(ctx context.Context, p identity.Principal) (roles, teams []string, err error) {
	for _, r := range identity.Roles {
		for _, g := range e.directory.Roles[r] {
			if slices.Contains(p.Roles, g) {
				roles = append(roles, r)
				break
			}
		}
	}
	if len(e.directory.Teams) == 0 {
		return roles, nil, nil
	}
	t, err := e.teams(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, dt := range e.directory.Teams {
		if !slices.Contains(p.Roles, dt.Group) {
			continue
		}
		if id := t.byName[dt.teamName()]; id != "" && !slices.Contains(teams, id) {
			teams = append(teams, id)
		}
	}
	sort.Strings(teams)
	return roles, teams, nil
}

func (dt DirectoryTeam) teamName() string {
	if dt.Name != "" {
		return dt.Name
	}
	return dt.Group
}

// emailOf is the address an access list knows p by.
func emailOf(p identity.Principal) string {
	if p.Email != "" {
		return strings.ToLower(p.Email)
	}
	if strings.Contains(p.Subject, "@") {
		return strings.ToLower(p.Subject)
	}
	return ""
}

// Grants returns what the access list gives p, recording the sign-in:
// who they are now and what their directory groups give them. A person
// not on the list is enrolled when their groups grant a role. A request
// writes nothing while nothing has changed, but for a refresh of the
// sign-in time every signInEvery.
func (e *Engine) Grants(ctx context.Context, p identity.Principal) (identity.Grants, error) {
	if e.access == nil {
		return identity.Grants{}, fmt.Errorf("%w: no access list is configured", identity.ErrForbidden)
	}
	email := emailOf(p)
	if p.Anonymous || email == "" {
		return identity.Grants{}, nil
	}
	if p.Delegated {
		return e.delegatedGrants(ctx, email)
	}
	dirRoles, dirTeams, err := e.directoryGrants(ctx, p)
	if err != nil {
		return identity.Grants{}, err
	}
	person, err := e.access.GetPerson(ctx, email)
	listed := err == nil
	if err != nil && !errors.Is(err, store.ErrNoPerson) {
		return identity.Grants{}, err
	}
	stale := !listed ||
		person.Name != p.Name || person.Subject != p.Subject ||
		!slices.Equal(person.DirectoryRoles, dirRoles) || !slices.Equal(person.DirectoryTeams, dirTeams) ||
		timeNow().Sub(person.LastSignedIn) > signInEvery
	if stale && (listed || len(dirRoles) > 0) {
		person, err = e.access.RecordSignIn(ctx, store.SignIn{
			Email: email, Name: p.Name, Subject: p.Subject,
			Roles: dirRoles, Teams: dirTeams, At: timeNow().UTC(), Enrol: len(dirRoles) > 0,
		})
		if errors.Is(err, store.ErrNoPerson) {
			return identity.Grants{}, nil // removed between the read and the write
		}
		if err != nil {
			return identity.Grants{}, err
		}
		listed = true
	}
	if !listed {
		return identity.Grants{}, nil
	}
	roles := unionRoles(person.Roles, dirRoles)
	agents := !person.AgentsOff && slices.ContainsFunc(roles, func(r string) bool { return slices.Contains(e.directory.Agents, r) })
	return identity.Grants{Listed: true, Roles: roles, Teams: union(person.Teams, dirTeams), Agents: agents}, nil
}

// delegatedFresh is how long the directory's roles, as recorded at a
// person's last sign-in, still count for their agents.
const delegatedFresh = 30 * 24 * time.Hour

// delegatedGrants are a person's grants for an agent acting on a stored
// grant: what an administrator gave them, and what the directory gave
// them at their last sign-in if that was recent. Nothing is recorded,
// since nobody signed in.
func (e *Engine) delegatedGrants(ctx context.Context, email string) (identity.Grants, error) {
	person, err := e.access.GetPerson(ctx, email)
	if errors.Is(err, store.ErrNoPerson) {
		return identity.Grants{}, nil
	}
	if err != nil {
		return identity.Grants{}, err
	}
	roles, teams := person.Roles, person.Teams
	if timeNow().Sub(person.LastSignedIn) <= delegatedFresh {
		roles, teams = unionRoles(person.Roles, person.DirectoryRoles), union(person.Teams, person.DirectoryTeams)
	}
	agents := !person.AgentsOff && slices.ContainsFunc(roles, func(r string) bool { return slices.Contains(e.directory.Agents, r) })
	return identity.Grants{Listed: true, Roles: roles, Teams: teams, Agents: agents}, nil
}

// People returns the access list.
func (e *Engine) People(ctx context.Context) ([]store.Person, error) {
	if e.access == nil {
		return nil, fmt.Errorf("%w: no access list is configured", ErrNotFound)
	}
	return e.access.ListPeople(ctx)
}

// GrantPerson lists a person by their address, if they are not listed,
// and sets the roles and teams an administrator gives them.
func (e *Engine) GrantPerson(ctx context.Context, email string, roles, teams []string, agentsOff bool, actor string) (store.Person, error) {
	if err := refuseAgent(ctx); err != nil {
		return store.Person{}, err
	}
	if e.access == nil {
		return store.Person{}, fmt.Errorf("%w: no access list is configured", ErrNotFound)
	}
	email = strings.ToLower(strings.TrimSpace(email))
	var problems []Problem
	if !strings.Contains(email, "@") {
		problems = append(problems, Problem{Path: "/email", Message: "an organisation address, such as someone@example.org"})
	}
	for _, r := range roles {
		if !slices.Contains(identity.Roles, r) {
			problems = append(problems, Problem{Path: "/roles", Message: fmt.Sprintf("%q is not a role", r)})
		}
	}
	t, err := e.teams(ctx)
	if err != nil {
		return store.Person{}, err
	}
	for _, id := range teams {
		if _, ok := t.name[id]; !ok {
			problems = append(problems, Problem{Path: "/teams", Message: fmt.Sprintf("there is no team %q", id)})
		}
	}
	if len(problems) > 0 {
		return store.Person{}, &ValidationError{Problems: problems}
	}
	if self := emailOf(identity.PrincipalFrom(ctx)); self == email && !slices.Contains(roles, identity.RoleAdministrator) {
		if cur, err := e.access.GetPerson(ctx, email); err == nil && slices.Contains(cur.Roles, identity.RoleAdministrator) {
			return store.Person{}, fmt.Errorf("%w: keep your administrator role, or ask another administrator", ErrSelf)
		}
	}
	return e.access.GrantPerson(ctx, store.Person{
		Email: email, Roles: unionRoles(roles, nil), Teams: union(teams, nil), AddedBy: actor, AddedOn: timeNow().UTC(), AgentsOff: agentsOff,
	})
}

// RemovePerson takes a person off the access list. An administrator may
// not remove themselves.
func (e *Engine) RemovePerson(ctx context.Context, email string) error {
	if err := refuseAgent(ctx); err != nil {
		return err
	}
	if e.access == nil {
		return fmt.Errorf("%w: no access list is configured", ErrNotFound)
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if emailOf(identity.PrincipalFrom(ctx)) == email {
		return ErrSelf
	}
	err := e.access.DeletePerson(ctx, email)
	if errors.Is(err, store.ErrNoPerson) {
		return fmt.Errorf("%w: %s is not on the access list", ErrNotFound, email)
	}
	return err
}

// ApplyDirectory creates every team the mapping names that does not
// exist yet, parents first, and returns the names it created. It never
// changes an existing team. Run it once per deployment, from one place
// (`cartograph access apply`), so two replicas never both create a team.
func (e *Engine) ApplyDirectory(ctx context.Context, actor string) ([]string, error) {
	if err := refuseAgent(ctx); err != nil {
		return nil, err
	}
	var created []string
	for _, dt := range e.directory.Teams {
		e.forgetTeams("Team")
		t, err := e.teams(ctx)
		if err != nil {
			return created, err
		}
		name := dt.teamName()
		if t.byName[name] != "" {
			continue
		}
		spec := map[string]any{}
		if dt.Parent != "" {
			parent := t.byName[dt.Parent]
			if parent == "" {
				return created, fmt.Errorf("team %q: its parent %q is not a team; list parents before their children", name, dt.Parent)
			}
			spec["parent"] = parent
		}
		// The interface's own rule: a short name is the id, a long one
		// a numbered id.
		base := document.Slug(name)
		if base == "" {
			base = "team"
		}
		id := freeID(base, t)
		doc := map[string]any{
			"apiVersion": "cartograph/v1",
			"kind":       "Team",
			"metadata":   map[string]any{"id": id, "name": name},
			"spec":       spec,
		}
		text, err := e.codec.Encode(doc)
		if err != nil {
			return created, err
		}
		if _, err := e.Commit(ctx, "Team", id, text, actor, "enrolled from the directory"); err != nil {
			return created, fmt.Errorf("team %q: %w", name, err)
		}
		created = append(created, name)
	}
	e.forgetTeams("Team")
	return created, nil
}

// freeID returns id, or id with the first number that makes it unused.
func freeID(id string, t *teamTree) string {
	if _, ok := t.name[id]; !ok {
		return id
	}
	for n := 2; ; n++ {
		c := fmt.Sprintf("%s-%d", id, n)
		if _, ok := t.name[c]; !ok {
			return c
		}
	}
}

// unionRoles returns the roles in a or b, in identity.Roles's order.
func unionRoles(a, b []string) []string {
	var out []string
	for _, r := range identity.Roles {
		if slices.Contains(a, r) || slices.Contains(b, r) {
			out = append(out, r)
		}
	}
	return out
}

// union returns the strings in a or b, sorted, once each.
func union(a, b []string) []string {
	out := slices.Concat(a, b)
	sort.Strings(out)
	return slices.Compact(out)
}

// KeepsAccess reports whether the engine keeps an access list.
func (e *Engine) KeepsAccess() bool { return e.access != nil }
