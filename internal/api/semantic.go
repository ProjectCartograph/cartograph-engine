package api

import (
	"context"
	"errors"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/semantic"
)

// ExportSemanticLayer writes the KPIs as a semantic layer in a syntax the
// deployment has an exporter for (TAXONOMY.md D57).
func (s *Server) ExportSemanticLayer(ctx context.Context, req apigen.ExportSemanticLayerRequestObject) (apigen.ExportSemanticLayerResponseObject, error) {
	formats := s.Engine.SemanticFormats()
	format := ""
	if req.Params.Format != nil {
		format = *req.Params.Format
	} else if len(formats) > 0 {
		format = formats[0]
	}
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	files, notes, err := s.Engine.ExportSemantic(ctx, format)
	if errors.Is(err, semantic.ErrNoFormat) {
		return apigen.ExportSemanticLayer404JSONResponse{Message: err.Error()}, nil
	}
	if err != nil {
		return nil, err
	}
	out := apigen.SemanticExport{Format: format, Formats: formats, Notes: notes}
	if out.Notes == nil {
		out.Notes = []string{}
	}
	for _, f := range files {
		out.Files = append(out.Files, struct {
			Content string `json:"content"`
			Path    string `json:"path"`
		}{Content: string(f.Content), Path: f.Path})
	}
	return apigen.ExportSemanticLayer200JSONResponse(out), nil
}
