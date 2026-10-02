// Package access is the Authorizer for an access list (docs/adr/0011,
// TAXONOMY.md D27): four roles, each a bundle of permissions, and teams
// that scope a contributor's work.
//
// The policy is pure. Grants says what a principal holds; the engine
// puts the chains of teams a manifest sits under, and the fields a
// write changes, on the Action. Nothing here reads a store.
package access

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
)

// Policy decides from what Grants says a principal holds.
type Policy struct {
	// Grants returns what the access list gives a principal. An error is
	// a refusal.
	Grants func(ctx context.Context, p auth.Principal) (auth.Grants, error)
}

var (
	_ auth.Authorizer = Policy{}
	_ auth.Scoper     = Policy{}
)

// teamKinds are the kinds a team owns: a contributor writes those of
// their teams and of the teams beneath them.
var teamKinds = []string{"Project", "Programme", "Operation", "DataSource"}

// registers are the shared registers every contributor keeps.
var registers = []string{
	"Assumption", "BeneficiaryGroup", "FundingSource", "Gap", "KPI", "KPIReadings",
	"ReportingCycle", "Resource", "Segment", "StakeholderMap", "Unit",
}

// strategyFields are the Settings fields a strategy editor may change:
// the vision and mission, and the names of the goal levels.
var strategyFields = []string{"goalLevels", "purpose"}

// Authorize refuses whoever is not listed or holds no role, lets every
// role read all but the access list, and decides writes by role, kind,
// team and field.
func (p Policy) Authorize(ctx context.Context, pr auth.Principal, a auth.Action) error {
	// Anyone the proxy let through may ask who they are, so an interface
	// can tell an unlisted person why it shows them nothing.
	if a.Resource == auth.ResourceSession {
		return nil
	}
	g, err := p.grants(ctx, pr)
	if err != nil {
		return err
	}
	// An agent acts only for someone whose roles allow agents and whose
	// agents an administrator has not turned off (docs/adr/0016).
	if pr.Agent != "" || a.Resource == auth.ResourceAgent {
		if !g.Agents {
			return fmt.Errorf("%w: agents are not enabled for you", auth.ErrForbidden)
		}
		if a.Resource == auth.ResourceAgent {
			return nil
		}
	}
	// The access list holds everyone's address and what they hold:
	// administrators read it, nobody else.
	if a.Verb == auth.VerbRead && a.Resource != auth.ResourceAccess {
		return nil
	}
	return decide(g, a)
}

// Scope says how much of kind the principal may write, for an interface
// to offer only that.
func (p Policy) Scope(ctx context.Context, pr auth.Principal, kind string) (auth.Scope, error) {
	g, err := p.grants(ctx, pr)
	if errors.Is(err, auth.ErrForbidden) {
		return auth.ScopeNone, nil
	}
	if err != nil {
		return auth.ScopeNone, err
	}
	switch {
	case has(g, auth.RoleAdministrator):
		return auth.ScopeAll, nil
	case slices.Contains(teamKinds, kind) && has(g, auth.RoleContributor):
		return auth.ScopeTeams, nil
	case slices.Contains(registers, kind) && has(g, auth.RoleContributor):
		return auth.ScopeAll, nil
	case (kind == "Goal" || kind == "Settings") && has(g, auth.RoleStrategyEditor):
		return auth.ScopeAll, nil
	}
	return auth.ScopeNone, nil
}

// grants returns what the principal holds, or a refusal that says why
// they hold nothing.
func (p Policy) grants(ctx context.Context, pr auth.Principal) (auth.Grants, error) {
	if pr.Anonymous {
		return auth.Grants{}, fmt.Errorf("%w: sign in first", auth.ErrForbidden)
	}
	g, err := p.Grants(ctx, pr)
	if err != nil {
		return auth.Grants{}, err
	}
	who := pr.Email
	if who == "" {
		who = pr.Subject
	}
	if !g.Listed {
		return auth.Grants{}, fmt.Errorf("%w: %s is not on the access list", auth.ErrForbidden, who)
	}
	if len(g.Roles) == 0 {
		return auth.Grants{}, fmt.Errorf("%w: %s holds no role", auth.ErrForbidden, who)
	}
	return g, nil
}

// decide is the write rule.
func decide(g auth.Grants, a auth.Action) error {
	if has(g, auth.RoleAdministrator) {
		return nil
	}
	switch {
	case a.Resource != "":
		return refuse("only an administrator may change %s", a.Resource)
	case a.Kind == "":
		return refuse("only an administrator may do that")
	case slices.Contains(teamKinds, a.Kind):
		if !has(g, auth.RoleContributor) {
			return refuse("only a contributor may change a %s", a.Kind)
		}
		return teamsAllow(g, a)
	case slices.Contains(registers, a.Kind):
		if !has(g, auth.RoleContributor) {
			return refuse("only a contributor may change a %s", a.Kind)
		}
		return nil
	case a.Kind == "Goal":
		if !has(g, auth.RoleStrategyEditor) {
			return refuse("only a strategy editor may change a goal, objective or outcome")
		}
		return nil
	case a.Kind == "Settings":
		if !has(g, auth.RoleStrategyEditor) {
			return refuse("only an administrator may change the settings")
		}
		if a.Change != nil {
			for _, f := range a.Change.Fields {
				if !slices.Contains(strategyFields, f) {
					return refuse("a strategy editor may change the vision, mission and level names, not %s", f)
				}
			}
		}
		return nil
	}
	return refuse("only an administrator may change a %s", a.Kind)
}

// teamsAllow lets a contributor change work that one of their teams owns,
// directly or through a team beneath it, before and after the change. A
// side with no chain does not restrict: a new manifest has no owner yet,
// and a draft may not name its team until later. The HTTP layer, which
// cannot see the content, sets no Change; the engine decides with it.
func teamsAllow(g auth.Grants, a auth.Action) error {
	if a.Change == nil {
		return nil
	}
	if len(a.Change.TeamsBefore) > 0 && !overlaps(a.Change.TeamsBefore, g.Teams) {
		return refuse("this %s belongs to a team you do not act for", a.Kind)
	}
	if len(a.Change.TeamsAfter) > 0 && !overlaps(a.Change.TeamsAfter, g.Teams) {
		return refuse("you may give a %s only to a team you act for", a.Kind)
	}
	return nil
}

func has(g auth.Grants, role string) bool { return slices.Contains(g.Roles, role) }

func overlaps(chain, teams []string) bool {
	for _, t := range chain {
		if slices.Contains(teams, t) {
			return true
		}
	}
	return false
}

func refuse(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{auth.ErrForbidden}, args...)...)
}
