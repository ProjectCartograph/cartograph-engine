package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// byPerson marks every request through the API as a person's own, so the
// engine records what it decides for them (docs/adr/0034). Agents reach
// the engine through MCP, not here, and the engine leaves them out again
// by their principal.
func byPerson(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(engine.ByPerson(r.Context())))
	})
}

// clockSkew is how far an interface's clock may be from the server's
// before its time is replaced by the server's.
const clockSkew = 24 * time.Hour

// RecordEvents keeps an interface's acts on the people's trace. The batch
// is refused whole when any act could carry something a person wrote, so
// an interface that gets it wrong learns at once rather than leaking.
func (s *Server) RecordEvents(ctx context.Context, req apigen.RecordEventsRequestObject) (apigen.RecordEventsResponseObject, error) {
	rec := s.Engine.Activity()
	if activity.IsOff(rec) {
		return apigen.RecordEvents404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse{Problems: []apigen.Problem{{Message: "the people's trace is off (CARTOGRAPH_UI_TRACE)"}}}}, nil
	}
	b := req.Body
	p := auth.PrincipalFrom(ctx)
	now := time.Now().UTC()
	acts := make([]activity.Event, 0, len(b.Events))
	var problems []apigen.Problem
	for i, in := range b.Events {
		act := activity.Event{
			At: now, Name: string(in.Name), Source: activity.Interface, Session: b.Session,
			Person: p.Subject, Interface: deref(b.Interface), Surface: deref(in.Surface), Kind: deref(in.Kind),
			Record: deref(in.Record), Step: deref(in.Step), Field: deref(in.Field), ChangeSet: deref(in.ChangeSet),
		}
		if in.At != nil && in.At.Sub(now).Abs() < clockSkew {
			act.At = in.At.UTC()
		}
		if in.Target != nil {
			act.Target = string(*in.Target)
		}
		if in.Outcome != nil {
			act.Outcome = string(*in.Outcome)
		}
		if in.Millis != nil {
			act.Millis = int64(*in.Millis)
		}
		if in.Sign != nil {
			act.Sign = *in.Sign
		}
		valid, err := activity.NewInterfaceAct(act)
		if err != nil {
			problems = append(problems, apigen.Problem{Path: fmt.Sprintf("/events/%d", i), Message: err.Error()})
			continue
		}
		acts = append(acts, valid)
	}
	if err := activity.CheckBatch(b.Session, deref(b.Interface)); err != nil {
		problems = append(problems, apigen.Problem{Path: "/session", Message: err.Error()})
	}
	if len(problems) > 0 {
		return apigen.RecordEvents400JSONResponse{BadRequestJSONResponse: apigen.BadRequestJSONResponse{Problems: problems}}, nil
	}
	if p.Agent != "" {
		// An agent's work is on the agents' trace; it sees no interface.
		return apigen.RecordEvents202JSONResponse{Kept: 0}, nil
	}
	for _, act := range acts {
		rec.Record(act)
	}
	return apigen.RecordEvents202JSONResponse{Kept: len(acts)}, nil
}
