package api

import (
	"context"
	"errors"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Comments are a conversation about a record (TAXONOMY.md D72); the
// handlers carry them to and from the engine and decide nothing.

const mentionsShown = 50

func toComment(c store.Comment) apigen.Comment {
	out := apigen.Comment{
		Id: c.ID, Kind: c.Kind, Manifest: c.Manifest, Anchor: c.Anchor, Body: c.Body,
		Mentions: make([]apigen.Mention, len(c.Mentions)), Author: c.Author, At: c.At, Resolved: c.Resolved,
	}
	for i, m := range c.Mentions {
		out.Mentions[i] = apigen.Mention{Kind: apigen.MentionKind(m.Kind), Id: m.ID}
	}
	if c.Reply != "" {
		out.Reply = &c.Reply
	}
	if c.Resolved {
		out.ResolvedBy = &c.ResolvedBy
		at := c.ResolvedAt
		out.ResolvedAt = &at
	}
	return out
}

func toComments(cs []store.Comment) []apigen.Comment {
	out := make([]apigen.Comment, len(cs))
	for i, c := range cs {
		out[i] = toComment(c)
	}
	return out
}

func (s *Server) ListComments(ctx context.Context, req apigen.ListCommentsRequestObject) (apigen.ListCommentsResponseObject, error) {
	cs, err := s.Engine.Comments(ctx, req.Kind, req.Id)
	var invalid *engine.ValidationError
	switch {
	case errors.Is(err, engine.ErrNotFound), errors.Is(err, engine.ErrUnknownKind), errors.As(err, &invalid):
		return apigen.ListComments404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	case err != nil:
		return nil, err
	}
	return apigen.ListComments200JSONResponse(toComments(cs)), nil
}

func (s *Server) AddComment(ctx context.Context, req apigen.AddCommentRequestObject) (apigen.AddCommentResponseObject, error) {
	d := engine.CommentDraft{Body: req.Body.Body, Anchor: deref(req.Body.Anchor), Reply: deref(req.Body.Reply)}
	if req.Body.Mentions != nil {
		for _, m := range *req.Body.Mentions {
			d.Mentions = append(d.Mentions, store.Mention{Kind: string(m.Kind), ID: m.Id})
		}
	}
	c, err := s.Engine.AddComment(ctx, req.Kind, req.Id, d)
	var invalid *engine.ValidationError
	switch {
	case errors.As(err, &invalid):
		return apigen.AddComment422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(invalid.Problems))}, nil
	case errors.Is(err, engine.ErrNotFound), errors.Is(err, engine.ErrUnknownKind):
		return apigen.AddComment404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	case err != nil:
		return nil, err
	}
	return apigen.AddComment201JSONResponse(toComment(c)), nil
}

func (s *Server) ResolveComment(ctx context.Context, req apigen.ResolveCommentRequestObject) (apigen.ResolveCommentResponseObject, error) {
	c, err := s.Engine.ResolveComment(ctx, req.Comment, req.Body.Resolved)
	var invalid *engine.ValidationError
	switch {
	case errors.As(err, &invalid):
		return apigen.ResolveComment422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(invalid.Problems))}, nil
	case errors.Is(err, engine.ErrNotFound):
		return apigen.ResolveComment404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	case err != nil:
		return nil, err
	}
	return apigen.ResolveComment200JSONResponse(toComment(c)), nil
}

func (s *Server) ListMyMentions(ctx context.Context, _ apigen.ListMyMentionsRequestObject) (apigen.ListMyMentionsResponseObject, error) {
	cs, err := s.Engine.MyMentions(ctx, mentionsShown)
	switch {
	case errors.Is(err, engine.ErrNotFound):
		return apigen.ListMyMentions404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	case err != nil:
		return nil, err
	}
	return apigen.ListMyMentions200JSONResponse(toComments(cs)), nil
}

func (s *Server) ListMentionables(ctx context.Context, req apigen.ListMentionablesRequestObject) (apigen.ListMentionablesResponseObject, error) {
	list, err := s.Engine.Mentionables(ctx, deref(req.Params.Q), 20)
	if err != nil {
		return nil, err
	}
	out := make([]apigen.Mentionable, len(list))
	for i, m := range list {
		out[i] = apigen.Mentionable{Kind: apigen.MentionableKind(m.Kind), Id: m.ID, Name: m.Name}
	}
	return apigen.ListMentionables200JSONResponse(out), nil
}
