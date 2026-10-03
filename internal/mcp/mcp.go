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
	"sort"
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
	AnnounceAgent(ctx context.Context, docID, actor, name, focus string, agent map[string]any)
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

const instructions = `Cartograph is the organisation's record of what it has decided to do: its
purpose, goals, objectives and outcomes, the gaps they close, the
projects, programmes and operations that deliver them, and the KPIs and
data that measure them. What you help define must fit that record and the
discipline it follows, exactly as a person working in the editor would
have to: the same checks, the same links, the same words.

You act for the person who connected you, with their access and no more.
You may read, validate, check and draft; people on the same manifest see
your drafts live. You may not make the record: saving a version,
recording a reading and moving a project are proposals your person
accepts or declines in Cartograph, after reading them.

Work this way, every time:
1. Name what the person wants in Cartograph's terms: which kind, and for
   a Goal which level (goal, objective, outcome). Ask if unsure.
2. Call guide for that kind and level before drafting anything. It gives
   the definition, every step and field in order, what each field must
   say with right and wrong examples, the links to make, the template to
   fill in, and the organisation's existing records. Use its words; they
   are the discipline's. If an existing record already says what the
   person wants, work on that one instead of defining another.
3. Follow the guide's plan, in its order, before the kind's own steps:
   it is what the new thing answers to, deepest first. Asked for an
   objective, ask first which gaps it must close (offer the plan's
   existing gaps by name); for each, how it is measured (a KPI) and where
   it was found (segments); then the outcome closing each; then the
   objective. When nothing existing fits, define it with the person, with
   guide for its kind, then come back. Never leave a link the plan names
   unmade without asking.
4. Start the draft at once. As soon as you know the kind and a working
   name, create it with save_draft (its id, name and what little you
   know), before asking anything else, so your person sees it in
   Cartograph and can work on it with you. Every manifest you will
   define for this piece of work gets its draft the same way, as you
   come to it.
5. Then work field by field with edit_draft, one step of the guide at a
   time, saving each answer as soon as your person gives it. Your person
   and their colleagues may edit the same draft in Cartograph while you
   work: edit_draft changes only the fields you name and returns the
   draft as it now stands, their changes included. Read it every time,
   build on what they wrote, and never put back a value they changed;
   if you disagree, say so and ask. Use save_draft again only to create.
6. Ask the person for what only they know (owners, figures, dates,
   sources), a step at a time, with examples drawn from their own
   records. Never invent a figure, a date, a source or an owner. Fix
   every open check edit_draft returns; checks reads the draft at any
   time.
7. Propose only when every check is met: propose_save for one manifest,
   propose_set for several that reference each other (a KPI, the gap it
   measures and the outcome that closes it), which your person accepts
   whole. An open check refuses the proposal. Leave one open only when
   your person cannot settle it now, naming it with the reason in
   openChecks; your person reads each reason.
8. Your work ends in a proposal, never in a chat message asking the
   person to accept: they accept in Cartograph, after reading it. Tell
   them what you proposed, and what you left open and why.
9. Some judgement no check can make: whether an outcome describes a state
   rather than an action, whether an aim says one thing, whether a
   statement is specific. That is yours. Hold every statement to the
   guide's examples before proposing, and tell your person where you are
   unsure.

Fields are named by JSON pointer (/spec/keyResults/0/target).`

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
		} else if req.Extra != nil {
			// Served without sessions, a client names itself only in its
			// first request; its User-Agent comes with every one.
			if name := ProductName(req.Extra.Header.Get("User-Agent")); name != "" {
				who.Agent = name
			}
		}
	}
	// A client running several agents (a main one and its sub-agents) may
	// name the one calling in the request's _meta, so its person can tell
	// their work apart; it is recorded beside its parent.
	if req.Params != nil {
		if sub, ok := req.Params.GetMeta()[SubagentMeta].(string); ok {
			if sub = strings.TrimSpace(sub); sub != "" && len(sub) <= 64 {
				who.Agent += " › " + sub
			}
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

// SubagentMeta is the _meta key a client names a sub-agent by.
const SubagentMeta = "cartograph/subagent"

// step is one thing an agent did, as the presence schema's agent has it
// (docs/adr/0018): what, on which manifest, and how it left it.
type step struct {
	Step, Kind, ID string
	Text           []byte   // the manifest, for its name
	Fields         []string // what a draft changed
	Checks         []engine.Check
	Proposal       string
	Parts          int
}

// announce shows what the agent did: on the manifest's shared draft, so
// the people on it see where it works, and on the presence document, so
// the person it acts for can follow it from anywhere. Without shared
// drafts nobody watches live, and nothing is sent.
func (c call) announce(st step) {
	sh := c.o.Engine.Shared()
	if c.o.Presence == nil || sh == nil {
		return
	}
	name := c.who.Name
	if name == "" {
		name = c.who.Subject
	}
	if name == "" {
		name = "The operator"
	}
	label := name + "'s agent (" + c.who.Agent + ")"
	// The step's number is its time in microseconds, so steps from any
	// replica order themselves; a float, so a browser reads a number.
	agent := map[string]any{"for": personFor(c.who), "seq": float64(time.Now().UnixMicro()), "step": st.Step}
	if st.Kind != "" {
		agent["kind"] = st.Kind
	}
	if st.ID != "" {
		agent["id"] = st.ID
	}
	if st.Text != nil {
		var doc struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}
		if b, err := c.o.Engine.Codec().Decode(st.Text); err == nil {
			if raw, err := json.Marshal(b); err == nil && json.Unmarshal(raw, &doc) == nil && doc.Metadata.Name != "" {
				agent["name"] = doc.Metadata.Name
			}
		}
	}
	if len(st.Fields) > 0 {
		agent["fields"] = st.Fields
	}
	if st.Checks != nil {
		met, open := 0, 0
		for _, ch := range st.Checks {
			if ch.Open() {
				open++
			} else {
				met++
			}
		}
		agent["met"], agent["open"] = met, open
	}
	if st.Proposal != "" {
		agent["proposal"], agent["parts"] = st.Proposal, max(st.Parts, 1)
	}
	focus := ""
	if len(st.Fields) > 0 {
		focus = st.Fields[0]
	}
	if st.Kind != "" && st.ID != "" {
		if docID, err := sh.DocumentFor(c.ctx, st.Kind, st.ID); err == nil {
			c.o.Presence.AnnounceAgent(c.ctx, docID, c.actor(), label, focus, agent)
		}
	}
	// The person's own feed, not the presence document everyone joins:
	// what an agent works on is its person's to see (docs/adr/0018).
	if docID, err := sh.AgentFeed(c.ctx, personFor(c.who)); err == nil {
		c.o.Presence.AnnounceAgent(c.ctx, docID, c.actor(), label, "", agent)
	}
}

// personFor is the person an agent acts for, as proposals name them.
func personFor(p identity.Principal) string {
	if p.Anonymous {
		return ""
	}
	if p.Email != "" {
		return strings.ToLower(p.Email)
	}
	return p.Subject
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
	var open *engine.OpenChecksError
	if errors.As(err, &open) {
		var b strings.Builder
		fmt.Fprintf(&b, "Not proposed: %d check%s still open, as a person would see them in the editor:", len(open.Open), map[bool]string{true: "", false: "s"}[len(open.Open) == 1])
		for _, c := range open.Open {
			fmt.Fprintf(&b, "\n- %s/%s %s (%s, section %s): %s", c.Kind, c.ManifestID, c.ID, c.State, orNone(c.Section), c.Message)
		}
		b.WriteString("\nMeet each one: ask your person for what only they know (an owner, a figure, a date), never invent it, " +
			"save the draft, and call checks until none is open. Only a check you cannot meet without them may be left, " +
			"by passing openChecks (for propose_save {check id: why}; for propose_set {Kind/id: {check id: why}}); your person sees each reason before deciding.")
		text = b.String()
	}
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: text}}}
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
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
	editIn struct {
		Kind  string         `json:"kind"`
		ID    string         `json:"id"`
		Work  []string       `json:"work,omitempty" jsonschema:"every other manifest you are defining with this one, as Kind/id (the ones you will propose together): their drafts are read and checked with it, and what they still lack is reported in around"`
		Set   map[string]any `json:"set,omitempty" jsonschema:"fields to set, by JSON pointer, such as {\"/spec/objective\": \"...\"}; in a list, a number replaces that item and - appends one"`
		Unset []string       `json:"unset,omitempty" jsonschema:"fields or list items to remove, by JSON pointer"`
	}
	manifestIn struct {
		Kind     string         `json:"kind"`
		ID       string         `json:"id"`
		Manifest map[string]any `json:"manifest" jsonschema:"the whole manifest: apiVersion, kind, metadata and spec"`
		Work     []string       `json:"work,omitempty" jsonschema:"every other manifest you are defining with this one, as Kind/id (the ones you will propose together): their drafts are read and checked with it, and what they still lack is reported in around"`
	}
	proposeSaveIn struct {
		Kind     string         `json:"kind"`
		ID       string         `json:"id"`
		Manifest map[string]any `json:"manifest,omitempty" jsonschema:"the manifest to save; the current draft when left out"`
		Reason   string         `json:"reason" jsonschema:"why, in a sentence the person will read"`
		// Checks the agent could not meet, each with why; the person sees
		// them on the proposal.
		OpenChecks map[string]string `json:"openChecks,omitempty" jsonschema:"only for a check you cannot meet without your person: check id to why it is left open; every other open check refuses the proposal"`
	}
	proposeSetIn struct {
		Manifests []setMemberIn `json:"manifests" jsonschema:"every manifest that stands or falls together, in any order; each is saved after those it references"`
		Reason    string        `json:"reason" jsonschema:"why, in a sentence the person will read"`
		// Checks the agent could not meet, by Kind/id, then check id.
		OpenChecks map[string]map[string]string `json:"openChecks,omitempty" jsonschema:"only for checks you cannot meet without your person: Kind/id to (check id to why); every other open check refuses the set"`
	}
	setMemberIn struct {
		Kind     string         `json:"kind"`
		ID       string         `json:"id"`
		Manifest map[string]any `json:"manifest,omitempty" jsonschema:"the manifest; its current draft when left out"`
	}
	checksIn struct {
		Kind     string         `json:"kind"`
		ID       string         `json:"id"`
		Manifest map[string]any `json:"manifest,omitempty" jsonschema:"a manifest to check without saving it; its draft or latest version when left out"`
		Work     []string       `json:"work,omitempty" jsonschema:"every other manifest you are defining with this one, as Kind/id (the ones you will propose together): their drafts are read and checked with it, and what they still lack is reported in around"`
	}
	guideIn struct {
		Kind   string `json:"kind"`
		Level  string `json:"level,omitempty" jsonschema:"for a Goal: goal, objective or outcome"`
		Locale string `json:"locale,omitempty" jsonschema:"the language of the words, such as en; English when there are none in it"`
		Step   string `json:"step,omitempty" jsonschema:"one step's key, to read a long guide a step at a time"`
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
			c.announce(step{Step: "read", Kind: in.Kind, ID: in.ID, Text: v.YAML})
			return map[string]any{"version": v.Number, "yaml": string(v.YAML)}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "schema", Description: "The JSON Schema of a kind: every field, its type, allowed values and what it refers to.", Annotations: readOnly},
		func(c call, in kindOnly) (any, error) { return e.Schema(in.Kind) })

	tool(s, o, person, &sdk.Tool{Name: "guide", Description: "Read this before drafting any manifest. How to define a kind well, for one level: what it is, every step and field in order, " +
		"what each field must say with right and wrong examples, the checks each answers and how to meet them, the links to make on other kinds, " +
		"and the organisation's existing records to reuse for each reference and link.", Annotations: readOnly},
		func(c call, in guideIn) (any, error) {
			g, err := e.Guide(c.ctx, in.Kind, in.Level, in.Locale)
			if err != nil {
				return nil, err
			}
			c.announce(step{Step: "guide", Kind: in.Kind})
			if in.Step == "" {
				return guided{Guide: g, Next: nextSteps}, nil
			}
			// One step only, for a long flow such as a project's; the
			// step keys come from a guide without step.
			for _, st := range g.Steps {
				if st.Key == in.Step {
					g.Steps = []engine.GuideStep{st}
					return guided{Guide: g, Next: nextSteps}, nil
				}
			}
			return nil, fmt.Errorf("%s has no step %q", in.Kind, in.Step)
		})

	tool(s, o, person, &sdk.Tool{Name: "flow", Description: "The steps of a kind's editor, in order, and the fields in each, as the contract states them. guide gives the same with the words, examples and records to use.", Annotations: readOnly},
		func(c call, in kindOnly) (any, error) {
			b, ok, err := e.FlowJSON(in.Kind)
			if err != nil || !ok {
				return nil, fmt.Errorf("no flow for %s", in.Kind)
			}
			var flow any
			return flow, json.Unmarshal(b, &flow)
		})

	tool(s, o, person, &sdk.Tool{Name: "checks", Description: "Every quality check a person sees in the editor, on the manifest as it stands (its draft, where there is one): " +
		"what is met (ok), what is still open (warn, or block for what stops a project's handoff), and the section where each is fixed. " +
		"Run it after every save_draft; propose only when nothing is open.", Annotations: readOnly},
		func(c call, in checksIn) (any, error) {
			if in.Manifest != nil {
				text, err := e.Codec().Encode(in.Manifest)
				if err != nil {
					return nil, err
				}
				checks, err := e.ChecksOf(c.ctx, in.Kind, in.ID, text)
				if err != nil {
					return nil, err
				}
				return checkReport(checks), nil
			}
			checks, err := e.DraftChecks(c.ctx, in.Kind, in.ID)
			if errors.Is(err, engine.ErrNotFound) {
				return nil, fmt.Errorf("%s/%s has no draft or version yet: save_draft it, or pass the manifest to check", in.Kind, in.ID)
			}
			if err != nil {
				return nil, err
			}
			c.announce(step{Step: "checks", Kind: in.Kind, ID: in.ID, Checks: checks})
			return withAround(c, checkReport(checks), in.Kind, in.ID, in.Work), nil
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
			before, _, _ := e.GetWorking(c.ctx, in.Kind, in.ID)
			if before == nil {
				if v, err := e.Get(c.ctx, in.Kind, in.ID); err == nil {
					before = v.YAML
				}
			}
			if err := e.SaveWorking(c.ctx, in.Kind, in.ID, text, c.actor()); err != nil {
				return nil, err
			}
			problems, _ := e.Validate(c.ctx, in.Kind, text)
			checks, err := e.ChecksOf(c.ctx, in.Kind, in.ID, text)
			if err != nil {
				return nil, err
			}
			c.announce(step{Step: "draft", Kind: in.Kind, ID: in.ID, Text: text, Fields: e.ChangedFields(before, text, 32), Checks: checks})
			out := withAround(c, checkReport(checks), in.Kind, in.ID, in.Work)
			out["saved"], out["problems"] = "draft", problems
			return out, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "edit_draft", Description: "Set or clear single fields of a manifest's draft, by JSON pointer, leaving every other field as it stands, " +
		"so changes people make in Cartograph at the same time are kept. Returns the draft as it now stands, their changes included, and its checks. " +
		"Starts the draft when the manifest has none. Use it for every change after save_draft creates a manifest.", Annotations: drafting},
		func(c call, in editIn) (any, error) {
			if len(in.Set) == 0 && len(in.Unset) == 0 {
				return nil, fmt.Errorf("name at least one field to set or unset")
			}
			text, err := e.EditDraft(c.ctx, in.Kind, in.ID, in.Set, in.Unset, c.actor())
			if err != nil {
				return nil, err
			}
			problems, _ := e.Validate(c.ctx, in.Kind, text)
			checks, err := e.ChecksOf(c.ctx, in.Kind, in.ID, text)
			if err != nil {
				return nil, err
			}
			fields := make([]string, 0, len(in.Set)+len(in.Unset))
			for p := range in.Set {
				fields = append(fields, p)
			}
			fields = append(fields, in.Unset...)
			sort.Strings(fields)
			c.announce(step{Step: "draft", Kind: in.Kind, ID: in.ID, Text: text, Fields: fields, Checks: checks})
			out := withAround(c, checkReport(checks), in.Kind, in.ID, in.Work)
			// The whole draft, as everyone now has it: what the person
			// changed since the agent last read it is in here to build on.
			out["saved"], out["problems"], out["draft"] = "draft", problems, string(text)
			return out, nil
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
			p, err := e.ProposeSave(c.ctx, in.Kind, in.ID, text, in.Reason, in.OpenChecks)
			if err == nil {
				c.announce(step{Step: "propose", Kind: in.Kind, ID: in.ID, Text: text, Proposal: p.ID, Parts: 1})
			}
			return proposed(p), err
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_set", Description: "Propose several manifests that reference each other, such as a KPI, the gap it measures and the outcome that closes it, " +
		"as one proposal your person accepts or declines whole. Each is checked against the others and against every check its kind has; " +
		"they are saved together, in the order their references need.", Annotations: proposal},
		func(c call, in proposeSetIn) (any, error) {
			members := make([]engine.SetMember, 0, len(in.Manifests))
			for _, m := range in.Manifests {
				var text []byte
				var err error
				if m.Manifest != nil {
					text, err = e.Codec().Encode(m.Manifest)
				} else {
					var found bool
					text, found, err = e.GetWorking(c.ctx, m.Kind, m.ID)
					if err == nil && !found {
						err = fmt.Errorf("%s/%s has no draft to propose: save one, or pass the manifest", m.Kind, m.ID)
					}
				}
				if err != nil {
					return nil, err
				}
				members = append(members, engine.SetMember{Kind: m.Kind, ID: m.ID, Text: text})
			}
			set, err := e.ProposeSet(c.ctx, members, in.Reason, in.OpenChecks)
			if err != nil {
				return nil, err
			}
			last := set[len(set)-1]
			c.announce(step{Step: "propose", Kind: last.Kind, ID: last.ManifestID, Text: last.Text, Proposal: last.ID, Parts: len(set)})
			out := proposed(set[0])
			parts := make([]string, len(set))
			for i, p := range set {
				parts[i] = p.Kind + "/" + p.ManifestID
			}
			out.(map[string]any)["parts"] = parts
			return out, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_item", Description: "Propose recording one item of a series, such as a KPI reading (kind KPIReadings, series readings), for your person to accept.", Annotations: proposal},
		func(c call, in proposeItemIn) (any, error) {
			p, err := e.ProposeAppend(c.ctx, in.Kind, in.ID, in.Series, in.Item, in.Reason)
			if err == nil {
				c.announce(step{Step: "propose", Kind: in.Kind, ID: in.ID, Proposal: p.ID, Parts: 1})
			}
			return proposed(p), err
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_state", Description: "Propose moving a project: defined, handed off or cancelled. A handoff must pass the project's blocking checks.", Annotations: proposal},
		func(c call, in proposeStateIn) (any, error) {
			p, err := e.ProposeState(c.ctx, in.Project, in.To, in.Reason)
			if err == nil {
				c.announce(step{Step: "propose", Kind: "Project", ID: in.Project, Proposal: p.ID, Parts: 1})
			}
			return proposed(p), err
		})

	for _, kind := range e.FlowKinds() {
		kind := kind
		s.AddPrompt(&sdk.Prompt{Name: "guide_" + strings.ToLower(kind), Description: "Define a " + kind + " with the person, step by step, as Cartograph's guide says."},
			func(ctx context.Context, _ *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
				g, err := e.Guide(ctx, kind, "", "")
				if err != nil {
					return nil, err
				}
				b, err := json.MarshalIndent(g, "", " ")
				if err != nil {
					return nil, err
				}
				text := "Help me define a " + kind + " in Cartograph. Work as Cartograph's instructions say: settle the level first where there is one " +
					"(then call guide again for that level), go step by step, make every link, reuse my organisation's records before defining new ones, " +
					"ask me for what only I know with examples, save drafts and meet every check, then propose. The guide:\n\n" + string(b)
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

// checkReport is checks as an agent acts on them: what is still open
// first, then what is met, and what to do next.
func checkReport(checks []engine.Check) map[string]any {
	open, met := []engine.Check{}, []string{}
	for _, c := range checks {
		if c.Open() {
			open = append(open, c)
		} else {
			met = append(met, c.ID)
		}
	}
	next := "Every check is met. Propose it when your person is ready."
	if len(open) > 0 {
		next = "Not ready to propose. Meet each open check: ask your person for what only they know, never invent it, then save the draft again."
	}
	return map[string]any{"open": open, "met": met, "next": next}
}

// withAround adds the work around a manifest to a report: every manifest
// joined to it along the links the work needs, with what each still
// lacks, so an agent fixing an outcome hears that the gap it closes has
// no indicator yet.
func withAround(c call, out map[string]any, kind, id string, work []string) map[string]any {
	var also []engine.Ref
	for _, w := range work {
		if k, i, ok := strings.Cut(w, "/"); ok && k != "" && i != "" {
			also = append(also, engine.Ref{Kind: k, ID: i})
		}
	}
	around, err := c.o.Engine.WorkAround(c.ctx, kind, id, also)
	if err != nil || len(around) == 0 {
		return out
	}
	unfinished := 0
	for _, a := range around {
		if len(a.Open) > 0 {
			unfinished++
		}
	}
	out["around"] = around
	if unfinished > 0 {
		out["aroundNext"] = fmt.Sprintf("%d manifest%s joined to this one still lack%s something (around, open). "+
			"The work is not finished until they are: settle each with your person, then propose them together with propose_set.",
			unfinished, map[bool]string{true: "", false: "s"}[unfinished == 1], map[bool]string{true: "s", false: ""}[unfinished == 1])
	}
	return out
}

// guided is a guide as an agent reads it: the guide, then what to do with
// it, where a small model reads it last and remembers it best.
type guided struct {
	engine.Guide
	Next []string `json:"next"`
}

// nextSteps is the method, named by tool, at the end of every guide.
var nextSteps = []string{
	"If an existing record already says what your person wants, work on it (get it, then edit_draft your changes) instead of defining another.",
	"Start with plan, in its order, before this kind's own steps: for each item, ask its question with the existing records offered by name; " +
		"when none fits, define one with guide for its kind and its own plan. For an objective that means the gaps first, each measured by a KPI and observed in segments, then the outcome closing each.",
	"Settle each step with your person, in order, using the field guides and their examples; never invent a figure, date, source or owner.",
	"Create each draft with save_draft as soon as you know its name, then edit_draft field by field; pass work (every Kind/id you are defining together) and meet every open check it returns, its own and around.",
	"Then propose: propose_save for one manifest, propose_set for several that reference each other. Your work is not done until it is proposed; your person accepts it in Cartograph, not in this conversation.",
}

// ProductName is a User-Agent's first product as a person reads it:
// claude-code/2.1 is Claude Code. Empty for a browser or a library.
func ProductName(ua string) string {
	product, _, _ := strings.Cut(strings.TrimSpace(ua), "/")
	product, _, _ = strings.Cut(product, " ")
	switch strings.ToLower(product) {
	case "", "mozilla", "go-http-client", "curl", "python-requests", "node", "undici", "axios", "node-fetch":
		return ""
	}
	words := strings.FieldsFunc(product, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
