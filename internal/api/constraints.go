package api

import (
	"context"
	"errors"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// GetProjectConstraints serves a project's triple constraint (TAXONOMY.md
// D60): each side's stance, its risks and its exposure.
func (s *Server) GetProjectConstraints(ctx context.Context, req apigen.GetProjectConstraintsRequestObject) (apigen.GetProjectConstraintsResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	c, err := s.Engine.Constraints(ctx, req.Id)
	if errors.Is(err, engine.ErrNotFound) {
		return apigen.GetProjectConstraints404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	var out apigen.TripleConstraint
	if err := convertJSON(c, &out); err != nil {
		return nil, err
	}
	return apigen.GetProjectConstraints200JSONResponse(out), nil
}
