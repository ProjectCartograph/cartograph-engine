// Package api is the thin driving adapter between the generated strict
// HTTP server (package apigen) and the engine. Every handler follows the
// same shape: convert the request, call exactly one engine method, convert
// the result or classify the error into a Problem-shaped response. No
// business logic lives here.
package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/printer"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/render"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting"
)

// Server implements apigen.StrictServerInterface over an *engine.Engine.
// Printer is the port a PDF goes through; nil means printer.None.
type Server struct {
	Engine  *engine.Engine
	Printer printer.Printer
	Authz   auth.Authorizer
	// Reports answers /reports; nil when the deployment turned
	// reporting off (docs/adr/0014).
	Reports reporting.Reporter
	// AgentTokens mints pasteable agent tokens; nil where another stack
	// authorizes agents.
	AgentTokens AgentTokens
	// AgentsOn is whether agents are served over MCP.
	AgentsOn bool
}

var _ apigen.StrictServerInterface = (*Server)(nil)

// Deps is what a Server needs beyond the engine.
type Deps struct {
	Printer printer.Printer
	// Authorizer answers GET /session's canWrite; the middleware still
	// decides every request. AllowAll when nil.
	Authorizer auth.Authorizer
	// Reports answers /reports. Nil turns reporting off: the reports
	// answer 404.
	Reports reporting.Reporter
	// AgentTokens mints the tokens POST /agents returns: the authorization
	// server's, when Cartograph is one. Nil answers 404.
	AgentTokens AgentTokens
	// AgentsOn says agents are served over MCP, for the session to say.
	AgentsOn bool
}

// Handler returns the complete net/http handler for the API, mounted under
// no particular prefix (the caller mounts it at /api/v1), with no PDF
// printer.
func Handler(e *engine.Engine) http.Handler {
	return New(e, Deps{})
}

// New returns the complete net/http handler for the API over the given
// engine and dependencies.
func New(e *engine.Engine, deps Deps) http.Handler {
	p := deps.Printer
	if p == nil {
		p = printer.None{}
	}
	z := deps.Authorizer
	if z == nil {
		z = auth.AllowAll{}
	}
	strict := apigen.NewStrictHandlerWithOptions(&Server{Engine: e, Printer: p, Authz: z, Reports: deps.Reports, AgentTokens: deps.AgentTokens, AgentsOn: deps.AgentsOn}, nil,
		apigen.StrictHTTPServerOptions{ResponseErrorHandlerFunc: writeError})
	return apigen.Handler(strict)
}

// actor is who a write records: the authenticated principal, or the
// vault's operator when nobody is authenticated.
func (s *Server) actor(ctx context.Context) (string, error) {
	settings, err := s.Engine.GetSettings(ctx)
	if err != nil {
		return "", err
	}
	return auth.PrincipalFrom(ctx).Actor(settings.Operator), nil
}

func (s *Server) GetHealth(_ context.Context, _ apigen.GetHealthRequestObject) (apigen.GetHealthResponseObject, error) {
	return apigen.GetHealth200JSONResponse{Status: "ok"}, nil
}

func (s *Server) GetSettings(ctx context.Context, _ apigen.GetSettingsRequestObject) (apigen.GetSettingsResponseObject, error) {
	settings, err := s.Engine.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.GetSettings200JSONResponse{
		GoalLevels:       settings.GoalLevels,
		ProjectLevelName: settings.ProjectLevelName,
		Operator:         settings.Operator,
		Examples:         examplesOrNil(settings.Examples),
		Purpose:          purposeOf(settings.Purpose),
	}, nil
}

// purposeOf maps the vault's vision and mission, absent when unset.
func purposeOf(p *engine.Purpose) *struct {
	Mission *string `json:"mission,omitempty"`
	Source  *string `json:"source,omitempty"`
	Vision  *string `json:"vision,omitempty"`
} {
	if p == nil {
		return nil
	}
	s := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	return &struct {
		Mission *string `json:"mission,omitempty"`
		Source  *string `json:"source,omitempty"`
		Vision  *string `json:"vision,omitempty"`
	}{Mission: s(p.Mission), Source: s(p.Source), Vision: s(p.Vision)}
}

// examplesOrNil keeps an absent map absent in the response rather than an
// empty object.
func examplesOrNil(m map[string][]string) *map[string][]string {
	if len(m) == 0 {
		return nil
	}
	return &m
}

