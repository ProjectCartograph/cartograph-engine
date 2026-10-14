package api

import (
	"context"
	"errors"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// GetProjectRound serves the decisions a project's walk can settle now
// (docs/adr/0032): the tree the MCP server hands an agent, for a person.
func (s *Server) GetProjectRound(ctx context.Context, req apigen.GetProjectRoundRequestObject) (apigen.GetProjectRoundResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	set := ""
	if req.Params.ChangeSet != nil {
		set = string(*req.Params.ChangeSet)
	}
	r, err := s.Engine.Round(ctx, set, []engine.Ref{{Kind: "Project", ID: req.Id}}, "")
	if errors.Is(err, engine.ErrNotFound) {
		return apigen.GetProjectRound404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	var out apigen.ProjectRound
	if err := convertJSON(r, &out); err != nil {
		return nil, err
	}
	return apigen.GetProjectRound200JSONResponse(out), nil
}
