// Package mcp is the MCP front door (docs/adr/0016): agents read, draft
// and propose through the same engine as people, with their person's
// authority and no more. Like the HTTP API and the sync socket, it is a
// driving adapter; it decides nothing the engine does not.
//
// It is stateless: every request stands alone (MCP's sessionless
// transport), so it holds nothing open and a deployment still scales to
// zero (docs/adr/0015).
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Presence announces an agent on a document, so the people on it see it
// working. The sync socket implements it; nil announces nothing.
type Presence interface {
	AnnounceAgent(ctx context.Context, docID, actor, name string)
}

// Options are what the MCP server works through.
type Options struct {
	Engine *engine.Engine
	// Authz decides whether a principal may act through an agent at all
	// (auth.ResourceAgent); nil allows it.
	Authz    auth.Authorizer
	Reports  reporting.Reporter
	Presence Presence
	Version  string
}

// Handler serves MCP over HTTP, sessionless. The principal is the one the
// authentication middleware put on the request: the person the agent
// acts for.
func Handler(o Options) http.Handler {
	return sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		return newServer(o, auth.PrincipalFrom(r.Context()))
	}, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

// ServeStdio serves MCP over stdin and stdout, for an agent on the
// operator's own machine: anonymous, as the command line is, and held to
// the same ceiling.
func ServeStdio(ctx context.Context, o Options) error {
	return newServer(o, identity.Anonymous).Run(ctx, &sdk.StdioTransport{})
}

const instructions = `Cartograph captures what an organisation has decided to do: goals,
programmes, projects, operations, KPIs and the data sources behind them.

You act for the person who connected you, with their access and no more.
You may read everything they may, check and validate, and edit drafts
(the people working on the same manifest see your changes and see you).
You may not make the record: saving a version, recording a reading,
moving a project and handing it off are proposals that your person
accepts or declines in Cartograph. Say what you propose and why.

To guide someone through defining something, use the guide prompt for
its kind: it gives the steps and fields in order. Fields are named by
JSON pointer (/spec/summary/problems/0/problem/situation). A check never
blocks a save; problems say which field to fix.`

// call is one tool call's context: the person, as acting through the
// calling agent, after checking they may use one.
type call struct {
	o   Options
	ctx context.Context
	who identity.Principal
}

func (o Options) begin(ctx context.Context, person identity.Principal, req *sdk.CallToolRequest) (call, error) {
	who := person
	// A grant named its agent when its person consented, and that name
	// stands whatever the client calls itself now; otherwise the client
	// names itself.
	if who.Agent == "" {
		who.Agent = "an agent"
		if info := req.ClientInfo(); info != nil && strings.TrimSpace(info.Name) != "" {
			who.Agent = strings.TrimSpace(info.Name)
		}
	}
	if o.Authz != nil {
		if err := o.Authz.Authorize(ctx, who, identity.Action{Verb: identity.VerbRead, Resource: identity.ResourceAgent}); err != nil {
			return call{}, err
		}
	}
	return call{o: o, ctx: identity.WithPrincipal(ctx, who), who: who}, nil
}

// actor is what a draft records the agent's edit as.
func (c call) actor() string {
	operator := "local"
	if s, err := c.o.Engine.GetSettings(c.ctx); err == nil && s.Operator != "" {
		operator = s.Operator
	}
	return c.who.Actor(operator)
}

// announce shows the agent on a manifest's shared draft.
func (c call) announce(kind, id string) {
	// Without shared drafts, nobody watches a draft live to see it.
	sh := c.o.Engine.Shared()
	if c.o.Presence == nil || sh == nil {
		return
	}
	docID, err := sh.DocumentFor(c.ctx, kind, id)
	if err != nil {
		return
	}
	name := c.who.Name
	if name == "" {
		name = c.who.Subject
	}
	if name == "" {
		name = "The operator"
	}
	c.o.Presence.AnnounceAgent(c.ctx, docID, c.actor(), name+"'s agent ("+c.who.Agent+")")
}

