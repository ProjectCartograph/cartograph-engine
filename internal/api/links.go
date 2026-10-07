package api

import (
	"context"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
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

// GetComponents is the graph of components across the workspace, read as
// the change set named would leave it.
func (s *Server) GetComponents(ctx context.Context, req apigen.GetComponentsRequestObject) (apigen.GetComponentsResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	g, err := s.Engine.Components(ctx)
	if err != nil {
		return nil, err
	}
	ref := func(r engine.Ref) apigen.WorkRef { return apigen.WorkRef{Kind: apigen.WorkRefKind(r.Kind), Id: r.ID} }
	refs := func(rs []engine.Ref) []apigen.WorkRef {
		out := make([]apigen.WorkRef, len(rs))
		for i, r := range rs {
			out[i] = ref(r)
		}
		return out
	}
	out := apigen.GetComponents200JSONResponse{
		Nodes:          make([]apigen.ComponentNode, len(g.Nodes)),
		Edges:          make([]apigen.ComponentEdge, len(g.Edges)),
		Loops:          make([][]apigen.WorkRef, len(g.Loops)),
		CriticalPath:   refs(g.CriticalPath),
		CriticalMonths: g.CriticalMonths,
	}
	for i, n := range g.Nodes {
		out.Nodes[i] = apigen.ComponentNode{
			Kind: apigen.ComponentNodeKind(n.Kind), Id: n.ID, Name: n.Name, Months: n.Months,
			Dependents: n.Dependents, Critical: n.Critical, InLoop: n.InLoop, MostDependedOn: n.MostDependedOn,
		}
	}
	for i, ed := range g.Edges {
		out.Edges[i] = apigen.ComponentEdge{From: ref(ed.From), To: ref(ed.To)}
		if ed.Why != "" {
			why := ed.Why
			out.Edges[i].Why = &why
		}
		if ed.Legacy {
			legacy := true
			out.Edges[i].Legacy = &legacy
		}
	}
	for i, l := range g.Loops {
		out.Loops[i] = refs(l)
	}
	return out, nil
}
