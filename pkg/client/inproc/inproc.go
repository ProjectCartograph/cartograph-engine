// Package inproc is the client.Client over an engine in the same
// process: no wire, no serialisation beyond the codec. It is what a
// terminal interface uses when it runs in the cartograph binary itself, and
// what the conformance suite uses to prove the scenarios against the
// engine directly.
package inproc

//lint:file-ignore SA1019 the merge-based shared draft is deprecated in 1.1.0 and removed in 2.0.0 (docs/adr/0007); until then this file still carries it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/pkg/client"
	"github.com/ProjectCartograph/cartograph-engine/pkg/merge"
)

// Client wraps an engine.
type Client struct {
	E *engine.Engine
	// Operator is recorded as the actor when the context carries no
	// principal: the vault's settings say what it is.
	Operator string
}

var _ client.Client = (*Client)(nil)

// New returns a client over e.
func New(e *engine.Engine) *Client { return &Client{E: e} }

func (c *Client) Actor(ctx context.Context) (string, error) {
	fallback := c.Operator
	if fallback == "" {
		s, err := c.E.GetSettings(ctx)
		if err != nil {
			return "", err
		}
		fallback = s.Operator
	}
	return auth.PrincipalFrom(ctx).Actor(fallback), nil
}

func (c *Client) Kinds(context.Context) ([]string, error) {
	infos := c.E.Kinds()
	out := make([]string, len(infos))
	for i, k := range infos {
		out[i] = k.Kind
	}
	return out, nil
}

func (c *Client) Schema(_ context.Context, kind string) ([]byte, error) { return c.E.SchemaJSON(kind) }

func (c *Client) Flow(_ context.Context, kind string) (client.Flow, bool, error) {
	b, found, err := c.E.FlowJSON(kind)
	if err != nil || !found {
		return nil, false, err
	}
	var f client.Flow
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, false, fmt.Errorf("flow %s: %w", kind, err)
	}
	return f, true, nil
}

func (c *Client) Settings(ctx context.Context) (client.Settings, error) {
	s, err := c.E.GetSettings(ctx)
	if err != nil {
		return client.Settings{}, err
	}
	return client.Settings{GoalLevels: s.GoalLevels, ProjectLevelName: s.ProjectLevelName, Operator: s.Operator, Examples: s.Examples}, nil
}

func (c *Client) List(ctx context.Context, kind, query string) ([]client.Summary, error) {
	rows, err := c.E.List(ctx, kind, engine.Filter{Q: query}, false)
	if err != nil {
		return nil, err
	}
	out := make([]client.Summary, len(rows))
	for i, r := range rows {
		out[i] = client.Summary{Kind: r.Kind, ID: r.ID, Name: r.Name, Version: r.Version, UpdatedOn: r.UpdatedOn, Labels: r.Labels}
	}
	return out, nil
}

func (c *Client) Get(ctx context.Context, kind, id string) (client.Manifest, error) {
	v, err := c.E.Get(ctx, kind, id)
	if errors.Is(err, engine.ErrNotFound) {
		return client.Manifest{}, client.ErrNotFound
	}
	if err != nil {
		return client.Manifest{}, err
	}
	doc, err := c.E.Codec().Decode(v.YAML)
	if err != nil {
		return client.Manifest{}, err
	}
	return client.Manifest{Kind: kind, ID: id, Version: v.Number, Doc: doc, Text: v.YAML}, nil
}

func (c *Client) Working(ctx context.Context, kind, id string) (client.Manifest, error) {
	text, found, err := c.E.GetWorking(ctx, kind, id)
	if err != nil {
		return client.Manifest{}, err
	}
	if !found {
		return client.Manifest{}, client.ErrNotFound
	}
	doc, err := c.E.Codec().Decode(text)
	if err != nil {
		return client.Manifest{}, err
	}
	return client.Manifest{Kind: kind, ID: id, Doc: doc, Text: text}, nil
}

func (c *Client) SaveWorking(ctx context.Context, kind, id string, doc map[string]any) error {
	text, err := c.E.Codec().Encode(doc)
	if err != nil {
		return err
	}
	return c.E.PutWorking(ctx, kind, id, text)
}

