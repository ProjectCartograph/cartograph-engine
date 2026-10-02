package api

import (
	"context"
	"encoding/json"
	"errors"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// GetGuide returns how to define a kind well, as agents read it over MCP.
// The engine's guide already has the contract's shape, field for field.
func (s *Server) GetGuide(ctx context.Context, req apigen.GetGuideRequestObject) (apigen.GetGuideResponseObject, error) {
	level, locale := "", ""
	if req.Params.Level != nil {
		level = *req.Params.Level
	}
	if req.Params.Locale != nil {
		locale = *req.Params.Locale
	}
	g, err := s.Engine.Guide(ctx, req.Kind, level, locale)
	if errors.Is(err, engine.ErrNotFound) {
		return apigen.GetGuide404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}
	var out apigen.Guide
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return apigen.GetGuide200JSONResponse(out), nil
}
