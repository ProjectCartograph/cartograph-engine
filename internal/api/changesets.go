package api

import (
	"context"
	"errors"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Change sets (docs/adr/0022): a piece of work's own drafts, reviewed and
// accepted whole. The handlers say only how each outcome is answered; what
// may happen to a change set is the engine's.

func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toChangeSet(cs store.ChangeSet) apigen.ChangeSet {
	out := apigen.ChangeSet{Id: cs.ID, Title: cs.Title, Status: apigen.ChangeSetStatus(cs.Status), At: cs.At, Updated: cs.Updated,
		Description: opt(cs.Description), Agent: opt(cs.Agent), For: opt(cs.For), Reason: opt(cs.Reason),
		DecidedBy: opt(cs.DecidedBy), DecisionReason: opt(cs.DecisionReason)}
	if !cs.DecidedAt.IsZero() {
		at := cs.DecidedAt
		out.DecidedAt = &at
	}
	if len(cs.Waivers) > 0 {
		ws := make([]apigen.Waiver, len(cs.Waivers))
		for i, w := range cs.Waivers {
			ws[i] = apigen.Waiver{On: opt(w.On), Check: w.Check, Message: w.Message, Reason: w.Reason}
		}
		out.Waivers = &ws
	}
	return out
}

// changeSetError answers what the engine refused, or nil to answer 500.
func changeSetError(err error) (status int, body apigen.ProblemList, handled bool) {
	var invalid *engine.ValidationError
	var open *engine.OpenChecksError
	switch {
	case errors.As(err, &invalid):
		return 422, toProblemList(invalid.Problems), true
	case errors.As(err, &open):
		var ps []engine.Problem
		for _, c := range open.Open {
			ps = append(ps, engine.Problem{Path: c.Kind + "/" + c.ManifestID, Message: c.ID + ": " + c.Message})
		}
		return 422, toProblemList(ps), true
	case errors.Is(err, engine.ErrNotFound), errors.Is(err, store.ErrNoChangeSet):
		return 404, problems(err.Error()), true
	case errors.Is(err, identity.ErrForbidden):
		return 403, problems(err.Error()), true
	case errors.Is(err, engine.ErrConflict), errors.Is(err, engine.ErrProposalStale), errors.Is(err, store.ErrChangeSetMoved):
		return 409, problems(err.Error()), true
	}
	return 0, apigen.ProblemList{}, false
}

func (s *Server) ListChangeSets(ctx context.Context, req apigen.ListChangeSetsRequestObject) (apigen.ListChangeSetsResponseObject, error) {
	status := ""
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	list, err := s.Engine.ChangeSets(ctx, status, req.Params.Everyone != nil && *req.Params.Everyone)
	if err != nil {
		return nil, err
	}
	out := make(apigen.ListChangeSets200JSONResponse, len(list))
	for i, cs := range list {
		out[i] = toChangeSet(cs)
	}
	return out, nil
}

func (s *Server) StartChangeSet(ctx context.Context, req apigen.StartChangeSetRequestObject) (apigen.StartChangeSetResponseObject, error) {
	title, description := "", ""
	if req.Body != nil {
		title, description = deref(req.Body.Title), deref(req.Body.Description)
	}
	cs, err := s.Engine.StartChangeSet(ctx, title, description)
	if err != nil {
		return nil, err
	}
	return apigen.StartChangeSet200JSONResponse(toChangeSet(cs)), nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Server) GetChangeSet(ctx context.Context, req apigen.GetChangeSetRequestObject) (apigen.GetChangeSetResponseObject, error) {
	view, err := s.Engine.ViewChangeSet(ctx, req.Set)
	if errors.Is(err, engine.ErrNotFound) {
		return apigen.GetChangeSet404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	out := apigen.ChangeSetReview{ChangeSet: toChangeSet(view.ChangeSet), Items: make([]apigen.ChangeSetItem, len(view.Items))}
	for i, it := range view.Items {
		item := apigen.ChangeSetItem{Kind: it.Item.Kind, Id: it.Item.ID, Name: opt(it.Name), Base: it.Item.Base, Included: it.Item.Included,
			By: opt(it.Item.By), Changes: toChanges(it.Changes), Checks: toChecks(it.Checks)}
		if !it.Item.At.IsZero() {
			at := it.Item.At
			item.At = &at
		}
		if it.Stale > 0 {
			stale := it.Stale
			item.Stale = &stale
		}
		out.Items[i] = item
	}
	return apigen.GetChangeSet200JSONResponse(out), nil
}

func (s *Server) RetitleChangeSet(ctx context.Context, req apigen.RetitleChangeSetRequestObject) (apigen.RetitleChangeSetResponseObject, error) {
	cs, err := s.Engine.RetitleChangeSet(ctx, req.Set, deref(req.Body.Title), deref(req.Body.Description))
	if code, body, ok := changeSetError(err); ok {
		switch code {
		case 404:
			return apigen.RetitleChangeSet404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(body)}, nil
		case 403:
			return nil, err
		}
		return apigen.RetitleChangeSet409JSONResponse(body), nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.RetitleChangeSet200JSONResponse(toChangeSet(cs)), nil
}

func (s *Server) GetChangeSetItem(ctx context.Context, req apigen.GetChangeSetItemRequestObject) (apigen.GetChangeSetItemResponseObject, error) {
	text, in, err := s.Engine.ChangeSetText(ctx, req.Set, req.Kind, req.Id)
	if errors.Is(err, engine.ErrNotFound) {
		return apigen.GetChangeSetItem404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.GetChangeSetItem200JSONResponse{Yaml: string(text), InChangeSet: in}, nil
}

func (s *Server) PutChangeSetItem(ctx context.Context, req apigen.PutChangeSetItemRequestObject) (apigen.PutChangeSetItemResponseObject, error) {
	var previous []byte
	if req.Body.Previous != nil {
		previous = []byte(*req.Body.Previous)
	}
	text, err := s.Engine.MergeInChangeSet(ctx, req.Set, req.Kind, req.Id, previous, []byte(req.Body.Yaml))
	if code, body, ok := changeSetError(err); ok {
		switch code {
		case 404:
			return apigen.PutChangeSetItem404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(body)}, nil
		case 422:
			return apigen.PutChangeSetItem422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(body)}, nil
		case 403:
			return nil, err
		}
		return apigen.PutChangeSetItem409JSONResponse(body), nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.PutChangeSetItem200JSONResponse{Yaml: string(text), InChangeSet: true}, nil
}

func (s *Server) IncludeChangeSetItem(ctx context.Context, req apigen.IncludeChangeSetItemRequestObject) (apigen.IncludeChangeSetItemResponseObject, error) {
	err := s.Engine.IncludeChangeItem(ctx, req.Set, req.Kind, req.Id, req.Body.Included)
	if code, body, ok := changeSetError(err); ok {
		switch code {
		case 404:
			return apigen.IncludeChangeSetItem404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(body)}, nil
		case 403:
			return nil, err
		}
		return apigen.IncludeChangeSetItem409JSONResponse(body), nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.IncludeChangeSetItem204Response{}, nil
}

func (s *Server) DropChangeSetItem(ctx context.Context, req apigen.DropChangeSetItemRequestObject) (apigen.DropChangeSetItemResponseObject, error) {
	err := s.Engine.DropChangeItem(ctx, req.Set, req.Kind, req.Id)
	if code, body, ok := changeSetError(err); ok {
		switch code {
		case 404:
			return apigen.DropChangeSetItem404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(body)}, nil
		case 403:
			return nil, err
		}
		return apigen.DropChangeSetItem409JSONResponse(body), nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.DropChangeSetItem204Response{}, nil
}

func (s *Server) ProposeChangeSet(ctx context.Context, req apigen.ProposeChangeSetRequestObject) (apigen.ProposeChangeSetResponseObject, error) {
	reason := ""
	var waive map[string]map[string]string
	if req.Body != nil {
		reason = deref(req.Body.Reason)
		if req.Body.OpenChecks != nil {
			waive = *req.Body.OpenChecks
		}
	}
	cs, err := s.Engine.ProposeChangeSet(ctx, req.Set, reason, waive)
	if code, body, ok := changeSetError(err); ok {
		switch code {
		case 404:
			return apigen.ProposeChangeSet404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(body)}, nil
		case 422:
			return apigen.ProposeChangeSet422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(body)}, nil
		case 403:
			return nil, err
		}
		return apigen.ProposeChangeSet409JSONResponse(body), nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.ProposeChangeSet200JSONResponse(toChangeSet(cs)), nil
}

func (s *Server) AcceptChangeSet(ctx context.Context, req apigen.AcceptChangeSetRequestObject) (apigen.AcceptChangeSetResponseObject, error) {
	_, reason, err := s.decision(ctx, req.Body)
	if err != nil {
		return nil, err
	}
	_, err = s.Engine.AcceptChangeSet(ctx, req.Set, reason)
	if code, body, ok := changeSetError(err); ok {
		switch code {
		case 404:
			return apigen.AcceptChangeSet404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(body)}, nil
		case 422:
			return apigen.AcceptChangeSet422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(body)}, nil
		case 403:
			return nil, err
		}
		return apigen.AcceptChangeSet409JSONResponse(body), nil
	}
	if err != nil {
		return nil, err
	}
	view, err := s.Engine.ViewChangeSet(ctx, req.Set)
	if err != nil {
		return nil, err
	}
	return apigen.AcceptChangeSet200JSONResponse(toChangeSet(view.ChangeSet)), nil
}