func (s *Server) GetGoalTree(ctx context.Context, _ apigen.GetGoalTreeRequestObject) (apigen.GetGoalTreeResponseObject, error) {
	tree, err := s.Engine.GoalTree(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.GetGoalTree200JSONResponse(toGoalTree(tree)), nil
}

func (s *Server) GetGoalChecks(ctx context.Context, req apigen.GetGoalChecksRequestObject) (apigen.GetGoalChecksResponseObject, error) {
	checks, err := s.Engine.GoalChecks(ctx, req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetGoalChecks404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	out := make(apigen.GetGoalChecks200JSONResponse, len(checks))
	for i, c := range checks {
		out[i] = toGoalCheck(c)
	}
	return out, nil
}

func (s *Server) DeleteGoal(ctx context.Context, req apigen.DeleteGoalRequestObject) (apigen.DeleteGoalResponseObject, error) {
	reason := ""
	if req.Body != nil {
		reason = req.Body.Reason
	}
	settings, err := s.Engine.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	err = s.Engine.DeleteGoal(ctx, req.Id, auth.PrincipalFrom(ctx).Actor(settings.Operator), reason)
	if err != nil {
		var ve *engine.ValidationError
		switch {
		case errors.As(err, &ve):
			return apigen.DeleteGoal422JSONResponse(toProblemList(ve.Problems)), nil
		case errors.Is(err, engine.ErrNotFound):
			return apigen.DeleteGoal404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		default:
			return nil, err
		}
	}
	return apigen.DeleteGoal204Response{}, nil
}

// DeleteManifest excludes a manifest from the live state (any kind).
func (s *Server) DeleteManifest(ctx context.Context, req apigen.DeleteManifestRequestObject) (apigen.DeleteManifestResponseObject, error) {
	reason := ""
	if req.Body != nil {
		reason = req.Body.Reason
	}
	settings, err := s.Engine.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	err = s.Engine.Delete(ctx, req.Kind, req.Id, auth.PrincipalFrom(ctx).Actor(settings.Operator), reason)
	if err != nil {
		var ve *engine.ValidationError
		switch {
		case errors.As(err, &ve):
			return apigen.DeleteManifest422JSONResponse(toProblemList(ve.Problems)), nil
		case errors.Is(err, engine.ErrNotFound):
			return apigen.DeleteManifest404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		default:
			return nil, err
		}
	}
	return apigen.DeleteManifest204Response{}, nil
}

func toGoalNode(n *engine.GoalNode) apigen.GoalNode {
	out := apigen.GoalNode{
		Id:         n.ID,
		Name:       n.Name,
		Level:      n.Level,
		KeyResults: n.KeyResults,
		Aligned: apigen.GoalAligned{
			Projects:   n.Aligned.Projects,
			Programmes: n.Aligned.Programmes,
			Operations: n.Aligned.Operations,
			Kpis:       n.Aligned.KPIs,
		},
	}
	out.Smart.Specific = n.Smart.Specific
	out.Smart.Measurable = n.Smart.Measurable
	out.Smart.Attainable = n.Smart.Attainable
	if n.Owner != "" {
		owner := n.Owner
		out.Owner = &owner
	}
	if n.Horizon != nil {
		h := struct {
			End       string `json:"end"`
			Inherited *bool  `json:"inherited,omitempty"`
			Start     string `json:"start"`
		}{Start: n.Horizon.Start, End: n.Horizon.End}
		if n.Horizon.Inherited {
			inherited := true
			h.Inherited = &inherited
		}
		out.Horizon = &h
	}
	out.Smart.Relevant = n.Smart.Relevant
	out.Smart.TimeBound = n.Smart.TimeBound
	if n.Parent != "" {
		parent := n.Parent
		out.Parent = &parent
	}
	children := make([]apigen.GoalNode, len(n.Children))
	for i, c := range n.Children {
		children[i] = toGoalNode(c)
	}
	out.Children = children
	if n.Objective != "" {
		objective := n.Objective
		out.Objective = &objective
	}
	if n.Why != "" {
		why := n.Why
		out.Why = &why
	}
	// The generated types are anonymous structs with the same JSON shape,
	// so the engine's values are carried across through JSON.
	if len(n.ContributesTo) > 0 {
		if b, err := json.Marshal(n.ContributesTo); err == nil {
			_ = json.Unmarshal(b, &out.ContributesTo)
		}
	}
	if len(n.Gaps) > 0 {
		if b, err := json.Marshal(n.Gaps); err == nil {
			_ = json.Unmarshal(b, &out.Gaps)
		}
	}
	return out
}

func toGoalTree(t engine.GoalTree) apigen.GoalTree {
	nodes := make([]apigen.GoalNode, len(t.Nodes))
	for i, n := range t.Nodes {
		nodes[i] = toGoalNode(n)
	}
	unplaced := make([]apigen.GoalNode, len(t.Unplaced))
	for i, n := range t.Unplaced {
		unplaced[i] = toGoalNode(n)
	}
	return apigen.GoalTree{Levels: t.Levels, Nodes: nodes, Unplaced: &unplaced}
}

func toGoalCheck(c engine.GoalCheck) apigen.GoalCheck {
	out := apigen.GoalCheck{Id: c.ID, State: apigen.GoalCheckState(c.State), Message: c.Message}
	if c.Fix != nil {
		out.Fix = &apigen.GoalCheckFix{Section: c.Fix.Section}
	}
	return out
}

// GetProgrammeChecks mirrors the goal's shape rather than the project's:
// a flat list, no blocking count, because none of a programme's checks
// blocks anything.
func (s *Server) GetGapChecks(ctx context.Context, req apigen.GetGapChecksRequestObject) (apigen.GetGapChecksResponseObject, error) {
	checks, err := s.Engine.GapChecks(ctx, req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetGapChecks404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	out := make(apigen.GetGapChecks200JSONResponse, len(checks))
	for i, c := range checks {
		out[i] = apigen.ProgrammeCheck{
			Id: c.ID, Section: c.Section,
			State: apigen.ProgrammeCheckState(c.State), Message: c.Message,
		}
	}
	return out, nil
}

func (s *Server) GetGapCoverage(ctx context.Context, req apigen.GetGapCoverageRequestObject) (apigen.GetGapCoverageResponseObject, error) {
	coverage, err := s.Engine.GapCoverageFor(ctx, req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetGapCoverage404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	claims := func(in []engine.GapClaim) *[]apigen.GapClaim {
		if len(in) == 0 {
			return nil
		}
		out := make([]apigen.GapClaim, len(in))
		for i, c := range in {
			out[i] = apigen.GapClaim{Kind: c.Kind, Id: c.ID, Name: c.Name, Whole: c.Whole}
			if len(c.Segments) > 0 {
				segs := append([]string(nil), c.Segments...)
				out[i].Segments = &segs
			}
		}
		return &out
	}
	body := apigen.GapCoverage{Gap: coverage.Gap, Segments: make([]apigen.GapSegmentCoverage, len(coverage.Segments))}
	for i, s := range coverage.Segments {
		body.Segments[i] = apigen.GapSegmentCoverage{Segment: s.Segment, Name: s.Name, AddressedBy: claims(s.Addressed)}
	}
	body.Whole = claims(coverage.Whole)
	return apigen.GetGapCoverage200JSONResponse(body), nil
}

// GetOperationChecks: the same flat, advisory shape as a programme's.
func (s *Server) GetOperationChecks(ctx context.Context, req apigen.GetOperationChecksRequestObject) (apigen.GetOperationChecksResponseObject, error) {
	checks, err := s.Engine.OperationChecks(ctx, req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetOperationChecks404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	out := make(apigen.GetOperationChecks200JSONResponse, len(checks))
	for i, c := range checks {
		out[i] = apigen.ProgrammeCheck{
			Id: c.ID, Section: c.Section,
			State: apigen.ProgrammeCheckState(c.State), Message: c.Message,
		}
	}
	return out, nil
}

func (s *Server) GetProgrammeChecks(ctx context.Context, req apigen.GetProgrammeChecksRequestObject) (apigen.GetProgrammeChecksResponseObject, error) {
	checks, err := s.Engine.ProgrammeChecks(ctx, req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetProgrammeChecks404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	out := make(apigen.GetProgrammeChecks200JSONResponse, len(checks))
	for i, c := range checks {
		out[i] = apigen.ProgrammeCheck{
			Id: c.ID, Section: c.Section,
			State: apigen.ProgrammeCheckState(c.State), Message: c.Message,
		}
	}
	return out, nil
}

// GetPortfolioChecks answers in the programme's shape: every check is
// advice read from other manifests (TAXONOMY.md D32).
func (s *Server) GetPortfolioChecks(ctx context.Context, req apigen.GetPortfolioChecksRequestObject) (apigen.GetPortfolioChecksResponseObject, error) {
	checks, err := s.Engine.PortfolioChecks(ctx, req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetPortfolioChecks404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	out := make(apigen.GetPortfolioChecks200JSONResponse, len(checks))
	for i, c := range checks {
		out[i] = apigen.ProgrammeCheck{
			Id: c.ID, Section: c.Section,
			State: apigen.ProgrammeCheckState(c.State), Message: c.Message,
		}
	}
	return out, nil
}

func (s *Server) GetProjectChecks(ctx context.Context, req apigen.GetProjectChecksRequestObject) (apigen.GetProjectChecksResponseObject, error) {
	checks, err := s.Engine.ProjectChecks(ctx, req.Id, false)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetProjectChecks404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	return apigen.GetProjectChecks200JSONResponse(toProjectChecks(checks)), nil
}

func (s *Server) GetProjectState(ctx context.Context, req apigen.GetProjectStateRequestObject) (apigen.GetProjectStateResponseObject, error) {
	st, err := s.Engine.GetProjectState(ctx, req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetProjectState404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	return apigen.GetProjectState200JSONResponse(toProjectState(st)), nil
}

// PostSnapshot records an explicit version of a manifest's current content
// with a reason. Snapshots the working copy if one exists, otherwise
// the current version. The author is from Settings.spec.operator.
func (s *Server) PostSnapshot(ctx context.Context, req apigen.PostSnapshotRequestObject) (apigen.PostSnapshotResponseObject, error) {
	reason := ""
	if req.Body != nil {
		reason = req.Body.Reason
	}
	settings, err := s.Engine.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	// Try to get the working copy first, fall back to current version
	yamlBytes, found, err := s.Engine.GetWorking(ctx, req.Kind, req.Id)
	if err != nil {
		return nil, err
	}
	if !found {
		// No working copy, use current version
		current, err := s.Engine.Get(ctx, req.Kind, req.Id)
		if err != nil {
			if errors.Is(err, engine.ErrNotFound) {
				return apigen.PostSnapshot404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
			}
			return nil, err
		}
		yamlBytes = current.YAML
	}

	v, err := s.Engine.Snapshot(ctx, req.Kind, req.Id, yamlBytes, reason, auth.PrincipalFrom(ctx).Actor(settings.Operator))
	if err != nil {
		var ve *engine.ValidationError
		if errors.As(err, &ve) {
			return apigen.PostSnapshot422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(ve.Problems))}, nil
		}
		return nil, err
	}
	return apigen.PostSnapshot200JSONResponse(toVersion(v)), nil
}

// GetWorking returns the working copy as last saved, decoded through the
// engine's codec, or 404 when the manifest has none.
func (s *Server) GetWorking(ctx context.Context, req apigen.GetWorkingRequestObject) (apigen.GetWorkingResponseObject, error) {
	text, found, err := s.Engine.GetWorking(ctx, req.Kind, req.Id)
	if err != nil {
		return nil, err
	}
	if !found {
		return apigen.GetWorking404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse{Problems: []apigen.Problem{{Message: "no working copy for " + req.Kind + "/" + req.Id}}}}, nil
	}
	view, err := s.buildManifestView(engine.Version{Kind: req.Kind, ID: req.Id, YAML: text})
	if err != nil {
		return nil, err
	}
	return apigen.GetWorking200JSONResponse{Yaml: string(text), Manifest: view.Manifest}, nil
}

// PutWorking saves a manifest's working copy without creating a version (autosave).
// DiscardWorking throws away a staged draft. Nothing is returned: the
// caller reads the manifest back, and what comes back is whatever the vault
// holds — the saved file, or nothing if it was never saved.
func (s *Server) DiscardWorking(ctx context.Context, req apigen.DiscardWorkingRequestObject) (apigen.DiscardWorkingResponseObject, error) {
	if err := s.Engine.DiscardWorking(ctx, req.Kind, req.Id); err != nil {
		return apigen.DiscardWorking422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList([]engine.Problem{
			{Message: err.Error()},
		}))}, nil
	}
	return apigen.DiscardWorking204Response{}, nil
}

func (s *Server) PutWorking(ctx context.Context, req apigen.PutWorkingRequestObject) (apigen.PutWorkingResponseObject, error) {
	yamlBytes, problem := s.resolveBody(&req.Body.Yaml, nil)
	if problem != nil {
		return apigen.PutWorking400JSONResponse{BadRequestJSONResponse: apigen.BadRequestJSONResponse(toProblemList([]engine.Problem{*problem}))}, nil
	}

	// Validate that the kind exists and basic YAML shape is OK
	doc, err := s.Engine.Codec().Decode(yamlBytes)
	if err != nil {
		return apigen.PutWorking422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList([]engine.Problem{
			{Message: "invalid " + s.Engine.Codec().Name() + ": " + err.Error()},
		}))}, nil
	}
	if doc == nil {
		return apigen.PutWorking422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList([]engine.Problem{
			{Message: "empty manifest"},
		}))}, nil
	}

	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	// Save to working copy, and into the shared draft
	if err := s.Engine.SaveWorking(ctx, req.Kind, req.Id, yamlBytes, actor); err != nil {
		var ce *store.ConflictError
		switch {
		case errors.As(err, &ce):
			// Exclusion conflict: return 409 with problem message containing the reason
			msg := string(ce.Theirs)
			if msg == "" {
				msg = fmt.Sprintf("cannot create working copy for %s/%s: conflict", ce.Kind, ce.ID)
			}
			return apigen.PutWorking409JSONResponse(toProblemList([]engine.Problem{
				{Message: msg},
			})), nil
		case errors.Is(err, engine.ErrUnknownKind):
			return apigen.PutWorking404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		default:
			return nil, err
		}
	}
	return apigen.PutWorking204Response{}, nil
}

