package api

import (
	"context"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// GetLinkCandidates is what a link may join from one record, read as the
// change set named would leave the workspace.
func (s *Server) GetLinkCandidates(ctx context.Context, req apigen.GetLinkCandidatesRequestObject) (apigen.GetLinkCandidatesResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	problem := ""
	if req.Params.Problem != nil {
		problem = *req.Params.Problem
	}
	cands, err := s.Engine.LinkCandidates(ctx, string(req.Link), req.Params.From, problem)
	if err != nil {
		return nil, err
	}
	out := make(apigen.GetLinkCandidates200JSONResponse, len(cands))
	for i, c := range cands {
		out[i] = apigen.LinkCandidate{Kind: c.Kind, Id: c.ID, Name: c.Name, Allowed: c.Allowed}
		if c.Linked {
			linked := true
			out[i].Linked = &linked
		}
		if c.Reason != "" {
			reason := c.Reason
			out[i].Reason = &reason
		}
	}
	return out, nil
}

// GetComponents is the graph of components across the workspace, read as
// the change set named would leave it.
func (s *Server) GetComponents(ctx context.Context, req apigen.GetComponentsRequestObject) (apigen.GetComponentsResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	g, err := s.Engine.Components(ctx)
	if err != nil {
		return nil, err
	}
	ref := func(r engine.Ref) apigen.WorkRef { return apigen.WorkRef{Kind: apigen.WorkRefKind(r.Kind), Id: r.ID} }
	refs := func(rs []engine.Ref) []apigen.WorkRef {
		out := make([]apigen.WorkRef, len(rs))
		for i, r := range rs {
			out[i] = ref(r)
		}
		return out
	}
	out := apigen.GetComponents200JSONResponse{
		Nodes:          make([]apigen.ComponentNode, len(g.Nodes)),
		Edges:          make([]apigen.ComponentEdge, len(g.Edges)),
		Loops:          make([][]apigen.WorkRef, len(g.Loops)),
		CriticalPath:   refs(g.CriticalPath),
		CriticalMonths: g.CriticalMonths,
	}
	for i, n := range g.Nodes {
		out.Nodes[i] = apigen.ComponentNode{
			Kind: apigen.ComponentNodeKind(n.Kind), Id: n.ID, Name: n.Name, Months: n.Months,
			Dependents: n.Dependents, Critical: n.Critical, InLoop: n.InLoop, MostDependedOn: n.MostDependedOn,
		}
	}
	for i, ed := range g.Edges {
		out.Edges[i] = apigen.ComponentEdge{From: ref(ed.From), To: ref(ed.To)}
		if ed.Why != "" {
			why := ed.Why
			out.Edges[i].Why = &why
		}
		if ed.Legacy {
			legacy := true
			out.Edges[i].Legacy = &legacy
		}
	}
	for i, l := range g.Loops {
		out.Loops[i] = refs(l)
	}
	return out, nil
}

// GetProjectSchedule is a project's milestones placed on time, read as the
// change set named would leave it.
func (s *Server) GetProjectSchedule(ctx context.Context, req apigen.GetProjectScheduleRequestObject) (apigen.GetProjectScheduleResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	items, err := s.Engine.Schedule(ctx, string(req.Id))
	if err != nil {
		return nil, err
	}
	out := make(apigen.GetProjectSchedule200JSONResponse, len(items))
	for i, it := range items {
		waits := it.WaitsOn
		if waits == nil {
			waits = []string{}
		}
		o := apigen.ScheduleItem{Id: it.ID, Name: it.Name, WaitsOn: waits, Pending: it.Pending, Late: it.Late, Critical: it.Critical, Unplaced: it.Unplaced}
		if it.Form != "" {
			f := apigen.ScheduleItemForm(it.Form)
			o.Form = &f
		}
		for _, p := range []struct {
			v   string
			dst **string
		}{{it.Month, &o.Month}, {it.NotBefore, &o.NotBefore}, {it.NotAfter, &o.NotAfter}} {
			if p.v != "" {
				v := p.v
				*p.dst = &v
			}
		}
		out[i] = o
	}
	return out, nil
}

// GetProjectWaits is what a project's dated items wait on, across kinds,
// laid out by the engine (TAXONOMY.md D47, D48).
func (s *Server) GetProjectWaits(ctx context.Context, req apigen.GetProjectWaitsRequestObject) (apigen.GetProjectWaitsResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	g, err := s.Engine.Waits(ctx, string(req.Id))
	if err != nil {
		return nil, err
	}
	out := apigen.GetProjectWaits200JSONResponse{Nodes: make([]apigen.WaitsNode, len(g.Nodes))}
	out.Edges = make([]struct {
		From int `json:"from"`
		To   int `json:"to"`
	}, len(g.Edges))
	for i, ed := range g.Edges {
		out.Edges[i].From, out.Edges[i].To = ed.From, ed.To
	}
	for i, n := range g.Nodes {
		out.Nodes[i] = waitsNode(n)
	}
	return out, nil
}

// waitsNode is a dated item as the contract says it.
func waitsNode(n engine.TimeNode) apigen.WaitsNode {
	text := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	o := apigen.WaitsNode{Kind: apigen.WaitsNodeKind(n.Kind), Name: n.Name, Critical: n.Critical, Conflict: n.Conflict, Late: n.Late, Unplaced: n.Unplaced,
		X: float32(n.X), Y: float32(n.Y), Item: text(n.Item), Follows: text(n.Follows), Month: text(n.Month)}
	o.Record.Kind, o.Record.Id = apigen.WaitsNodeRecordKind(n.Record.Kind), n.Record.ID
	if n.Timing != nil {
		t := n.Timing
		o.Timing = &t
	}
	if len(n.Risks) > 0 {
		r := n.Risks
		o.Risks = &r
	}
	return o
}

// MatchWhatHappened ranks a project's items by what a person says happened
// (TAXONOMY.md D59).
func (s *Server) MatchWhatHappened(ctx context.Context, req apigen.MatchWhatHappenedRequestObject) (apigen.MatchWhatHappenedResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	text := ""
	if req.Body != nil {
		text = req.Body.Text
	}
	h, err := s.Engine.WhatHappened(ctx, string(req.Id), text)
	if err != nil {
		return nil, err
	}
	out := apigen.MatchWhatHappened200JSONResponse{Available: h.Available}
	out.Matches = make([]struct {
		Happens    []string                   `json:"happens"`
		Item       string                     `json:"item"`
		Kind       apigen.HappenedMatchesKind `json:"kind"`
		Likelihood *float32                   `json:"likelihood,omitempty"`
		Name       string                     `json:"name"`
	}, len(h.Matches))
	for i, m := range h.Matches {
		out.Matches[i].Happens, out.Matches[i].Item, out.Matches[i].Kind, out.Matches[i].Name = m.Happens, m.Item, apigen.HappenedMatchesKind(m.Kind), m.Name
		if h.Available {
			l := float32(m.Likelihood)
			out.Matches[i].Likelihood = &l
		}
	}
	return out, nil
}

// GetWhatATriggerReaches is every item a trigger on one item reaches
// (TAXONOMY.md D59).
func (s *Server) GetWhatATriggerReaches(ctx context.Context, req apigen.GetWhatATriggerReachesRequestObject) (apigen.GetWhatATriggerReachesResponseObject, error) {
	ctx, err := s.previewing(ctx, req.Params.ChangeSet)
	if err != nil {
		return nil, err
	}
	nodes, err := s.Engine.Affects(ctx, string(req.Id), req.Params.Item)
	if err != nil {
		return nil, err
	}
	out := make(apigen.GetWhatATriggerReaches200JSONResponse, len(nodes))
	for i, n := range nodes {
		out[i] = waitsNode(n)
	}
	return out, nil
}
