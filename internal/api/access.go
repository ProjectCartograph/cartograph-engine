package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// The access list (docs/adr/0011). The middleware has already let only
// an administrator this far; the engine checks what is granted.

func (s *Server) ListPeople(ctx context.Context, _ apigen.ListPeopleRequestObject) (apigen.ListPeopleResponseObject, error) {
	people, err := s.Engine.People(ctx)
	if errors.Is(err, engine.ErrNotFound) {
		return apigen.ListPeople404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(problems("this deployment keeps no access list"))}, nil
	}
	if err != nil {
		return nil, err
	}
	out := apigen.ListPeople200JSONResponse{People: make([]apigen.Person, len(people))}
	for i, p := range people {
		out.People[i] = toPerson(p)
	}
	return out, nil
}

func (s *Server) GrantPerson(ctx context.Context, req apigen.GrantPersonRequestObject) (apigen.GrantPersonResponseObject, error) {
	if req.Body == nil {
		return apigen.GrantPerson422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList([]engine.Problem{{Message: "roles and teams are required"}}))}, nil
	}
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	roles := make([]string, len(req.Body.Roles))
	for i, r := range req.Body.Roles {
		roles[i] = string(r)
	}
	p, err := s.Engine.GrantPerson(ctx, req.Email, roles, req.Body.Teams, actor)
	var invalid *engine.ValidationError
	switch {
	case errors.As(err, &invalid):
		return apigen.GrantPerson422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(invalid.Problems))}, nil
	case errors.Is(err, engine.ErrSelf):
		return apigen.GrantPerson409JSONResponse(problems(err.Error())), nil
	case errors.Is(err, engine.ErrNotFound):
		return apigen.GrantPerson404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(problems(err.Error()))}, nil
	case err != nil:
		return nil, err
	}
	return apigen.GrantPerson200JSONResponse(toPerson(p)), nil
}

func (s *Server) RemovePerson(ctx context.Context, req apigen.RemovePersonRequestObject) (apigen.RemovePersonResponseObject, error) {
	err := s.Engine.RemovePerson(ctx, req.Email)
	switch {
	case errors.Is(err, engine.ErrSelf):
		return apigen.RemovePerson409JSONResponse(problems(err.Error())), nil
	case errors.Is(err, engine.ErrNotFound):
		return apigen.RemovePerson404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(problems(err.Error()))}, nil
	case err != nil:
		return nil, err
	}
	return apigen.RemovePerson204Response{}, nil
}

// sessionAccess is what the access list gives the session's principal,
// or nil when the deployment keeps no list.
func (s *Server) sessionAccess(ctx context.Context, p auth.Principal) (*apigen.SessionAccess, error) {
	if !s.Engine.KeepsAccess() {
		return nil, nil
	}
	g, err := s.Engine.Grants(ctx, p)
	if err != nil {
		return nil, err
	}
	out := &apigen.SessionAccess{Listed: g.Listed, Roles: toRoles(g.Roles), Teams: orEmpty(g.Teams), Scopes: map[string]apigen.SessionAccessScopes{}}
	if out.Reach, err = s.Engine.TeamsBeneath(ctx, g.Teams); err != nil {
		return nil, err
	}
	out.Reach = orEmpty(out.Reach)
	scoper, _ := s.Authz.(auth.Scoper)
	for _, k := range s.Engine.Kinds() {
		scope := auth.ScopeNone
		if scoper != nil {
			if scope, err = scoper.Scope(ctx, p, k.Kind); err != nil {
				return nil, err
			}
		}
		out.Scopes[k.Kind] = apigen.SessionAccessScopes(scope)
	}
	return out, nil
}

func toPerson(p store.Person) apigen.Person {
	out := apigen.Person{
		Email: p.Email, Name: p.Name,
		Roles: toRoles(p.Roles), Teams: orEmpty(p.Teams),
		DirectoryRoles: toRoles(p.DirectoryRoles), DirectoryTeams: orEmpty(p.DirectoryTeams),
		AddedBy: p.AddedBy, AddedOn: p.AddedOn,
	}
	if !p.LastSignedIn.IsZero() {
		t := p.LastSignedIn
		out.LastSignedIn = &t
	}
	return out
}

func toRoles(roles []string) []apigen.Role {
	out := []apigen.Role{}
	for _, r := range auth.Roles {
		if slices.Contains(roles, r) {
			out = append(out, apigen.Role(r))
		}
	}
	return out
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func problems(message string) apigen.ProblemList {
	return apigen.ProblemList{Problems: []apigen.Problem{{Message: message}}}
}

// writeError answers an error a handler returned rather than turned into
// a response. A refusal from deep inside a write (the engine asks the
// authorizer again with the change, docs/adr/0011) is 403 in the
// contract's problem shape, wherever it surfaced; anything else is 500.
func writeError(w http.ResponseWriter, _ *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, auth.ErrForbidden), errors.Is(err, engine.ErrReadOnly):
		status = http.StatusForbidden
	case errors.Is(err, engine.ErrSelf):
		status = http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"problems":[{"path":"","message":"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(err.Error()) + `"}]}`))
}