func (s *Server) TransitionProjectState(ctx context.Context, req apigen.TransitionProjectStateRequestObject) (apigen.TransitionProjectStateResponseObject, error) {
	reason := ""
	if req.Body != nil && req.Body.Reason != nil {
		reason = *req.Body.Reason
	}
	to := ""
	if req.Body != nil {
		to = string(req.Body.To)
	}
	settings, err := s.Engine.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	actor := auth.PrincipalFrom(ctx).Actor(settings.Operator)

	// A handoff renders the charter and stores it as a bundle. The gate
	// is asked first so nothing is rendered for a project the engine
	// would refuse; the engine asks it again inside Handoff.
	if to == engine.ProjectStateHandedOff {
		snapshotNum, err := s.Engine.HandoffGate(ctx, req.Id)
		if err != nil {
			var ve *engine.ValidationError
			switch {
			case errors.As(err, &ve):
				return apigen.TransitionProjectState422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(ve.Problems))}, nil
			case errors.Is(err, engine.ErrNotFound):
				return apigen.TransitionProjectState404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
			default:
				return nil, err
			}
		}

		htmlBytes, jsonBytes, err := render.Charter(ctx, s.Engine, req.Id, snapshotNum)
		if err != nil {
			return nil, err
		}
		// A PDF is a convenience: no printer, no PDF, and the handoff
		// still goes through with the HTML and the JSON.
		pdfBytes, _ := s.Printer.Print(ctx, htmlBytes)

		_, err = s.Engine.Handoff(ctx, req.Id, actor, engine.HandoffRequest{
			HtmlBytes: htmlBytes,
			JsonBytes: jsonBytes,
			PdfBytes:  pdfBytes,
		})
		if err != nil {
			var ve *engine.ValidationError
			if errors.As(err, &ve) {
				return apigen.TransitionProjectState422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(ve.Problems))}, nil
			}
			return nil, err
		}

		st, err := s.Engine.GetProjectState(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		return apigen.TransitionProjectState200JSONResponse(toProjectState(st)), nil
	}

	// Normal state transition for non-handoff states
	st, err := s.Engine.TransitionProjectState(ctx, req.Id, to, actor, reason)
	if err != nil {
		var ve *engine.ValidationError
		switch {
		case errors.As(err, &ve):
			return apigen.TransitionProjectState422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(ve.Problems))}, nil
		case errors.Is(err, engine.ErrNotFound):
			return apigen.TransitionProjectState404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		default:
			return nil, err
		}
	}
	return apigen.TransitionProjectState200JSONResponse(toProjectState(st)), nil
}

