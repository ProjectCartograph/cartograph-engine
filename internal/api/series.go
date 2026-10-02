package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting"
)

// AppendSeriesItem records one item of a series, such as a reading, and
// commits the manifest with it.
func (s *Server) AppendSeriesItem(ctx context.Context, req apigen.AppendSeriesItemRequestObject) (apigen.AppendSeriesItemResponseObject, error) {
	settings, err := s.Engine.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.Engine.AppendSeriesItem(ctx, req.Kind, req.Id, req.Series, req.Body.Item,
		auth.PrincipalFrom(ctx).Actor(settings.Operator), req.Body.Reason)
	if err == nil {
		return apigen.AppendSeriesItem200JSONResponse(toVersion(v)), nil
	}
	var ve *engine.ValidationError
	switch {
	case errors.As(err, &ve):
		return apigen.AppendSeriesItem422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(ve.Problems))}, nil
	case errors.Is(err, engine.ErrNoSeries), errors.Is(err, engine.ErrNotFound), errors.Is(err, engine.ErrUnknownKind):
		return apigen.AppendSeriesItem404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	case errors.Is(err, engine.ErrConflict):
		return apigen.AppendSeriesItem409JSONResponse{ConflictJSONResponse: apigen.ConflictJSONResponse(apigen.ConflictResponse{})}, nil
	}
	return nil, err
}

// GetReport answers a report from the deployment's reporter, as JSON or
// as CSV; 404 when reporting is off.
func (s *Server) GetReport(ctx context.Context, req apigen.GetReportRequestObject) (apigen.GetReportResponseObject, error) {
	if s.Reports == nil {
		return apigen.GetReport404JSONResponse{NotFoundJSONResponse: notFound(errReportsOff)}, nil
	}
	t, err := s.Reports.Run(ctx, string(req.Name))
	if errors.Is(err, reporting.ErrNoReport) {
		return apigen.GetReport404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	if req.Params.Format != nil && *req.Params.Format == apigen.Csv {
		b, err := ReportCSV(t)
		if err != nil {
			return nil, err
		}
		return apigen.GetReport200TextcsvResponse{Body: bytes.NewReader(b), ContentLength: int64(len(b))}, nil
	}
	return apigen.GetReport200JSONResponse(apigen.Report{Name: t.Name, Columns: t.Columns, Rows: t.Rows}), nil
}

var errReportsOff = errors.New("reporting is off in this deployment")

// ReportCSV writes a report as CSV, a header row first.
func ReportCSV(t reporting.Table) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(t.Columns); err != nil {
		return nil, err
	}
	for _, r := range t.Rows {
		rec := make([]string, len(r))
		for i, v := range r {
			if v != nil {
				rec[i] = fmt.Sprint(v)
			}
		}
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}
