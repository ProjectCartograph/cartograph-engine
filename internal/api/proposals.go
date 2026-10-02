package api

import (
	"context"
	"encoding/json"
	"errors"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/render"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// ListProposals lists what agents proposed (docs/adr/0016): the caller's
// open ones by default, or every proposal on one manifest.
func (s *Server) ListProposals(ctx context.Context, req apigen.ListProposalsRequestObject) (apigen.ListProposalsResponseObject, error) {
	f := store.ProposalFilter{Status: store.ProposalOpen}
	if req.Params.Kind != nil && req.Params.Id != nil {
		f.Kind, f.ManifestID = *req.Params.Kind, *req.Params.Id
	}
	if req.Params.Status != nil {
		f.Status = string(*req.Params.Status)
		if f.Status == "all" {
			f.Status = ""
		}
	}
	list, err := s.Engine.Proposals(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make(apigen.ListProposals200JSONResponse, len(list))
	for i, p := range list {
		out[i] = s.toProposal(p)
	}
	return out, nil
}

// AcceptProposal makes the record a proposal proposed, as the caller.
func (s *Server) AcceptProposal(ctx context.Context, req apigen.AcceptProposalRequestObject) (apigen.AcceptProposalResponseObject, error) {
	actor, reason, err := s.decision(ctx, req.Body)
	if err != nil {
		return nil, err
	}
	p, err := s.Engine.AcceptProposal(ctx, req.Proposal, actor, reason, func(ctx context.Context, id, _ string) (int, error) {
		return s.handoff(ctx, id, actor)
	})
	var invalid *engine.ValidationError
	switch {
	case err == nil:
		return apigen.AcceptProposal200JSONResponse(s.toProposal(p)), nil
	case errors.As(err, &invalid):
		return apigen.AcceptProposal422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(invalid.Problems))}, nil
	case errors.Is(err, store.ErrProposalDecided), errors.Is(err, engine.ErrProposalStale), errors.Is(err, engine.ErrConflict):
		return apigen.AcceptProposal409JSONResponse(problems(err.Error())), nil
	case errors.Is(err, engine.ErrNotFound):
		return apigen.AcceptProposal404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	return nil, err
}

// DeclineProposal declines a proposal, as the caller.
func (s *Server) DeclineProposal(ctx context.Context, req apigen.DeclineProposalRequestObject) (apigen.DeclineProposalResponseObject, error) {
	actor, reason, err := s.decision(ctx, req.Body)
	if err != nil {
		return nil, err
	}
	p, err := s.Engine.DeclineProposal(ctx, req.Proposal, actor, reason)
	switch {
	case err == nil:
		return apigen.DeclineProposal200JSONResponse(s.toProposal(p)), nil
	case errors.Is(err, store.ErrProposalDecided):
		return apigen.DeclineProposal409JSONResponse(problems(err.Error())), nil
	case errors.Is(err, engine.ErrNotFound):
		return apigen.DeclineProposal404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	return nil, err
}

func (s *Server) decision(ctx context.Context, body *apigen.ProposalDecision) (actor, reason string, err error) {
	settings, err := s.Engine.GetSettings(ctx)
	if err != nil {
		return "", "", err
	}
	if body != nil && body.Reason != nil {
		reason = *body.Reason
	}
	return auth.PrincipalFrom(ctx).Actor(settings.Operator), reason, nil
}

// handoff renders, prints and hands off a project, as a state change to
// handed off does, and returns the version handed off.
func (s *Server) handoff(ctx context.Context, id, actor string) (int, error) {
	n, err := s.Engine.HandoffGate(ctx, id)
	if err != nil {
		return 0, err
	}
	htmlBytes, jsonBytes, err := render.Charter(ctx, s.Engine, id, n)
	if err != nil {
		return 0, err
	}
	pdfBytes, _ := s.Printer.Print(ctx, htmlBytes)
	if _, err := s.Engine.Handoff(ctx, id, actor, engine.HandoffRequest{HtmlBytes: htmlBytes, JsonBytes: jsonBytes, PdfBytes: pdfBytes}); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Server) toProposal(p store.Proposal) apigen.Proposal {
	out := apigen.Proposal{Id: p.ID, Kind: p.Kind, ManifestId: p.ManifestID, Op: apigen.ProposalOp(p.Op), Reason: p.Reason,
		Agent: p.Agent, For: p.For, Base: p.Base, At: p.At, Status: apigen.ProposalStatus(p.Status)}
	if p.Text != nil {
		if doc, err := s.Engine.Codec().Decode(p.Text); err == nil {
			out.Manifest = &doc
		}
	}
	if p.Series != "" {
		series := p.Series
		out.Series = &series
	}
	if p.Item != nil {
		var item map[string]any
		if json.Unmarshal(p.Item, &item) == nil {
			out.Item = &item
		}
	}
	if p.State != "" {
		state := p.State
		out.State = &state
	}
	if p.Status != store.ProposalOpen {
		by, at := p.DecidedBy, p.DecidedAt
		out.DecidedBy, out.DecidedAt = &by, &at
	}
	if p.Version > 0 {
		v := p.Version
		out.Version = &v
	}
	return out
}