func (s *Server) GetProjectCharterHtml(ctx context.Context, req apigen.GetProjectCharterHtmlRequestObject) (apigen.GetProjectCharterHtmlResponseObject, error) {
	// Determine snapshot: 0 for working copy, otherwise get latest
	snapshot := 0
	if req.Params.Working == nil || !*req.Params.Working {
		// Get latest snapshot
		versions, err := s.Engine.Versions(ctx, "Project", req.Id)
		if err != nil {
			if errors.Is(err, engine.ErrNotFound) {
				return apigen.GetProjectCharterHtml404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
			}
			return nil, err
		}
		if len(versions) == 0 {
			// No snapshot, return 404
			return apigen.GetProjectCharterHtml404JSONResponse{
				NotFoundJSONResponse: apigen.NotFoundJSONResponse{
					Problems: []apigen.Problem{{Message: "no snapshot yet"}},
				},
			}, nil
		}
		snapshot = versions[len(versions)-1].Number
	}

	// Render charter
	htmlBytes, _, err := render.Charter(ctx, s.Engine, req.Id, snapshot)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetProjectCharterHtml404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}

	return apigen.GetProjectCharterHtml200TexthtmlResponse{
		Body:          bytes.NewReader(htmlBytes),
		ContentLength: int64(len(htmlBytes)),
	}, nil
}

// GetProgrammeCharterHtml renders a programme as a document.
func (s *Server) GetProgrammeCharterHtml(
	ctx context.Context,
	req apigen.GetProgrammeCharterHtmlRequestObject,
) (apigen.GetProgrammeCharterHtmlResponseObject, error) {
	htmlBytes, err := render.ProgrammeCharter(ctx, s.Engine, req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetProgrammeCharterHtml404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	return apigen.GetProgrammeCharterHtml200TexthtmlResponse{
		Body:          bytes.NewReader(htmlBytes),
		ContentLength: int64(len(htmlBytes)),
	}, nil
}

// GetOperationCharterHtml renders an operation as its service description.
func (s *Server) GetOperationCharterHtml(
	ctx context.Context,
	req apigen.GetOperationCharterHtmlRequestObject,
) (apigen.GetOperationCharterHtmlResponseObject, error) {
	htmlBytes, err := render.OperationCharter(ctx, s.Engine, req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetOperationCharterHtml404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	return apigen.GetOperationCharterHtml200TexthtmlResponse{
		Body:          bytes.NewReader(htmlBytes),
		ContentLength: int64(len(htmlBytes)),
	}, nil
}

// GetCharterPdf prints the charter of a project, programme or operation to
// PDF. A project prints its working copy when asked, otherwise its latest
// version, and its working copy when it has no version yet: somebody
// exporting a draft wants the draft, not a refusal.
func (s *Server) GetCharterPdf(ctx context.Context, req apigen.GetCharterPdfRequestObject) (apigen.GetCharterPdfResponseObject, error) {
	var (
		html []byte
		err  error
	)
	switch req.Kind {
	case "Project":
		snapshot := 0
		if req.Params.Working == nil || !*req.Params.Working {
			if versions, verr := s.Engine.Versions(ctx, "Project", req.Id); verr == nil && len(versions) > 0 {
				snapshot = versions[len(versions)-1].Number
			}
		}
		html, _, err = render.Charter(ctx, s.Engine, req.Id, snapshot)
	case "Programme":
		html, err = render.ProgrammeCharter(ctx, s.Engine, req.Id)
	case "Operation":
		html, err = render.OperationCharter(ctx, s.Engine, req.Id)
	default:
		return apigen.GetCharterPdf404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse{
			Problems: []apigen.Problem{{Message: "only projects, programmes and operations have a charter"}},
		}}, nil
	}
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return apigen.GetCharterPdf404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	pdf, err := s.Printer.Print(ctx, html)
	if err == nil {
		return apigen.GetCharterPdf200ApplicationpdfResponse{Body: bytes.NewReader(pdf), ContentLength: int64(len(pdf))}, nil
	}
	return apigen.GetCharterPdf422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList([]engine.Problem{
		{Message: "The PDF could not be made: " + err.Error()},
	}))}, nil
}

func toProjectCheckFix(f engine.ProjectCheckFix) apigen.ProjectCheckFix {
	return apigen.ProjectCheckFix{Phase: apigen.ProjectCheckFixPhase(f.Phase), Section: f.Section}
}

func toProjectCheckItem(i engine.ProjectCheckItem) apigen.ProjectCheckItem {
	return apigen.ProjectCheckItem{
		Id: i.ID, Section: i.Section, Phase: apigen.ProjectCheckItemPhase(i.Phase),
		State: apigen.ProjectCheckState(i.State), Message: i.Message, Fix: toProjectCheckFix(i.Fix),
	}
}

func toProjectChecks(pc engine.ProjectChecks) apigen.ProjectChecks {
	items := make([]apigen.ProjectCheckItem, len(pc.Items))
	for i, it := range pc.Items {
		items[i] = toProjectCheckItem(it)
	}
	out := apigen.ProjectChecks{Blocking: pc.Blocking, Items: items}
	if pc.DerivedEnd != "" {
		de := pc.DerivedEnd
		out.DerivedEnd = &de
	}
	return out
}

func toProjectStateEntry(e engine.ProjectStateEntry) apigen.ProjectStateEntry {
	out := apigen.ProjectStateEntry{State: apigen.ProjectStateName(e.State), Actor: e.Actor, On: e.On}
	if e.Reason != "" {
		r := e.Reason
		out.Reason = &r
	}
	if e.Snapshot != 0 {
		s := e.Snapshot
		out.Snapshot = &s
	}
	if e.Bundle != "" {
		b := e.Bundle
		out.Bundle = &b
	}
	return out
}

func toProjectState(st engine.ProjectState) apigen.ProjectState {
	hist := make([]apigen.ProjectStateEntry, len(st.History))
	for i, h := range st.History {
		hist[i] = toProjectStateEntry(h)
	}
	return apigen.ProjectState{State: apigen.ProjectStateName(st.State), History: hist}
}

func (s *Server) ListKinds(_ context.Context, _ apigen.ListKindsRequestObject) (apigen.ListKindsResponseObject, error) {
	infos := s.Engine.Kinds()
	out := make(apigen.ListKinds200JSONResponse, len(infos))
	for i, ki := range infos {
		out[i] = apigen.KindCount{Kind: ki.Kind, Count: ki.Count}
		if ki.Summary != "" {
			out[i].Summary = &ki.Summary
		}
	}
	return out, nil
}

// rawSchemaResponse writes a schema's own bytes, so the order its author
// wrote the properties in survives the wire. The generated 200 response is
// a map, and a Go map has no order.
type rawSchemaResponse []byte

func (r rawSchemaResponse) VisitGetSchemaResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(r)
	return err
}

// rawFlowResponse serves a flow document's own bytes, like a schema's.
type rawFlowResponse []byte

func (r rawFlowResponse) VisitGetFlowResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(r)
	return err
}

func (s *Server) ListFlows(context.Context, apigen.ListFlowsRequestObject) (apigen.ListFlowsResponseObject, error) {
	return apigen.ListFlows200JSONResponse(s.Engine.FlowKinds()), nil
}

func (s *Server) GetFlow(_ context.Context, req apigen.GetFlowRequestObject) (apigen.GetFlowResponseObject, error) {
	raw, found, err := s.Engine.FlowJSON(req.Kind)
	if err != nil {
		return nil, err
	}
	if !found {
		return apigen.GetFlow404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse{Problems: []apigen.Problem{{Message: req.Kind + " has no flow; it is a sheet"}}}}, nil
	}
	return rawFlowResponse(raw), nil
}

