package api

import (
	"context"
	"errors"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

func sharedDocument(docID string) apigen.SharedDocument {
	return apigen.SharedDocument{DocumentId: docID, Url: "automerge:" + docID}
}

// GetSharedDocument names a manifest's shared draft, creating it on first
// use (docs/adr/0007).
func (s *Server) GetSharedDocument(ctx context.Context, req apigen.GetSharedDocumentRequestObject) (apigen.GetSharedDocumentResponseObject, error) {
	shared := s.Engine.Shared()
	if shared == nil {
		return nil, engine.ErrNoShared
	}
	docID, err := shared.DocumentFor(ctx, req.Kind, req.Id)
	if errors.Is(err, engine.ErrUnknownKind) || errors.Is(err, engine.ErrNotFound) {
		return apigen.GetSharedDocument404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.GetSharedDocument200JSONResponse(sharedDocument(docID)), nil
}

// GetPresenceDocument names the document that routes presence on screens
// not about one manifest.
func (s *Server) GetPresenceDocument(ctx context.Context, _ apigen.GetPresenceDocumentRequestObject) (apigen.GetPresenceDocumentResponseObject, error) {
	shared := s.Engine.Shared()
	if shared == nil {
		return nil, engine.ErrNoShared
	}
	docID, err := shared.PresenceDocument(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.GetPresenceDocument200JSONResponse(sharedDocument(docID)), nil
}

// GetSession says who this request acts as. canWrite is the authorizer's
// answer for a write with no particular manifest; the engine still decides
// every write on its own.
func (s *Server) GetSession(ctx context.Context, _ apigen.GetSessionRequestObject) (apigen.GetSessionResponseObject, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	p := auth.PrincipalFrom(ctx)
	out := apigen.Session{Actor: actor, CanWrite: s.Authz.Authorize(ctx, p, auth.Action{Verb: auth.VerbWrite}) == nil}
	if p.Name != "" {
		name := p.Name
		out.Name = &name
	}
	if p.Email != "" {
		email := p.Email
		out.Email = &email
	}
	if out.Access, err = s.sessionAccess(ctx, p); err != nil {
		return nil, err
	}
	if out.Access != nil {
		// canWrite is whether they may write anything at all.
		out.CanWrite = false
		for _, sc := range out.Access.Scopes {
			if sc != apigen.SessionAccessScopesNone {
				out.CanWrite = true
			}
		}
	}
	return apigen.GetSession200JSONResponse(out), nil
}

// Sync answers a request to the sync path that did not upgrade: the
// socket itself is served by the sync server, mounted ahead of this
// handler (cmd/cartograph).
func (s *Server) Sync(_ context.Context, _ apigen.SyncRequestObject) (apigen.SyncResponseObject, error) {
	return apigen.Sync426Response{}, nil
}