// tool registers a tool whose handler runs as the agent.
func tool[In any](s *sdk.Server, o Options, person identity.Principal, t *sdk.Tool, h func(c call, in In) (any, error)) {
	sdk.AddTool(s, t, func(ctx context.Context, req *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
		c, err := o.begin(ctx, person, req)
		if err != nil {
			return failed(err), nil, nil
		}
		out, err := h(c, in)
		if err != nil {
			return failed(err), nil, nil
		}
		return nil, out, nil
	})
}

// failed is a tool error the agent reads: problems by field, or why it
// was refused.
func failed(err error) *sdk.CallToolResult {
	var invalid *engine.ValidationError
	text := err.Error()
	if errors.As(err, &invalid) {
		var b strings.Builder
		b.WriteString("Not valid:")
		for _, p := range invalid.Problems {
			fmt.Fprintf(&b, "\n- %s: %s", orRoot(p.Path), p.Message)
		}
		text = b.String()
	}
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: text}}}
}

func orRoot(path string) string {
	if path == "" {
		return "(the manifest)"
	}
	return path
}

var (
	readOnly = &sdk.ToolAnnotations{ReadOnlyHint: true}
	drafting = &sdk.ToolAnnotations{IdempotentHint: true, DestructiveHint: ptr(false)}
	proposal = &sdk.ToolAnnotations{DestructiveHint: ptr(false)}
)

func ptr[T any](v T) *T { return &v }

// Inputs.
type (
	none     struct{}
	kindOnly struct {
		Kind string `json:"kind" jsonschema:"a kind, such as Goal or Project"`
	}
	manifestRef struct {
		Kind string `json:"kind" jsonschema:"the manifest's kind"`
		ID   string `json:"id" jsonschema:"the manifest's id"`
	}
	searchIn struct {
		Kind  string   `json:"kind" jsonschema:"the kind to list"`
		Query string   `json:"query,omitempty" jsonschema:"words in the id or name"`
		Refs  []string `json:"refs,omitempty" jsonschema:"Kind/id that every result must reference"`
		After string   `json:"after,omitempty" jsonschema:"the last id of the previous page"`
		Limit int      `json:"limit,omitempty" jsonschema:"at most this many, 50 when left out"`
	}
	diffIn struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
		From int    `json:"from" jsonschema:"the earlier version"`
		To   int    `json:"to" jsonschema:"the later version"`
	}
	manifestIn struct {
		Kind     string         `json:"kind"`
		ID       string         `json:"id"`
		Manifest map[string]any `json:"manifest" jsonschema:"the whole manifest: apiVersion, kind, metadata and spec"`
	}
	proposeSaveIn struct {
		Kind     string         `json:"kind"`
		ID       string         `json:"id"`
		Manifest map[string]any `json:"manifest,omitempty" jsonschema:"the manifest to save; the current draft when left out"`
		Reason   string         `json:"reason" jsonschema:"why, in a sentence the person will read"`
	}
	proposeItemIn struct {
		Kind   string         `json:"kind" jsonschema:"KPIReadings for a reading"`
		ID     string         `json:"id"`
		Series string         `json:"series" jsonschema:"the series under spec, readings for a reading"`
		Item   map[string]any `json:"item" jsonschema:"the item: for a reading, period (YYYY-MM) and value, and provisional and note when they apply"`
		Reason string         `json:"reason"`
	}
	proposeStateIn struct {
		Project string `json:"project" jsonschema:"the project's id"`
		To      string `json:"to" jsonschema:"defined, handed off or cancelled"`
		Reason  string `json:"reason" jsonschema:"why; a cancellation needs one"`
	}
	reportIn struct {
		Name string `json:"name" jsonschema:"projects, kpi-readings, alignment or teams"`
	}
	eventsIn struct {
		After int64 `json:"after,omitempty" jsonschema:"the seq of the last event read; 0 for the start"`
		Limit int   `json:"limit,omitempty"`
	}
)

