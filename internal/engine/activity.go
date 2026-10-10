package engine

import (
	"context"
	"errors"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
)

// WithActivity keeps what the engine decides on a person's behalf by its
// shape (docs/adr/0034): a draft kept, a version saved or refused, a change
// set opened, rolled in or closed. Off when not given.
func WithActivity(r activity.Recorder) Option {
	return func(e *Engine) { e.activity = r }
}

// Activity is the recorder people's acts go to, Off when none was given,
// for the driving adapters that take an interface's own acts.
func (e *Engine) Activity() activity.Recorder {
	if e.activity == nil {
		return activity.Off{}
	}
	return e.activity
}

type byPersonKey struct{}

// ByPerson marks ctx as a person's own request, made through an
// interface. Only acts on such a context are recorded: the engine's own
// work (an index rebuilt, a shared draft flushed, the standard units
// written) and an agent's calls are not a person's work.
func ByPerson(ctx context.Context) context.Context {
	return context.WithValue(ctx, byPersonKey{}, true)
}

// note records act for the person on ctx, when ctx is a person's request
// and a recorder is set.
func (e *Engine) note(ctx context.Context, act activity.Event) {
	if activity.IsOff(e.activity) || ctx.Value(byPersonKey{}) == nil {
		return
	}
	p := identity.PrincipalFrom(ctx)
	if p.Agent != "" {
		return
	}
	act.At = timeNow().UTC()
	act.Source = activity.Server
	act.Person = p.Subject
	if act.ChangeSet == "" {
		if set, ok := ctx.Value(changeSetIDKey{}).(string); ok {
			act.ChangeSet = set
		}
	}
	e.activity.Record(act)
}

// noteOutcome records act with the outcome err gives it: refused, with
// the fields named, for problems the person can fix; failed for anything
// else.
func (e *Engine) noteOutcome(ctx context.Context, act activity.Event, err error) {
	act.Outcome = activity.OK
	if err != nil {
		act.Outcome = activity.Failed
		var ve *ValidationError
		if errors.As(err, &ve) {
			act.Outcome = activity.Refused
			act.Problems = len(ve.Problems)
			act.Paths = problemPaths(ve.Problems)
		} else if errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
			act.Outcome = activity.Refused
		}
	}
	e.note(ctx, act)
}

// problemPaths are the fields problems name, with list positions as "-"
// (keys in braces stay: they are ids, never values).
func problemPaths(ps []Problem) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		segs := strings.Split(p.Path, "/")
		for i, s := range segs {
			if s != "" && strings.Trim(s, "0123456789") == "" {
				segs[i] = "-"
			}
		}
		out = append(out, strings.Join(segs, "/"))
	}
	return out
}
