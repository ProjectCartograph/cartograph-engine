package api

import (
	"context"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// StructureQuestions lists the questions that decide what each piece of
// work is (TAXONOMY.md D56).
func (s *Server) StructureQuestions(_ context.Context, _ apigen.StructureQuestionsRequestObject) (apigen.StructureQuestionsResponseObject, error) {
	var qs []apigen.StructureQuestion
	if err := convertJSON(engine.StructureQuestions, &qs); err != nil {
		return nil, err
	}
	return apigen.StructureQuestions200JSONResponse{Questions: qs}, nil
}

// ClassifyStructure says what each piece is from its answers.
func (s *Server) ClassifyStructure(_ context.Context, req apigen.ClassifyStructureRequestObject) (apigen.ClassifyStructureResponseObject, error) {
	var pieces []engine.StructurePiece
	if err := convertJSON(req.Body.Pieces, &pieces); err != nil {
		return nil, err
	}
	var out apigen.Structure
	if err := convertJSON(engine.Classify(pieces), &out); err != nil {
		return nil, err
	}
	return apigen.ClassifyStructure200JSONResponse(out), nil
}
