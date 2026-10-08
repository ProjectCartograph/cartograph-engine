package api

import (
	"context"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
)

// GetLineage draws a project's data lineage from its data as it is being
// edited and the workspace as the change set reads it.
func (s *Server) GetLineage(ctx context.Context, req apigen.GetLineageRequestObject) (apigen.GetLineageResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	name := ""
	if req.Body.Name != nil {
		name = *req.Body.Name
	}
	l, err := s.Engine.LineageOf(ctx, req.Body.Project, name, req.Body.Uses, req.Body.Produces)
	if err != nil {
		return nil, err
	}
	out := apigen.Lineage{Nodes: make([]apigen.LineageNode, len(l.Nodes)), Edges: make([]apigen.GraphEdge, len(l.Edges))}
	for i, n := range l.Nodes {
		out.Nodes[i] = apigen.LineageNode{Kind: n.Kind, Id: n.ID, Name: n.Name, Role: apigen.LineageNodeRole(n.Role), X: float32(n.X), Y: float32(n.Y)}
	}
	for i, ed := range l.Edges {
		out.Edges[i] = apigen.GraphEdge{From: apigen.Ref{Kind: ed.From.Kind, Id: ed.From.ID}, To: apigen.Ref{Kind: ed.To.Kind, Id: ed.To.ID}}
	}
	return apigen.GetLineage200JSONResponse(out), nil
}
