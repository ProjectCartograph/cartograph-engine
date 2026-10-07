package api

import (
	"context"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
)

// GetLinkCandidates is what a link may join from one record, read as the
// change set named would leave the workspace.
func (s *Server) GetLinkCandidates(ctx context.Context, req apigen.GetLinkCandidatesRequestObject) (apigen.GetLinkCandidatesResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	problem := ""
	if req.Params.Problem != nil {
		problem = *req.Params.Problem
	}
	cands, err := s.Engine.LinkCandidates(ctx, string(req.Link), req.Params.From, problem)
	if err != nil {
		return nil, err
	}
	out := make(apigen.GetLinkCandidates200JSONResponse, len(cands))
	for i, c := range cands {
		out[i] = apigen.LinkCandidate{Kind: c.Kind, Id: c.ID, Name: c.Name, Allowed: c.Allowed}
		if c.Linked {
			linked := true
			out[i].Linked = &linked
		}
		if c.Reason != "" {
			reason := c.Reason
			out[i].Reason = &reason
		}
	}
	return out, nil
}