func (c *Client) SaveVersion(ctx context.Context, kind, id string, doc map[string]any, reason string) (client.Version, error) {
	text, err := c.E.Codec().Encode(doc)
	if err != nil {
		return client.Version{}, err
	}
	actor, err := c.Actor(ctx)
	if err != nil {
		return client.Version{}, err
	}
	var v engine.Version
	if kind == "Project" {
		v, err = c.E.CommitProject(ctx, id, text, actor, reason)
	} else {
		v, err = c.E.Commit(ctx, kind, id, text, actor, reason)
	}
	if err != nil {
		var ve *engine.ValidationError
		if errors.As(err, &ve) {
			return client.Version{}, &client.Refused{Problems: problems(ve.Problems)}
		}
		return client.Version{}, err
	}
	return client.Version{Kind: v.Kind, ID: v.ID, Number: v.Number, Actor: v.Actor, Reason: v.Reason, On: v.On}, nil
}

func (c *Client) Validate(ctx context.Context, kind string, doc map[string]any) ([]client.Problem, error) {
	text, err := c.E.Codec().Encode(doc)
	if err != nil {
		return nil, err
	}
	ps, err := c.E.Validate(ctx, kind, text)
	if err != nil {
		return nil, err
	}
	return problems(ps), nil
}

func (c *Client) Checks(ctx context.Context, kind, id string) ([]client.Check, error) {
	var out []client.Check
	if kind == "Project" {
		pc, err := c.E.ProjectChecks(ctx, id, false)
		if err != nil {
			return nil, err
		}
		for _, it := range pc.Items {
			out = append(out, client.Check{Key: it.ID, State: it.State, Path: it.Section, Message: it.Message, Fix: it.Fix.Section})
		}
	}
	if d := c.E.Drafts(); d != nil {
		for _, n := range d.Notes(kind, id) {
			out = append(out, client.Check{Key: "conflict", State: "note", Path: n.Path,
				Message: fmt.Sprintf("%s changed this while it was being edited; the earlier value was %v", n.Winner.Clock.Actor, n.Loser.Value)})
		}
	}
	if out == nil {
		out = []client.Check{}
	}
	return out, nil
}

func (c *Client) Edit(ctx context.Context, kind, id string, ops []merge.Op) (client.EditResult, error) {
	d := c.E.Drafts()
	if d == nil {
		return client.EditResult{}, client.ErrUnsupported
	}
	actor, err := c.Actor(ctx)
	if err != nil {
		return client.EditResult{}, err
	}
	res, err := d.Edit(ctx, kind, id, actor, ops)
	if err != nil {
		return client.EditResult{}, err
	}
	return client.EditResult{Seq: res.Seq, Conflicts: res.Conflicts}, nil
}

func (c *Client) OpsSince(ctx context.Context, kind, id string, after int64) ([]client.Op, error) {
	d := c.E.Drafts()
	if d == nil {
		return nil, client.ErrUnsupported
	}
	ops, err := d.Since(ctx, kind, id, after)
	if err != nil {
		return nil, err
	}
	out := make([]client.Op, len(ops))
	for i, op := range ops {
		out[i] = client.Op{Seq: op.Seq, Op: op.Op}
	}
	return out, nil
}

func (c *Client) Subscribe(ctx context.Context, kind, id string) (<-chan client.Event, error) {
	sub := c.E.Bus().Subscribe(ctx, kind, id)
	out := make(chan client.Event, 64)
	go func() {
		defer close(out)
		for ev := range sub.C {
			e := client.Event{Type: ev.Type, Kind: ev.Kind, ID: ev.ID, On: ev.On, Seq: ev.Seq, Actor: ev.Actor, Field: ev.Field}
			for _, o := range ev.Ops {
				if op, ok := o.Op.(merge.Op); ok {
					e.Ops = append(e.Ops, op)
				}
			}
			out <- e
		}
	}()
	return out, nil
}

func (c *Client) Close() error { return nil }

func problems(ps []engine.Problem) []client.Problem {
	out := make([]client.Problem, len(ps))
	for i, p := range ps {
		out[i] = client.Problem{Path: p.Path, Message: p.Message}
	}
	return out
}
