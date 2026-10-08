package api

import (
	"context"
	"errors"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// GetProjectDMAIC says whether a project can be taken through DMAIC
// (TAXONOMY.md D58).
func (s *Server) GetProjectDMAIC(ctx context.Context, req apigen.GetProjectDMAICRequestObject) (apigen.GetProjectDMAICResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	d, err := s.Engine.DMAICOf(ctx, req.Id)
	if errors.Is(err, engine.ErrNotFound) {
		return apigen.GetProjectDMAIC404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	var out apigen.DMAIC
	if err := convertJSON(d, &out); err != nil {
		return nil, err
	}
	return apigen.GetProjectDMAIC200JSONResponse(out), nil
}

// GetKPIControlChart reads a KPI's readings as a control chart.
func (s *Server) GetKPIControlChart(ctx context.Context, req apigen.GetKPIControlChartRequestObject) (apigen.GetKPIControlChartResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	cc, err := s.Engine.ControlChartOf(ctx, req.Id)
	if errors.Is(err, engine.ErrNotFound) {
		return apigen.GetKPIControlChart404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	var out apigen.ControlChart
	if err := convertJSON(cc, &out); err != nil {
		return nil, err
	}
	return apigen.GetKPIControlChart200JSONResponse(out), nil
}
