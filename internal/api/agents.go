package api

import (
	"context"
	"errors"
	"time"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// AgentTokens is the port POST /agents mints through: the authorization
// server that issues agents' tokens, which grants the signed-in person's
// agent and returns a token that carries the grant (docs/adr/0016).
type AgentTokens interface {
	Mint(ctx context.Context, label string, ttl time.Duration) (string, store.AgentGrant, error)
}

// ListAgentGrants lists the agents people let act for them.
func (s *Server) ListAgentGrants(ctx context.Context, req apigen.ListAgentGrantsRequestObject) (apigen.ListAgentGrantsResponseObject, error) {
	person := ""
	if req.Params.Person != nil {
		person = *req.Params.Person
	}
	list, err := s.Engine.AgentGrants(ctx, person)
	if err != nil {
		return nil, err
	}
	out := make(apigen.ListAgentGrants200JSONResponse, len(list))
	for i, g := range list {
		out[i] = toAgentGrant(g)
	}
	return out, nil
}

// CreateAgentToken lets an agent act for the caller by a token they paste
// into its client.
func (s *Server) CreateAgentToken(ctx context.Context, req apigen.CreateAgentTokenRequestObject) (apigen.CreateAgentTokenResponseObject, error) {
	if s.AgentTokens == nil {
		return apigen.CreateAgentToken404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(problems("agents sign in with this deployment's own authorization server, not a token from Cartograph"))}, nil
	}
	days := 0
	if req.Body.Days != nil {
		days = *req.Body.Days
	}
	token, g, err := s.AgentTokens.Mint(ctx, req.Body.Label, time.Duration(days)*24*time.Hour)
	var invalid *engine.ValidationError
	switch {
	case errors.As(err, &invalid):
		return apigen.CreateAgentToken422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(invalid.Problems))}, nil
	case err != nil:
		return nil, err
	}
	return apigen.CreateAgentToken201JSONResponse{Grant: toAgentGrant(g), Token: token}, nil
}

// RevokeAgentGrant disconnects an agent at once.
func (s *Server) RevokeAgentGrant(ctx context.Context, req apigen.RevokeAgentGrantRequestObject) (apigen.RevokeAgentGrantResponseObject, error) {
	err := s.Engine.RevokeAgentGrant(ctx, req.Grant)
	if errors.Is(err, engine.ErrNotFound) {
		return apigen.RevokeAgentGrant404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.RevokeAgentGrant204Response{}, nil
}

func toAgentGrant(g store.AgentGrant) apigen.AgentGrant {
	out := apigen.AgentGrant{Id: g.ID, Person: g.Email, Label: g.Label, CreatedAt: g.CreatedAt, ExpiresAt: g.ExpiresAt}
	if !g.LastUsed.IsZero() {
		out.LastUsed = &g.LastUsed
	}
	if !g.RevokedAt.IsZero() {
		out.RevokedAt = &g.RevokedAt
	}
	return out
}

// GetAgentFeed returns the document to follow a person's agents on.
func (s *Server) GetAgentFeed(ctx context.Context, req apigen.GetAgentFeedRequestObject) (apigen.GetAgentFeedResponseObject, error) {
	person := ""
	if req.Params.Person != nil {
		person = *req.Params.Person
	}
	docID, err := s.Engine.AgentFeed(ctx, person)
	if errors.Is(err, engine.ErrNotFound) {
		return apigen.GetAgentFeed404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.GetAgentFeed200JSONResponse(sharedDocument(docID)), nil
}