func (s *Server) GetSchema(_ context.Context, req apigen.GetSchemaRequestObject) (apigen.GetSchemaResponseObject, error) {
	raw, err := s.Engine.SchemaJSON(req.Kind)
	if err != nil {
		if errors.Is(err, engine.ErrUnknownKind) {
			return apigen.GetSchema404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	return rawSchemaResponse(raw), nil
}

func (s *Server) ListManifests(ctx context.Context, req apigen.ListManifestsRequestObject) (apigen.ListManifestsResponseObject, error) {
	f := engine.Filter{}
	if req.Params.Q != nil {
		f.Q = *req.Params.Q
	}
	if req.Params.Ref != nil {
		for _, raw := range *req.Params.Ref {
			kind, id, ok := strings.Cut(raw, "/")
			if !ok || kind == "" || id == "" {
				return apigen.ListManifests404JSONResponse{}, fmt.Errorf("malformed ref filter %q, want Kind/id", raw)
			}
			f.Refs = append(f.Refs, engine.Ref{Kind: kind, ID: id})
		}
	}
	expand := req.Params.Expand != nil && *req.Params.Expand == apigen.Spec
	if req.Params.Limit == nil && req.Params.Cursor == nil {
		summaries, err := s.Engine.List(ctx, req.Kind, f, false)
		if err != nil {
			if errors.Is(err, engine.ErrUnknownKind) {
				return apigen.ListManifests404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
			}
			return nil, err
		}
		out, err := s.withProjectState(ctx, req.Kind, toSummaries(summaries))
		if err != nil {
			return nil, err
		}
		if expand {
			s.withSpecs(ctx, req.Kind, out)
		}
		var resp apigen.ListManifests200JSONResponse
		if err := resp.FromListManifests200JSONResponseBody0(out); err != nil {
			return nil, err
		}
		return resp, nil
	}

	limit, after := pageParams(req.Params.Limit, req.Params.Cursor)
	page, more, err := s.Engine.ListPage(ctx, req.Kind, f, after, limit)
	if err != nil {
		if errors.Is(err, engine.ErrUnknownKind) {
			return apigen.ListManifests404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	var next *string
	if more && len(page) > 0 {
		c := encodeCursor(page[len(page)-1].ID)
		next = &c
	}
	out, err := s.withProjectState(ctx, req.Kind, toSummaries(page))
	if err != nil {
		return nil, err
	}
	if expand {
		s.withSpecs(ctx, req.Kind, out)
	}
	var resp apigen.ListManifests200JSONResponse
	if err := resp.FromManifestList(apigen.ManifestList{Items: out, Next: next}); err != nil {
		return nil, err
	}
	return resp, nil
}

// withProjectState decorates every Project summary with its current state
// (Summary.state, per the contract, present only for kind Project). Every
// other kind is returned unchanged. A draft-only Summary
// (includeDrafts=true: Draft true, no committed version) is always "draft"
// by construction (a project with no committed version can have no state
// history at all: TransitionProjectState requires a committed project),
// so this skips the engine call for it rather than asking GetProjectState
// to fail with not-found.
func (s *Server) withProjectState(ctx context.Context, kind string, out []apigen.Summary) ([]apigen.Summary, error) {
	if kind != "Project" {
		return out, nil
	}
	ids := make([]string, 0, len(out))
	for _, sum := range out {
		if sum.Draft == nil || !*sum.Draft {
			ids = append(ids, sum.Id)
		}
	}
	// One read for the page, not one per project.
	states, err := s.Engine.ProjectStates(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		state := "draft"
		if st, ok := states[out[i].Id]; ok {
			state = st
		}
		out[i].State = &state
	}
	return out, nil
}

// pageParams reads a page request. The cursor is opaque to callers: it
// is the base64 encoding of the id of the last item on the previous
// page, so paging picks up right after it. limit defaults to 50 and is
// clamped to [1, 200]. A cursor that fails to decode (malformed, or from
// a different list) is treated as no cursor, starting from the first
// page, rather than an error.
func pageParams(limitParam *int, cursorParam *string) (limit int, afterID string) {
	limit = 50
	if limitParam != nil {
		limit = *limitParam
	}
	limit = max(1, min(limit, 200))
	if cursorParam != nil && *cursorParam != "" {
		if id, err := decodeCursor(*cursorParam); err == nil {
			afterID = id
		}
	}
	return limit, afterID
}

func encodeCursor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

func decodeCursor(c string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Server) GetManifest(ctx context.Context, req apigen.GetManifestRequestObject) (apigen.GetManifestResponseObject, error) {
	// An excluded manifest answers 404 with the way back.
	if exclusions, err := s.Engine.ListExcluded(ctx); err == nil {
		for _, excl := range exclusions {
			if excl.Kind == req.Kind && excl.ID == req.Id {
				notFoundMsg := fmt.Sprintf("excluded on %s: %s; recover to restore", excl.On.Format("2006-01-02"), excl.Reason)
				return apigen.GetManifest404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse{Problems: []apigen.Problem{{Message: notFoundMsg}}}}, nil
			}
		}
	}

	v, err := s.Engine.Get(ctx, req.Kind, req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrUnknownKind) || errors.Is(err, engine.ErrNotFound) {
			return apigen.GetManifest404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	view, err := s.buildManifestView(v)
	if err != nil {
		return nil, err
	}
	return apigen.GetManifest200JSONResponse(view), nil
}

func (s *Server) PutManifest(ctx context.Context, req apigen.PutManifestRequestObject) (apigen.PutManifestResponseObject, error) {
	yamlBytes, problem := s.resolveBody(req.Body.Yaml, req.Body.Manifest)
	if problem != nil {
		return apigen.PutManifest400JSONResponse{BadRequestJSONResponse: apigen.BadRequestJSONResponse(toProblemList([]engine.Problem{*problem}))}, nil
	}

	settings, err := s.Engine.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	var v engine.Version
	if req.Kind == "Project" {
		// CommitProject handles Project specially; the generic Commit
		// below stays kind-agnostic. See CommitProject's own doc comment.
		v, err = s.Engine.CommitProject(ctx, req.Id, yamlBytes, auth.PrincipalFrom(ctx).Actor(settings.Operator), req.Body.Reason)
	} else {
		v, err = s.Engine.Commit(ctx, req.Kind, req.Id, yamlBytes, auth.PrincipalFrom(ctx).Actor(settings.Operator), req.Body.Reason)
	}
	if err != nil {
		return putManifestError(err, yamlBytes)
	}
	return apigen.PutManifest200JSONResponse(toVersion(v)), nil
}

func putManifestError(err error, oursYAML []byte) (apigen.PutManifestResponseObject, error) {
	var ve *engine.ValidationError
	var ce *store.ConflictError
	switch {
	case errors.As(err, &ve):
		return apigen.PutManifest422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(ve.Problems))}, nil
	case errors.As(err, &ce):
		return apigen.PutManifest409JSONResponse{ConflictJSONResponse: apigen.ConflictJSONResponse(apigen.ConflictResponse{
			Ours:   string(ce.Ours),
			Theirs: string(ce.Theirs),
		})}, nil
	case errors.Is(err, engine.ErrConflict):
		return apigen.PutManifest409JSONResponse{ConflictJSONResponse: apigen.ConflictJSONResponse(apigen.ConflictResponse{
			Ours:   string(oursYAML),
			Theirs: "",
		})}, nil
	case errors.Is(err, engine.ErrUnknownKind):
		return apigen.PutManifest404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
	default:
		return nil, err
	}
}

func (s *Server) DiffManifest(ctx context.Context, req apigen.DiffManifestRequestObject) (apigen.DiffManifestResponseObject, error) {
	changes, err := s.Engine.Diff(ctx, req.Kind, req.Id, req.Params.From, req.Params.To)
	if err != nil {
		if errors.Is(err, engine.ErrUnknownKind) || errors.Is(err, engine.ErrNotFound) {
			return apigen.DiffManifest404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	return apigen.DiffManifest200JSONResponse(toChanges(changes)), nil
}

func (s *Server) GetReferences(ctx context.Context, req apigen.GetReferencesRequestObject) (apigen.GetReferencesResponseObject, error) {
	refs, err := s.Engine.References(ctx, req.Kind, req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrUnknownKind) {
			return apigen.GetReferences404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	return apigen.GetReferences200JSONResponse(toReferences(refs)), nil
}

func (s *Server) ListVersions(ctx context.Context, req apigen.ListVersionsRequestObject) (apigen.ListVersionsResponseObject, error) {
	if _, err := s.Engine.Get(ctx, req.Kind, req.Id); err != nil {
		if errors.Is(err, engine.ErrUnknownKind) || errors.Is(err, engine.ErrNotFound) {
			return apigen.ListVersions404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	vs, err := s.Engine.Versions(ctx, req.Kind, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.ListVersions200JSONResponse(toVersions(vs)), nil
}

func (s *Server) GetVersion(ctx context.Context, req apigen.GetVersionRequestObject) (apigen.GetVersionResponseObject, error) {
	v, err := s.Engine.GetVersion(ctx, req.Kind, req.Id, req.N)
	if err != nil {
		if errors.Is(err, engine.ErrUnknownKind) || errors.Is(err, engine.ErrNotFound) {
			return apigen.GetVersion404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	view, err := s.buildManifestView(v)
	if err != nil {
		return nil, err
	}
	return apigen.GetVersion200JSONResponse(view), nil
}

func (s *Server) ValidateManifest(ctx context.Context, req apigen.ValidateManifestRequestObject) (apigen.ValidateManifestResponseObject, error) {
	yamlBytes, problem := s.resolveBody(req.Body.Yaml, req.Body.Manifest)
	if problem != nil {
		return apigen.ValidateManifest400JSONResponse{BadRequestJSONResponse: apigen.BadRequestJSONResponse(toProblemList([]engine.Problem{*problem}))}, nil
	}
	problems, err := s.Engine.Validate(ctx, req.Kind, yamlBytes)
	if err != nil {
		if errors.Is(err, engine.ErrUnknownKind) {
			return apigen.ValidateManifest404JSONResponse{NotFoundJSONResponse: notFound(err)}, nil
		}
		return nil, err
	}
	return apigen.ValidateManifest200JSONResponse(toProblemList(problems)), nil
}

// resolveBody picks yaml text out of a write request body: either the yaml
// field verbatim, or the manifest field marshalled to YAML. Exactly one
// must be present.
func (s *Server) resolveBody(y *string, manifest *map[string]interface{}) ([]byte, *engine.Problem) {
	haveYAML := y != nil && strings.TrimSpace(*y) != ""
	haveManifest := manifest != nil
	if haveYAML == haveManifest {
		return nil, &engine.Problem{Message: "exactly one of yaml or manifest is required"}
	}
	if haveYAML {
		return []byte(*y), nil
	}
	b, err := s.Engine.Codec().Encode(*manifest)
	if err != nil {
		return nil, &engine.Problem{Message: "manifest could not be converted to " + s.Engine.Codec().Name() + ": " + err.Error()}
	}
	return b, nil
}

func notFound(err error) apigen.NotFoundJSONResponse {
	return apigen.NotFoundJSONResponse{Problems: []apigen.Problem{{Message: err.Error()}}}
}

func toProblemList(problems []engine.Problem) apigen.ProblemList {
	out := make([]apigen.Problem, len(problems))
	for i, p := range problems {
		out[i] = apigen.Problem{Path: p.Path, Message: p.Message}
	}
	return apigen.ProblemList{Problems: out}
}

// withSpecs carries each summary's current spec (expand=spec), so a list
// can summarise what every record holds in one request. A record that
// cannot be read keeps its summary without a spec.
func (s *Server) withSpecs(ctx context.Context, kind string, out []apigen.Summary) {
	ids := make([]string, len(out))
	at := make(map[string]int, len(out))
	for i, sum := range out {
		ids[i] = sum.Id
		at[sum.Id] = i
	}
	// One read for the page, not one per manifest.
	versions, err := s.Engine.GetMany(ctx, kind, ids)
	if err != nil {
		return
	}
	for _, v := range versions {
		var doc manifestDoc
		if s.Engine.Codec().DecodeInto(v.YAML, &doc) != nil || doc.Spec == nil {
			continue
		}
		spec := doc.Spec
		out[at[v.ID]].Spec = &spec
	}
}

func toSummaries(ss []engine.Summary) []apigen.Summary {
	out := make([]apigen.Summary, len(ss))
	for i, s := range ss {
		out[i] = apigen.Summary{Kind: s.Kind, Id: s.ID, Name: s.Name, Version: s.Version, UpdatedOn: s.UpdatedOn}
		if len(s.Labels) > 0 {
			labels := s.Labels
			out[i].Labels = &labels
		}
		if s.Draft {
			draft := true
			out[i].Draft = &draft
		}
	}
	return out
}

func toVersion(v engine.Version) apigen.Version {
	return apigen.Version{Kind: v.Kind, Id: v.ID, Number: v.Number, Actor: v.Actor, Reason: v.Reason, On: v.On}
}

func toVersions(vs []engine.Version) []apigen.Version {
	out := make([]apigen.Version, len(vs))
	for i, v := range vs {
		out[i] = toVersion(v)
	}
	return out
}

func toChanges(cs []engine.Change) []apigen.Change {
	out := make([]apigen.Change, len(cs))
	for i, c := range cs {
		out[i] = apigen.Change{Path: c.Path, Op: apigen.ChangeOp(c.Op), From: c.From, To: c.To}
	}
	return out
}

func toRef(r engine.Ref) apigen.Ref {
	out := apigen.Ref{Kind: r.Kind, Id: r.ID}
	if r.Path != "" {
		path := r.Path
		out.Path = &path
	}
	return out
}

func toReferences(r engine.Refs) apigen.References {
	outgoing := make([]apigen.Ref, len(r.Outgoing))
	for i, ref := range r.Outgoing {
		outgoing[i] = toRef(ref)
	}
	incoming := toSummaries(r.Incoming)
	if incoming == nil {
		incoming = []apigen.Summary{}
	}
	return apigen.References{Outgoing: outgoing, Incoming: incoming}
}

// manifestDoc is enough of a manifest's shape to build the generic
// apigen.Manifest envelope from its raw YAML.
type manifestDoc struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Metadata   manifestMeta   `yaml:"metadata"`
	Spec       map[string]any `yaml:"spec"`
}

type manifestMeta struct {
	ID      string            `yaml:"id"`
	Name    string            `yaml:"name"`
	Alias   string            `yaml:"alias"`
	Labels  map[string]string `yaml:"labels"`
	Pending []manifestPending `yaml:"pending"`
}

type manifestPending struct {
	Path string `yaml:"path"`
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
	Note string `yaml:"note"`
}

func (s *Server) buildManifestView(v engine.Version) (apigen.ManifestView, error) {
	var doc manifestDoc
	if err := s.Engine.Codec().DecodeInto(v.YAML, &doc); err != nil {
		return apigen.ManifestView{}, fmt.Errorf("parse stored manifest: %w", err)
	}
	m := apigen.Manifest{ApiVersion: doc.APIVersion, Kind: doc.Kind, Spec: doc.Spec}
	if m.Spec == nil {
		m.Spec = map[string]any{}
	}
	m.Metadata.Id = doc.Metadata.ID
	m.Metadata.Name = doc.Metadata.Name
	if doc.Metadata.Alias != "" {
		alias := doc.Metadata.Alias
		m.Metadata.Alias = &alias
	}
	if len(doc.Metadata.Labels) > 0 {
		labels := doc.Metadata.Labels
		m.Metadata.Labels = &labels
	}
	// The placeholders are kept so an edit made from this copy keeps them
	// (TAXONOMY.md D31).
	if len(doc.Metadata.Pending) > 0 {
		pending := make([]apigen.Pending, len(doc.Metadata.Pending))
		for i, p := range doc.Metadata.Pending {
			pending[i] = apigen.Pending{Path: p.Path, Kind: p.Kind, Name: p.Name}
			if p.Note != "" {
				note := p.Note
				pending[i].Note = &note
			}
		}
		m.Metadata.Pending = &pending
	}
	return apigen.ManifestView{Version: toVersion(v), Manifest: m, Yaml: string(v.YAML)}, nil
}

// toVault maps the engine's state to the contract's Vault manifest.
func toVault(st engine.State) apigen.Vault {
	include := st.Included
	return apigen.Vault{
		ApiVersion: "cartograph/v1",
		Kind:       "Vault",
		Metadata: struct {
			Id string `json:"id"`
		}{Id: st.Name},
		Spec: struct {
			Include *[]string `json:"include,omitempty"`
		}{Include: &include},
	}
}

// GetVault returns the state manifest: what the live state includes.
func (s *Server) GetVault(ctx context.Context, _ apigen.GetVaultRequestObject) (apigen.GetVaultResponseObject, error) {
	st, err := s.Engine.GetState(ctx)
	if err != nil {
		if errors.Is(err, engine.ErrNoState) {
			return apigen.GetVault400JSONResponse{BadRequestJSONResponse: apigen.BadRequestJSONResponse{Problems: []apigen.Problem{{Message: err.Error()}}}}, nil
		}
		return nil, err
	}
	return apigen.GetVault200JSONResponse(toVault(st)), nil
}

// ListUnapplied lists manifests present but not in the live state.
func (s *Server) ListUnapplied(ctx context.Context, _ apigen.ListUnappliedRequestObject) (apigen.ListUnappliedResponseObject, error) {
	refs, err := s.Engine.ListUnapplied(ctx)
	if err != nil {
		if errors.Is(err, engine.ErrNoState) {
			return apigen.ListUnapplied200JSONResponse{}, nil
		}
		return nil, err
	}
	out := make([]apigen.ManifestRef, len(refs))
	for i, r := range refs {
		out[i] = apigen.ManifestRef{Kind: r.Kind, Id: r.ID, Name: r.Name}
	}
	return apigen.ListUnapplied200JSONResponse(out), nil
}

// ApplyRef includes one manifest, or many, in the live state. Many in one
// call, because one at a time was quadratic and felt it:
// the engine reloads and reindexes once for the batch.
func (s *Server) ApplyRef(ctx context.Context, request apigen.ApplyRefRequestObject) (apigen.ApplyRefResponseObject, error) {
	refused := func(problems []engine.Problem) apigen.ApplyRefResponseObject {
		return apigen.ApplyRef422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(problems))}
	}
	var refs []string
	if request.Body.Refs != nil {
		refs = append(refs, *request.Body.Refs...)
	}
	if request.Body.Ref != nil && *request.Body.Ref != "" {
		refs = append(refs, *request.Body.Ref)
	}
	st, err := s.Engine.Apply(ctx, refs)
	if err != nil {
		var ve *engine.ValidationError
		switch {
		case errors.As(err, &ve):
			return refused(ve.Problems), nil
		case errors.Is(err, engine.ErrNoState):
			return refused([]engine.Problem{{Message: err.Error()}}), nil
		default:
			return nil, err
		}
	}
	return apigen.ApplyRef200JSONResponse(toVault(st)), nil
}

// ListExcluded lists every excluded manifest, newest first.
func (s *Server) ListExcluded(ctx context.Context, _ apigen.ListExcludedRequestObject) (apigen.ListExcludedResponseObject, error) {
	exclusions, err := s.Engine.ListExcluded(ctx)
	if err != nil {
		if errors.Is(err, engine.ErrNoState) {
			return apigen.ListExcluded200JSONResponse{}, nil
		}
		return nil, err
	}
	result := make([]apigen.Exclusion, len(exclusions))
	for i, e := range exclusions {
		result[i] = apigen.Exclusion{Kind: e.Kind, Id: e.ID, Name: e.Name, On: e.On, Reason: e.Reason}
	}
	return apigen.ListExcluded200JSONResponse(result), nil
}

// RecoverRef re-includes an excluded manifest in the live state.
func (s *Server) RecoverRef(ctx context.Context, request apigen.RecoverRefRequestObject) (apigen.RecoverRefResponseObject, error) {
	refused := func(problems []engine.Problem) apigen.RecoverRefResponseObject {
		return apigen.RecoverRef422JSONResponse{UnprocessableJSONResponse: apigen.UnprocessableJSONResponse(toProblemList(problems))}
	}
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	st, err := s.Engine.Recover(ctx, request.Body.Ref, actor, "recovered via API")
	if err != nil {
		var ve *engine.ValidationError
		switch {
		case errors.As(err, &ve):
			return refused(ve.Problems), nil
		case errors.Is(err, engine.ErrNoState):
			return refused([]engine.Problem{{Message: err.Error()}}), nil
		default:
			return nil, err
		}
	}
	return apigen.RecoverRef200JSONResponse(toVault(st)), nil
}

// ListSnapshots returns every snapshot (version) across all kinds, newest first.
func (s *Server) ListSnapshots(ctx context.Context, req apigen.ListSnapshotsRequestObject) (apigen.ListSnapshotsResponseObject, error) {
	limit := 20 // default
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	cursor := ""
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}

	versions, nextCursor, err := s.Engine.ListAllVersions(ctx, limit, cursor)
	if err != nil {
		return nil, err
	}

	// Convert versions to snapshots
	snapshots := make([]apigen.Snapshot, len(versions))
	for i, v := range versions {
		name := nameOf(s.Engine.Codec(), v.YAML)
		snapshot := apigen.Snapshot{
			Version:    fmt.Sprintf("v%d", v.Number),
			Definition: &name,
			Kind:       v.Kind,
			Id:         v.ID,
			Number:     v.Number,
			Reason:     v.Reason,
			On:         v.On,
		}

		// For Project kind, lookup bundle from state history
		if v.Kind == "Project" {
			state, err := s.Engine.GetProjectState(ctx, v.ID)
			if err == nil {
				// Find state entry for this snapshot number
				for _, entry := range state.History {
					if entry.Snapshot == v.Number && entry.Bundle != "" {
						snapshot.Bundle = &entry.Bundle
						break
					}
				}
			}
		}

		snapshots[i] = snapshot
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}

	return apigen.ListSnapshots200JSONResponse{
		Snapshots: snapshots,
		Cursor:    cursorPtr,
	}, nil
}

// nameOf reads metadata.name through the codec; an unreadable text has
// no name rather than an error, since the store accepted it already.
func nameOf(c codec.Codec, text []byte) string {
	doc, err := c.Decode(text)
	if err != nil {
		return ""
	}
	md, _ := doc["metadata"].(map[string]any)
	name, _ := md["name"].(string)
	return name
}

// GetGlossary is every word of the taxonomy, defined plainly, in the order
// of work.
func (s *Server) GetGlossary(_ context.Context, req apigen.GetGlossaryRequestObject) (apigen.GetGlossaryResponseObject, error) {
	locale := ""
	if req.Params.Locale != nil {
		locale = *req.Params.Locale
	}
	g, err := s.Engine.Glossary(locale)
	if err != nil {
		return nil, err
	}
	out := make(apigen.GetGlossary200JSONResponse, len(g))
	for i, e := range g {
		out[i] = apigen.GlossaryEntry{Key: e.Key, Kind: e.Kind, Summary: e.Summary}
		if e.Level != "" {
			out[i].Level = &e.Level
		}
		if e.Example != "" {
			out[i].Example = &e.Example
		}
		if len(e.After) > 0 {
			out[i].After = &e.After
		}
		if e.Register {
			out[i].Register = &e.Register
		}
	}
	return out, nil
}

// GetOrder is the order of work and how far the workspace has got.
func (s *Server) GetOrder(ctx context.Context, _ apigen.GetOrderRequestObject) (apigen.GetOrderResponseObject, error) {
	o, err := s.Engine.WorkspaceOrder(ctx)
	if err != nil {
		return nil, err
	}
	out := apigen.Order{Stages: make([]apigen.OrderStage, len(o.Stages)), Registers: make([]apigen.KindCount, len(o.Registers))}
	for i, st := range o.Stages {
		out.Stages[i] = apigen.OrderStage{Key: st.Key, Kind: st.Kind, Count: st.Count, State: apigen.OrderStageState(st.State)}
		if st.Level != "" {
			out.Stages[i].Level = &st.Level
		}
		if len(st.After) > 0 {
			out.Stages[i].After = &st.After
		}
		if st.Optional {
			out.Stages[i].Optional = &st.Optional
		}
		if len(st.Waiting) > 0 {
			out.Stages[i].Waiting = &st.Waiting
		}
	}
	for i, r := range o.Registers {
		out.Registers[i] = apigen.KindCount{Kind: r.Kind, Count: r.Count}
	}
	if o.Next != "" {
		out.Next = &o.Next
	}
	return apigen.GetOrder200JSONResponse(out), nil
}

func (s *Server) GetGraph(ctx context.Context, req apigen.GetGraphRequestObject) (apigen.GetGraphResponseObject, error) {
	var focus *engine.Ref
	if req.Params.Focus != nil {
		if kind, id, ok := strings.Cut(*req.Params.Focus, "/"); ok && kind != "" && id != "" {
			focus = &engine.Ref{Kind: kind, ID: id}
		}
	}
	g, err := s.Engine.Graph(ctx, focus)
	if err != nil {
		return nil, err
	}
	out := apigen.Graph{Nodes: make([]apigen.GraphNode, len(g.Nodes)), Edges: make([]apigen.GraphEdge, len(g.Edges))}
	for i, n := range g.Nodes {
		out.Nodes[i] = apigen.GraphNode{Kind: n.Kind, Id: n.ID, Name: n.Name, X: n.X, Y: n.Y}
		if n.Level != "" {
			out.Nodes[i].Level = &n.Level
		}
		if n.Distance >= 0 {
			out.Nodes[i].Distance = &n.Distance
		}
		if n.Stage != "" {
			out.Nodes[i].Stage = &n.Stage
		}
		out.Nodes[i].Layer = &n.Layer
	}
	for i, e := range g.Edges {
		out.Edges[i] = apigen.GraphEdge{From: apigen.Ref{Kind: e.From.Kind, Id: e.From.ID}, To: apigen.Ref{Kind: e.To.Kind, Id: e.To.ID}}
	}
	return apigen.GetGraph200JSONResponse(out), nil
}

// Understand is what a typed text reads as, and what already says it.
func (s *Server) Understand(ctx context.Context, req apigen.UnderstandRequestObject) (apigen.UnderstandResponseObject, error) {
	locale := ""
	if req.Body.Locale != nil {
		locale = *req.Body.Locale
	}
	u, err := s.Engine.Understand(ctx, req.Body.Text, locale)
	if err != nil {
		return nil, err
	}
	var out apigen.Understanding
	if err := convertJSON(u, &out); err != nil {
		return nil, err
	}
	return apigen.Understand200JSONResponse(out), nil
}

// Relevant is what in the workspace is relevant to a piece of work.
func (s *Server) Relevant(ctx context.Context, req apigen.RelevantRequestObject) (apigen.RelevantResponseObject, error) {
	var kinds []string
	if req.Body.Kinds != nil {
		kinds = *req.Body.Kinds
	}
	level, limit := "", 0
	if req.Body.Level != nil {
		level = *req.Body.Level
	}
	if req.Body.Limit != nil {
		limit = *req.Body.Limit
	}
	r, err := s.Engine.Relevant(ctx, req.Body.Text, kinds, level, limit)
	if err != nil {
		return nil, err
	}
	var out apigen.Relevance
	if err := convertJSON(r, &out); err != nil {
		return nil, err
	}
	return apigen.Relevant200JSONResponse(out), nil
}

// FromIdea is the sentence of an idea that answers each question a walk
// asks.
func (s *Server) FromIdea(ctx context.Context, req apigen.FromIdeaRequestObject) (apigen.FromIdeaResponseObject, error) {
	answers, ok := s.Engine.FromIdea(ctx, req.Body.Kind, req.Body.Idea)
	var out apigen.IdeaReading
	if err := convertJSON(map[string]any{"available": ok, "answers": answers}, &out); err != nil {
		return nil, err
	}
	return apigen.FromIdea200JSONResponse(out), nil
}

// GetDecisionModel is whether a decision model is configured and ready.
func (s *Server) GetDecisionModel(ctx context.Context, _ apigen.GetDecisionModelRequestObject) (apigen.GetDecisionModelResponseObject, error) {
	m := s.Engine.DecisionModel(ctx)
	return apigen.GetDecisionModel200JSONResponse(apigen.DecisionModel{Configured: m.Configured, Ready: m.Ready}), nil
}

// MatchExisting is the records of a kind that say what a text says.
func (s *Server) MatchExisting(ctx context.Context, req apigen.MatchExistingRequestObject) (apigen.MatchExistingResponseObject, error) {
	level := ""
	if req.Body.Level != nil {
		level = *req.Body.Level
	}
	var out []apigen.Match
	if err := convertJSON(s.Engine.MatchExisting(ctx, req.Body.Kind, level, req.Body.Text), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []apigen.Match{}
	}
	return apigen.MatchExisting200JSONResponse(out), nil
}

// convertJSON copies an engine value into its generated wire type, field
// for field, by their shared JSON names.
func convertJSON(from, to any) error {
	b, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, to)
}