func (s *Server) CloseChangeSet(ctx context.Context, req apigen.CloseChangeSetRequestObject) (apigen.CloseChangeSetResponseObject, error) {
	_, reason, err := s.decision(ctx, req.Body)
	if err != nil {
		return nil, err
	}
	cs, err := s.Engine.CloseChangeSet(ctx, req.Set, reason)
	if code, body, ok := changeSetError(err); ok {
		switch code {
		case 404:
			return apigen.CloseChangeSet404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(body)}, nil
		case 403:
			return nil, err
		}
		return apigen.CloseChangeSet409JSONResponse(body), nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.CloseChangeSet200JSONResponse(toChangeSet(cs)), nil
}

func (s *Server) ReopenChangeSet(ctx context.Context, req apigen.ReopenChangeSetRequestObject) (apigen.ReopenChangeSetResponseObject, error) {
	_, reason, err := s.decision(ctx, req.Body)
	if err != nil {
		return nil, err
	}
	cs, err := s.Engine.ReopenChangeSet(ctx, req.Set, reason)
	if code, body, ok := changeSetError(err); ok {
		switch code {
		case 404:
			return apigen.ReopenChangeSet404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse(body)}, nil
		case 403:
			return nil, err
		}
		return apigen.ReopenChangeSet409JSONResponse(body), nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.ReopenChangeSet200JSONResponse(toChangeSet(cs)), nil
}