func newServer(o Options, person identity.Principal) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "cartograph", Version: o.Version}, &sdk.ServerOptions{Instructions: instructions})
	e := o.Engine

	tool(s, o, person, &sdk.Tool{Name: "kinds", Description: "The kinds of thing Cartograph keeps, and how many of each.", Annotations: readOnly},
		func(c call, _ none) (any, error) { return e.Kinds(), nil })

	tool(s, o, person, &sdk.Tool{Name: "search", Description: "List manifests of a kind, by name or id, and by what they reference, a page at a time.", Annotations: readOnly},
		func(c call, in searchIn) (any, error) {
			f := engine.Filter{Q: in.Query}
			for _, r := range in.Refs {
				kind, id, ok := strings.Cut(r, "/")
				if !ok {
					return nil, fmt.Errorf("ref %q: want Kind/id", r)
				}
				f.Refs = append(f.Refs, engine.Ref{Kind: kind, ID: id})
			}
			limit := in.Limit
			if limit <= 0 || limit > 200 {
				limit = 50
			}
			page, more, err := e.ListPage(c.ctx, in.Kind, f, in.After, limit)
			return map[string]any{"items": page, "more": more}, err
		})

	tool(s, o, person, &sdk.Tool{Name: "get", Description: "One manifest as it stands now (its draft, where there is one), as YAML.", Annotations: readOnly},
		func(c call, in manifestRef) (any, error) {
			v, err := e.Get(c.ctx, in.Kind, in.ID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"version": v.Number, "yaml": string(v.YAML)}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "schema", Description: "The JSON Schema of a kind: every field, its type, allowed values and what it refers to.", Annotations: readOnly},
		func(c call, in kindOnly) (any, error) { return e.Schema(in.Kind) })

	tool(s, o, person, &sdk.Tool{Name: "flow", Description: "The steps of a kind's editor, in order, and the fields in each: the order to guide a person in.", Annotations: readOnly},
		func(c call, in kindOnly) (any, error) {
			b, ok, err := e.FlowJSON(in.Kind)
			if err != nil || !ok {
				return nil, fmt.Errorf("no flow for %s", in.Kind)
			}
			var flow any
			return flow, json.Unmarshal(b, &flow)
		})

	tool(s, o, person, &sdk.Tool{Name: "checks", Description: "What a goal or a project still needs, by section: advice, never a block on saving.", Annotations: readOnly},
		func(c call, in manifestRef) (any, error) {
			switch in.Kind {
			case "Goal":
				return e.GoalChecks(c.ctx, in.ID)
			case "Project":
				return e.ProjectChecks(c.ctx, in.ID, true)
			}
			return nil, fmt.Errorf("checks are for goals and projects, not %s", in.Kind)
		})

	tool(s, o, person, &sdk.Tool{Name: "goal_tree", Description: "Every goal, objective and outcome as a tree, with what is aligned to each.", Annotations: readOnly},
		func(c call, _ none) (any, error) { return e.GoalTree(c.ctx) })

	tool(s, o, person, &sdk.Tool{Name: "references", Description: "What a manifest references, and what references it.", Annotations: readOnly},
		func(c call, in manifestRef) (any, error) { return e.References(c.ctx, in.Kind, in.ID) })

	tool(s, o, person, &sdk.Tool{Name: "history", Description: "Every saved version of a manifest: who saved it, when and why.", Annotations: readOnly},
		func(c call, in manifestRef) (any, error) {
			vs, err := e.Versions(c.ctx, in.Kind, in.ID)
			out := make([]map[string]any, len(vs))
			for i, v := range vs {
				out[i] = map[string]any{"number": v.Number, "actor": v.Actor, "reason": v.Reason, "on": v.On}
			}
			return out, err
		})

	tool(s, o, person, &sdk.Tool{Name: "diff", Description: "What changed between two versions of a manifest, field by field.", Annotations: readOnly},
		func(c call, in diffIn) (any, error) { return e.Diff(c.ctx, in.Kind, in.ID, in.From, in.To) })

	tool(s, o, person, &sdk.Tool{Name: "validate", Description: "Check a manifest against its schema and rules without saving anything.", Annotations: readOnly},
		func(c call, in manifestIn) (any, error) {
			text, err := e.Codec().Encode(in.Manifest)
			if err != nil {
				return nil, err
			}
			problems, err := e.Validate(c.ctx, in.Kind, text)
			return map[string]any{"problems": problems}, err
		})

	tool(s, o, person, &sdk.Tool{Name: "events", Description: "What happened since a cursor: versions saved, readings recorded, states changed, proposals made and decided.", Annotations: readOnly},
		func(c call, in eventsIn) (any, error) {
			limit := in.Limit
			if limit <= 0 {
				limit = 100
			}
			return e.Events(c.ctx, in.After, limit)
		})

	tool(s, o, person, &sdk.Tool{Name: "my_proposals", Description: "The proposals waiting for your person to decide.", Annotations: readOnly},
		func(c call, _ none) (any, error) {
			return e.Proposals(c.ctx, store.ProposalFilter{Status: store.ProposalOpen})
		})

	if o.Reports != nil {
		tool(s, o, person, &sdk.Tool{Name: "report", Description: "A report over the record: projects, kpi-readings, alignment or teams.", Annotations: readOnly},
			func(c call, in reportIn) (any, error) {
				t, err := o.Reports.Run(c.ctx, in.Name)
				return map[string]any{"columns": t.Columns, "rows": t.Rows}, err
			})
	}

	tool(s, o, person, &sdk.Tool{Name: "save_draft", Description: "Save a manifest as its draft, never as a version. People on the same manifest see the change, and see you, at once. A draft may be incomplete.", Annotations: drafting},
		func(c call, in manifestIn) (any, error) {
			text, err := e.Codec().Encode(in.Manifest)
			if err != nil {
				return nil, err
			}
			if err := e.SaveWorking(c.ctx, in.Kind, in.ID, text, c.actor()); err != nil {
				return nil, err
			}
			c.announce(in.Kind, in.ID)
			problems, _ := e.Validate(c.ctx, in.Kind, text)
			return map[string]any{"saved": "draft", "problems": problems}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_save", Description: "Propose saving a manifest as its next version, for your person to accept or decline in Cartograph. Checked now as a save would be.", Annotations: proposal},
		func(c call, in proposeSaveIn) (any, error) {
			var text []byte
			var err error
			if in.Manifest != nil {
				text, err = e.Codec().Encode(in.Manifest)
			} else {
				var found bool
				text, found, err = e.GetWorking(c.ctx, in.Kind, in.ID)
				if err == nil && !found {
					err = fmt.Errorf("%s/%s has no draft to propose: save one, or pass the manifest", in.Kind, in.ID)
				}
			}
			if err != nil {
				return nil, err
			}
			p, err := e.ProposeSave(c.ctx, in.Kind, in.ID, text, in.Reason)
			c.announce(in.Kind, in.ID)
			return proposed(p), err
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_item", Description: "Propose recording one item of a series, such as a KPI reading (kind KPIReadings, series readings), for your person to accept.", Annotations: proposal},
		func(c call, in proposeItemIn) (any, error) {
			p, err := e.ProposeAppend(c.ctx, in.Kind, in.ID, in.Series, in.Item, in.Reason)
			return proposed(p), err
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_state", Description: "Propose moving a project: defined, handed off or cancelled. A handoff must pass the project's blocking checks.", Annotations: proposal},
		func(c call, in proposeStateIn) (any, error) {
			p, err := e.ProposeState(c.ctx, in.Project, in.To, in.Reason)
			return proposed(p), err
		})

	for _, kind := range e.FlowKinds() {
		kind := kind
		s.AddPrompt(&sdk.Prompt{Name: "guide_" + strings.ToLower(kind), Description: "Guide a person through defining a " + kind + ", step by step."},
			func(ctx context.Context, _ *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
				b, _, err := e.FlowJSON(kind)
				if err != nil {
					return nil, err
				}
				text := "Guide the person through defining a " + kind + ", one step at a time, in this order. " +
					"Ask for what each field needs, save their answers as a draft with save_draft as you go, run checks, " +
					"and when they are happy, propose_save with a reason. The steps and their fields:\n\n" + string(b)
				return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{{Role: "user", Content: &sdk.TextContent{Text: text}}}}, nil
			})
	}
	return s
}

// proposed is what an agent is told after proposing.
func proposed(p store.Proposal) any {
	if p.ID == "" {
		return nil
	}
	who := p.For
	if who == "" {
		who = "the operator"
	}
	return map[string]any{
		"proposal": p.ID, "status": p.Status, "at": p.At.Format(time.RFC3339),
		"next": "Proposed. " + who + " accepts or declines it in Cartograph, under Proposals; nothing is saved until then.",
	}
}
