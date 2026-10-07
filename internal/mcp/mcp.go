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
You may read, validate, check and draft; you may not make the record.
Everything you draft goes into your change set: your own drafts, apart
from the record and from every other agent's work, as a branch is. Your
person sees it as you work, may open it and work in it with you, and
accepts it whole in Cartograph after reviewing every change in it.
Your person may also ask you to work in one of theirs: to refine a
piece of work they started, or to meet its checks before they merge it.
Find it with change_sets, pass its changeSet to every tool you call for
it, and start from checks on the whole set. Their drafts are live: they
may be editing beside you, so change only what the work needs, with
edit_draft, and read a draft again before you change it. It stays
theirs: you never propose it; when nothing is open, tell them, and they
propose and merge it.
Recording a reading and moving a project are proposals of their own.

Work this way, every time:
- First, call decision_model. When it is ready and the workspace (or
  your change set) holds records to choose from, call relevant with what
  the work is about before you choose what a draft names (the outcomes it
  serves, its programme, indicators, groups, data sources), and offer
  that shortlist first: the decision model is Cartograph's way of finding
  what in the workspace is relevant, and it is quicker and more complete
  than reading every register. It ranks and never decides. When it is
  not ready, relevant still ranks by shared words, so read the register
  more fully before offering choices.
0. Ask your person once, at the start, how they want to work, as a
   choice: with suggestions, or step by step. With suggestions, at every
   step you offer two to four concrete options, drawn from the documents
   they gave you, their existing records (next's choices) and the
   guide's good examples, and they pick one or write their own; it is
   quicker. Step by step, you ask and they answer in their own words.
   When your client has a tool for asking a multiple-choice question
   (Claude Code's AskUserQuestion, for one), ask with it, so your person
   picks rather than types; otherwise number the options. Always leave
   room for their own answer, and never pick for them. When your person
   is not there to ask (you were given documents to port on your own),
   skip this step: work from the documents, and leave_open what only
   they can answer.
1. Cartograph's record is a directed acyclic graph, written from the top
   down in one order: purpose, goals, objectives, outcomes, the KPIs
   that measure them, the gaps they close, then portfolios, programmes,
   operations and projects, and last a portfolio's decisions. Each thing names only what comes before it, so what it
   names already exists when it is written, and nothing finished is
   opened again to link it to something later. When your person does not
   say where to start, or the workspace is new, call next without work:
   it names the stage to write now, from the purpose down. Then name what
   the person wants in Cartograph's terms: which kind, and for a Goal
   which level (goal, objective, outcome). Ask if unsure. Two choices
   decide most work. A service: one running today is an Operation with
   status running, recorded as it stands, whatever else exists; a new
   one is an Operation with status planned, written before the project
   that sets it up, which names it as where it lands. Work that ends:
   a Project has one objective, the change it exists to make,
   measured by one to three key results. Every other change a document
   lists is either a key result of that objective or a Project of its
   own that the first lists among its components, as software names the
   libraries it depends on. A component is work with a change of its own
   (a survey that sets a baseline, a portal or system people will use,
   an app); an output the project hands over (a handbook, materials, a
   toolkit, training) is a deliverable, whoever leads it. A document's
   workstreams are one or the other, never a list inside the project. Projects
   that each need the others to bring about one change, with a theory of
   change linking what they deliver to that change, are a Programme
   (it may hold sub-programmes). Projects and programmes grouped to
   decide what to fund and in what order, against strategic objectives,
   with no causal link needed, are a Portfolio, and its invest, hold or
   stop decisions are a PortfolioDecisions file written after them. A
   grouping that is neither is not recorded as a kind: never make a
   Programme or a Portfolio only because things are grouped. Recording
   what already exists follows the same order as defining something new.
   Documents your person gives you are evidence, not a structure: plans
   use their own words, and the same word means different things in
   different plans. Call taxonomy, and record each thing a document says
   as the Cartograph kind and level it is by definition, whatever the
   document calls it: a plan's "priority" may be a goal, its "objective"
   an outcome, its "target" a KPI's target, its "problem" a gap.
   taxonomy's porting map says, part by part, where each part of a
   charter or plan goes and what stays out; follow it rather than
   judging. Keep the
   document's own wording where the porting map says (an aim's
   statedAs, a gap's statement) and cite it as the source;
   never add a kind, level or field Cartograph does not have, and when a
   thing fits no kind, say so to your person rather than forcing it.
2. Call guide for that kind and level before drafting anything. It gives
   the definition, every step and field in order (a field marked
   required cannot be left out), what each field must
   say with right and wrong examples, the links to make, the template to
   fill in, and the organisation's existing records. Use its words; they
   are the discipline's. If an existing record already says what the
   person wants, work on that one instead of defining another.
3. Follow the guide's plan, in its order (a kind nothing names, such as
   a project, has prepare instead: what must exist before it). Its
   "before" items are what
   the new thing names: each must exist before the new thing is written
   (offer the existing records by name; define one with guide for its
   kind only when none fits). Then define the new thing, whole. Its
   "after" items are what will name it, written next, each after what it
   names in turn: for an objective, its outcomes, the KPI measuring each,
   then the gaps they close. A link is always made from the later thing
   to the earlier one, as that later thing is written; never go back to
   an earlier record to add it. Never leave a link the plan names unmade
   without asking. This order is enforced, not advised: a draft that
   names anything that is neither in the record nor already drafted in
   your change set is refused, and so is a placeholder (metadata.pending,
   which is for people alone). When something the plan needs does not
   exist, define it first, in this change set, with guide for its kind,
   and only then the thing that names it.
4. Start each piece of work with start_work, titled as your person
   would say it, so it is one change set they review on its own. Then
   start the draft at once: as soon as you know the kind and a working
   name, create it with save_draft (its id, name and what little you
   know), before asking anything else, so your person sees it in
   Cartograph and can work on it with you. Every manifest you will
   define for this piece of work gets its draft the same way, in the same
   change set, as you come to it.
5. Then work field by field with edit_draft, in the order next gives,
   saving each answer as soon as your person gives it. Your person
   and their colleagues may edit the same draft in Cartograph while you
   work, in your change set: edit_draft changes only the fields you name
   and returns the draft as it now stands, their changes included. Read it every time,
   build on what they wrote, and never put back a value they changed;
   if you disagree, say so and ask. Use save_draft again only to create.
6. Cartograph sets the order of the work; you do not have to work it
   out. Every save_draft, edit_draft and checks you call with work (every
   Kind/id you are defining together) ends with next: do that, then the
   next. It walks the work once, from the top: each manifest is finished
   in one visit (what it is, then its numbers, then its links to what is
   already there) before the next one down, and a check that something
   later settles by naming it is placed with that later thing. The next
   tool gives the same, with what follows.
7. Take what the documents your person gave you say: figures, dates,
   sources and owners, quoting where each came from. For what they do
   not say, ask your person, one question at a time, as soon as a check
   needs it: say why you ask (what the document says, or that it is
   silent, and which check waits on it), and offer the options you can
   draw from the document and their records, so they can pick. A target
   the document defers is still asked: they may know it, or decide it
   now. Never invent a figure, a date, a source or an owner. Speak to
   your person in their document's words, not Cartograph's: "intake
   graders (the members who grade produce as it reaches a depot)" as
   the document says it, never a shortened "graders" or a kind name
   such as BeneficiaryGroup. Say the full term with what it means
   the first time in each question; name a Cartograph kind only when
   they must choose between kinds, and then say in a few words what it
   is.
8. Propose your change set with propose when every check across it is
   met; your person accepts it whole, after trimming anything not ready.
   An open check refuses the proposal. When your person, asked, cannot
   answer yet (a figure decided later, a score nobody has made), call
   leave_open for it with the reason and what you asked, and carry on: next passes it by, and propose waives
   it with your reason, which your person reads. Never leave one you
   could meet from the documents.
9. Your work ends in a proposal, never in a chat message asking the
   person to accept: they accept in Cartograph, after reading it. Tell
   them what you proposed, and what you left open and why.
10. Some judgement no check can make: whether an outcome describes a state
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
	// ChangeSet is the change set the step was in, so a person following
	// opens its draft there.
	ChangeSet string
	Text      []byte   // the manifest, for its name
	Fields    []string // what a draft changed
	Checks    []engine.Check
	Proposal  string
	Parts     int
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
	if st.ChangeSet != "" {
		agent["changeSet"] = st.ChangeSet
	}
	focus := ""
	if len(st.Fields) > 0 {
		focus = st.Fields[0]
	}
	// On the manifest's shared draft only when the step was there: work in
	// a change set is not on the shared draft, and is followed on the feed.
	if st.Kind != "" && st.ID != "" && st.ChangeSet == "" {
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

// inChangeSet is the call reading the agent's change set: id, else its
// latest open one; with open, a new one when it has none. found is false
// when it has none and none was opened.
func (c call) inChangeSet(id string, open bool) (store.ChangeSet, call, bool, error) {
	e := c.o.Engine
	var cs store.ChangeSet
	var err error
	if open {
		cs, err = e.WorkingChangeSet(c.ctx, id)
	} else {
		var found bool
		cs, found, err = e.CurrentChangeSet(c.ctx, id)
		if err == nil && !found {
			return store.ChangeSet{}, c, false, nil
		}
	}
	if err != nil {
		return store.ChangeSet{}, c, false, err
	}
	ctx, err := e.InChangeSet(c.ctx, cs.ID)
	if err != nil {
		return store.ChangeSet{}, c, false, err
	}
	c.ctx = context.WithValue(ctx, workingKey{}, working{set: cs.ID, theirs: !e.WorksIn(c.ctx, cs)})
	return cs, c, true, nil
}

// workingKey carries the change set a call works in.
type workingKey struct{}

// working is the change set a call works in, and whether it is the
// person's, brought into rather than the agent's own (docs/adr/0025).
type working struct {
	set    string
	theirs bool
}

// workingOn is the change set the call on ctx works in, if it has one.
func workingOn(ctx context.Context) (working, bool) {
	w, ok := ctx.Value(workingKey{}).(working)
	return w, ok
}

// finish is how the work ends: the agent proposes its own change set; one
// it was brought into, its person proposes and merges.
func finish(ctx context.Context) string {
	if w, ok := workingOn(ctx); ok && w.theirs {
		return "tell your person it is ready; the change set is theirs to propose and merge"
	}
	return "propose the change set with propose"
}

// reading joins the change set the agent works in, where it has one, so a
// tool that only reads (validate, guide, relevant, match) sees its drafts
// as saved, as the tools that write do.
func (c call) reading(set string) call {
	if _, in, found, err := c.inChangeSet(set, false); err == nil && found {
		return in
	}
	return c
}

// proposeChangeSet proposes the change set, and announces it.
func proposeChangeSet(c call, cs store.ChangeSet, reason string, waive map[string]map[string]string) (any, error) {
	e := c.o.Engine
	out, err := e.ProposeChangeSet(c.ctx, cs.ID, reason, waive)
	if err != nil {
		return nil, err
	}
	view, err := e.ViewChangeSet(c.ctx, out.ID)
	if err != nil {
		return nil, err
	}
	var items []string
	for _, it := range view.Items {
		if it.Item.Included {
			items = append(items, it.Item.Kind+"/"+it.Item.ID)
		}
	}
	if len(view.Items) > 0 {
		last := view.Items[len(view.Items)-1].Item
		c.announce(step{Step: "propose", Kind: last.Kind, ID: last.ID, Text: last.Text, Proposal: out.ID, Parts: len(items), ChangeSet: out.ID})
	}
	return changeSetOut(out, items), nil
}

// componentsOut is the components graph as an agent reads it, each
// piece of work by name with its kind and id.
func componentsOut(g engine.ComponentGraph) map[string]any {
	ref := func(r engine.Ref) string { return r.Kind + "/" + r.ID }
	nodes := []map[string]any{}
	for _, n := range g.Nodes {
		nodes = append(nodes, map[string]any{"work": ref(n.Ref), "name": n.Name, "months": n.Months, "dependents": n.Dependents,
			"mostDependedOn": n.MostDependedOn, "critical": n.Critical, "inLoop": n.InLoop})
	}
	edges := []map[string]any{}
	for _, ed := range g.Edges {
		edges = append(edges, map[string]any{"from": ref(ed.From), "dependsOn": ref(ed.To), "why": ed.Why})
	}
	path := []string{}
	for _, r := range g.CriticalPath {
		path = append(path, ref(r))
	}
	loops := [][]string{}
	for _, l := range g.Loops {
		var one []string
		for _, r := range l {
			one = append(one, ref(r))
		}
		loops = append(loops, one)
	}
	return map[string]any{"work": nodes, "dependencies": edges, "criticalPath": path, "criticalMonths": g.CriticalMonths, "loops": loops}
}

// scheduleOut is a project's milestones placed on time, as an agent reads
// them.
func scheduleOut(items []engine.ScheduleItem) map[string]any {
	out := []map[string]any{}
	for _, it := range items {
		m := map[string]any{"id": it.ID, "name": it.Name, "form": it.Form, "month": it.Month, "critical": it.Critical}
		if it.NotBefore != "" {
			m["notBefore"] = it.NotBefore
		}
		if it.NotAfter != "" {
			m["notAfter"] = it.NotAfter
		}
		if len(it.WaitsOn) > 0 {
			m["waitsOn"] = it.WaitsOn
		}
		if it.Pending {
			m["setWhenAnEventHappens"] = true
		}
		if it.Late {
			m["late"] = true
		}
		if it.Unplaced {
			m["unplaced"] = true
		}
		out = append(out, m)
	}
	return map[string]any{"milestones": out}
}

// changeSetOut is a change set as an agent reads it after proposing.
func changeSetOut(cs store.ChangeSet, items []string) map[string]any {
	waived := make([]map[string]any, len(cs.Waivers))
	for i, w := range cs.Waivers {
		waived[i] = map[string]any{"on": w.On, "check": w.Check, "reason": w.Reason}
	}
	return map[string]any{"changeSet": cs.ID, "title": cs.Title, "status": cs.Status, "items": items, "leftForYourPerson": waived,
		"next": "Proposed. Your person reviews the whole change set in Cartograph, under Change sets, and accepts it there; tell them what it holds and what you left open."}
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
			"by passing openChecks to propose ({Kind/id: {check id: why}}); your person sees each reason before deciding.")
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
	leaveIn struct {
		ChangeSet string      `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out"`
		Kind      string      `json:"kind"`
		ID        string      `json:"id"`
		Check     string      `json:"check,omitempty" jsonschema:"the check's id, as checks reports it"`
		Reason    string      `json:"reason" jsonschema:"what your person must supply or decide, in one line they can act on; empty takes it back"`
		Also      []leaveItem `json:"also,omitempty" jsonschema:"more checks the same missing fact leaves open, on this draft or others, each {kind, id, check}: one reason for all of them"`
		Correct   bool        `json:"correct,omitempty" jsonschema:"true to put this reason in place of the one already given; left out, a second fact behind the same check adds its reason to the first"`
		Asked     string      `json:"asked,omitempty" jsonschema:"what you asked your person, with the reason you gave, and what they answered; \"not available\" only when you were told to work without them. Required to leave a check"`
	}
	leaveItem struct {
		Kind  string `json:"kind"`
		ID    string `json:"id"`
		Check string `json:"check"`
	}
	readingIn struct {
		ChangeSet string `json:"changeSet,omitempty" jsonschema:"the change set to read in: your own, or your person's when they ask you to help with it; your latest open one when left out"`
	}
	manifestRef struct {
		ChangeSet string `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string `json:"kind" jsonschema:"the manifest's kind"`
		ID        string `json:"id" jsonschema:"the manifest's id"`
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
	localeOnly struct {
		Locale string `json:"locale,omitempty"`
	}
	matchIn struct {
		Kind  string `json:"kind" jsonschema:"the kind to look in"`
		Level string `json:"level,omitempty" jsonschema:"for a Goal, the level: goal, objective or outcome"`
		Text  string `json:"text" jsonschema:"what the new thing would say: its name and statement, in the document's words"`
	}
	fromIdeaIn struct {
		Kind string `json:"kind" jsonschema:"the kind being defined, such as Project"`
		Idea string `json:"idea" jsonschema:"the work as your person described it, in their words"`
	}
	relevantIn struct {
		Text  string   `json:"text" jsonschema:"what the work is about, and what has been written of it so far"`
		Kinds []string `json:"kinds,omitempty" jsonschema:"the kinds to rank, such as Goal, KPI, Programme, BeneficiaryGroup, DataSource; every kind a piece of work names when left out"`
		Level string   `json:"level,omitempty" jsonschema:"for Goal, the level to rank among: goal, objective or outcome"`
	}
	startWorkIn struct {
		Title       string `json:"title" jsonschema:"what this piece of work is, as your person would say it"`
		Description string `json:"description,omitempty" jsonschema:"what it is for, and what it will hold"`
	}
	proposeIn struct {
		ChangeSet  string                       `json:"changeSet,omitempty" jsonschema:"the change set to propose; your latest open one when left out"`
		Reason     string                       `json:"reason" jsonschema:"why, in a sentence the person will read"`
		OpenChecks map[string]map[string]string `json:"openChecks,omitempty" jsonschema:"only for checks you cannot meet without your person: Kind/id to (check id to why); every other open check refuses the proposal"`
	}
	nextIn struct {
		ChangeSet string   `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Work      []string `json:"work,omitempty" jsonschema:"every manifest in this piece of work, as Kind/id; leave it out for the stage of the workspace to write now"`
		Locale    string   `json:"locale,omitempty"`
	}
	editIn struct {
		ChangeSet string         `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string         `json:"kind"`
		ID        string         `json:"id"`
		Work      []string       `json:"work,omitempty" jsonschema:"every other manifest you are defining with this one, as Kind/id (the ones you will propose together): their drafts are read and checked with it, and what they still lack is reported in around"`
		Set       map[string]any `json:"set,omitempty" jsonschema:"fields to set, by JSON pointer, such as {\"/spec/objective\": \"...\"}; in a list, a number replaces that item, and - (or the next number) appends one; null removes the field"`
		Unset     []string       `json:"unset,omitempty" jsonschema:"fields or list items to remove, by JSON pointer"`
	}
	manifestIn struct {
		ChangeSet string         `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string         `json:"kind"`
		ID        string         `json:"id"`
		Manifest  map[string]any `json:"manifest" jsonschema:"the whole manifest: apiVersion, kind, metadata and spec"`
		Work      []string       `json:"work,omitempty" jsonschema:"every other manifest you are defining with this one, as Kind/id (the ones you will propose together): their drafts are read and checked with it, and what they still lack is reported in around"`
	}
	saveIn struct {
		ChangeSet string         `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string         `json:"kind"`
		ID        string         `json:"id"`
		Manifest  map[string]any `json:"manifest" jsonschema:"the whole manifest: apiVersion, kind, metadata and spec"`
		Replace   bool           `json:"replace,omitempty" jsonschema:"true to replace a draft this change set already holds, every field of it; leave out to create, and change an existing draft with edit_draft"`
		Work      []string       `json:"work,omitempty" jsonschema:"every other manifest you are defining with this one, as Kind/id (the ones you will propose together): their drafts are read and checked with it, and what they still lack is reported in around"`
	}
	proposeSaveIn struct {
		ChangeSet string         `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string         `json:"kind"`
		ID        string         `json:"id"`
		Manifest  map[string]any `json:"manifest,omitempty" jsonschema:"the manifest to save; the current draft when left out"`
		Reason    string         `json:"reason" jsonschema:"why, in a sentence the person will read"`
		// Checks the agent could not meet, each with why; the person sees
		// them on the proposal.
		OpenChecks map[string]string `json:"openChecks,omitempty" jsonschema:"only for a check you cannot meet without your person: check id to why it is left open; every other open check refuses the proposal"`
	}
	proposeSetIn struct {
		ChangeSet string        `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
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
		ChangeSet string         `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string         `json:"kind,omitempty" jsonschema:"the manifest's kind; leave kind and id out to check the whole change set, as propose will"`
		ID        string         `json:"id,omitempty"`
		Manifest  map[string]any `json:"manifest,omitempty" jsonschema:"a manifest to check without saving it; its draft or latest version when left out"`
		Work      []string       `json:"work,omitempty" jsonschema:"every other manifest you are defining with this one, as Kind/id (the ones you will propose together): their drafts are read and checked with it, and what they still lack is reported in around"`
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

	tool(s, o, person, &sdk.Tool{Name: "get", Description: "One manifest as it stands for you: your change set's draft of it, where you have one, else the record, as YAML.", Annotations: readOnly},
		func(c call, in manifestRef) (any, error) {
			cs, c, found, err := c.inChangeSet(in.ChangeSet, false)
			if err != nil {
				return nil, err
			}
			if found {
				text, inSet, err := e.ChangeSetText(c.ctx, cs.ID, in.Kind, in.ID)
				if err != nil {
					return nil, err
				}
				c.announce(step{Step: "read", Kind: in.Kind, ID: in.ID, Text: text, ChangeSet: cs.ID})
				return map[string]any{"yaml": string(text), "changeSet": cs.ID, "inChangeSet": inSet}, nil
			}
			v, err := e.Get(c.ctx, in.Kind, in.ID)
			if err != nil {
				return nil, err
			}
			c.announce(step{Step: "read", Kind: in.Kind, ID: in.ID, Text: v.YAML})
			return map[string]any{"version": v.Number, "yaml": string(v.YAML)}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "schema", Description: "The JSON Schema of a kind: every field, its type, allowed values and what it refers to, " +
		"with the shared definitions it points to (metadata, references, key results) in defs.", Annotations: readOnly},
		func(c call, in kindOnly) (any, error) {
			schema, err := e.Schema(in.Kind)
			if err != nil {
				return nil, err
			}
			defs, err := e.SchemaDefs(in.Kind)
			if err != nil {
				return nil, err
			}
			return map[string]any{"schema": schema, "defs": defs}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "guide", Description: "Read this before drafting any manifest. How to define a kind well, for one level: what it is, every step and field in order, " +
		"what each field must say with right and wrong examples, the checks each answers and how to meet them, the links to make on other kinds, " +
		"and the organisation's existing records to reuse for each reference and link.", Annotations: readOnly},
		func(c call, in guideIn) (any, error) {
			c = c.reading("")
			g, err := e.Guide(c.ctx, in.Kind, in.Level, in.Locale)
			if err != nil {
				return nil, err
			}
			c.announce(step{Step: "guide", Kind: in.Kind})
			if in.Step == "" {
				return withPrepare(e, g), nil
			}
			// One step only, for a long flow such as a project's; the
			// step keys come from a guide without step.
			for _, st := range g.Steps {
				if st.Key == in.Step {
					g.Steps = []engine.GuideStep{st}
					return withPrepare(e, g), nil
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
		"In a change set it also lists what is still open on the set's other drafts (openInChangeSet), exactly as propose will find it; " +
		"leave kind and id out to check the whole set. Run it after every save_draft; propose only when nothing is open.", Annotations: readOnly},
		func(c call, in checksIn) (any, error) {
			cs, c, found, err := c.inChangeSet(in.ChangeSet, false)
			if err != nil {
				return nil, err
			}
			if in.Kind == "" && in.ID == "" {
				if !found {
					return nil, fmt.Errorf("no change set open: name the manifest to check, or start_work")
				}
				out, err := withSet(c, checkReport(nil), cs.ID, "", "")
				if err == nil {
					// For the whole set, its own line is the one to follow.
					out["next"] = out["setNext"]
					delete(out, "setNext")
					delete(out, "open")
					delete(out, "met")
				}
				return out, err
			}
			if in.Manifest != nil {
				text, err := e.Codec().Encode(in.Manifest)
				if err != nil {
					return nil, err
				}
				checks, err := e.ChecksOf(c.ctx, in.Kind, in.ID, text)
				if err != nil {
					return nil, err
				}
				problems, err := e.Validate(c.ctx, in.Kind, text)
				if err != nil {
					return nil, err
				}
				return checkReport(withProblems(checks, problems)), nil
			}
			checks, err := e.DraftChecks(c.ctx, in.Kind, in.ID)
			if errors.Is(err, engine.ErrNotFound) {
				return nil, fmt.Errorf("%s/%s has no draft or version yet: save_draft it, or pass the manifest to check", in.Kind, in.ID)
			}
			if err != nil {
				return nil, err
			}
			problems, err := e.DraftProblems(c.ctx, in.Kind, in.ID)
			if err != nil {
				return nil, err
			}
			c.announce(step{Step: "checks", Kind: in.Kind, ID: in.ID, Checks: checks, ChangeSet: cs.ID})
			out := withAround(c, leaving(c, checkReport(withProblems(checks, problems)), in.Kind, in.ID), in.Kind, in.ID, in.Work)
			if !found {
				return out, nil
			}
			return withSet(c, out, cs.ID, in.Kind, in.ID)
		})

	tool(s, o, person, &sdk.Tool{Name: "goal_tree", Description: "Every goal, objective and outcome as a tree, with what is aligned to each.", Annotations: readOnly},
		func(c call, _ none) (any, error) { return e.GoalTree(c.ctx) })

	tool(s, o, person, &sdk.Tool{Name: "references", Description: "What a manifest references, and what references it.", Annotations: readOnly},
		func(c call, in manifestRef) (any, error) { return e.References(c.ctx, in.Kind, in.ID) })

	tool(s, o, person, &sdk.Tool{Name: "components", Description: "The graph of components across every project and programme (TAXONOMY.md D46), as your change set reads it: " +
		"who depends on whom (edges run from the work that depends to the work it depends on), how many months each runs, how widely each is depended on, " +
		"any loops, and the critical path, the chain that runs longest on the calendar. Ask it when your person asks what holds the work up, what a delay would move, " +
		"or what to start first.", Annotations: readOnly},
		func(c call, in readingIn) (any, error) {
			c = c.reading(in.ChangeSet)
			g, err := e.Components(c.ctx)
			if err != nil {
				return nil, err
			}
			return componentsOut(g), nil
		})

	tool(s, o, person, &sdk.Tool{Name: "schedule", Description: "A project's milestones placed on time (TAXONOMY.md D47, D48), as your change set reads it: the month each falls in, " +
		"its window, what it waits on, whether it is still to be set by an event or late, and the chain that decides its last date (critical). " +
		"A milestone that waits on another project's is placed from there.", Annotations: readOnly},
		func(c call, in manifestRef) (any, error) {
			c = c.reading(in.ChangeSet)
			items, err := e.Schedule(c.ctx, in.ID)
			if err != nil {
				return nil, err
			}
			return scheduleOut(items), nil
		})

	tool(s, o, person, &sdk.Tool{Name: "match", Description: "Before defining anything, the existing records of a kind that already say what it would say, most likely first: " +
		"judged by Cartograph's decision model where one is configured, else by the words they share (by says which). Work on a match instead of defining another.", Annotations: readOnly},
		func(c call, in matchIn) (any, error) {
			c = c.reading("")
			return map[string]any{"matches": e.MatchExisting(c.ctx, in.Kind, in.Level, in.Text)}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "decision_model", Description: "Call first, once a session: whether Cartograph has a decision model configured and answering now, whatever backs it. " +
		"When it is ready, relevant, match and understand rank by meaning; when it is not, by shared words only, so read the registers more fully yourself.", Annotations: readOnly},
		func(c call, _ struct{}) (any, error) {
			return e.DecisionModel(c.ctx), nil
		})

	tool(s, o, person, &sdk.Tool{Name: "relevant", Description: "What in the workspace is relevant to a piece of work: the likeliest few records of each kind, likeliest first, " +
		"ranked by the decision model where one answers, else by shared words (available says which). Call it with what the work is about before choosing what a draft names, " +
		"and offer its shortlist first. It ranks; it never decides: your person still chooses, and anything else in the register remains a choice.", Annotations: readOnly},
		func(c call, in relevantIn) (any, error) {
			c = c.reading("")
			return e.Relevant(c.ctx, in.Text, in.Kinds, in.Level, 0)
		})

	tool(s, o, person, &sdk.Tool{Name: "from_idea", Description: "When your person describes their work roughly, the sentence of it that answers each question the walk for kind asks " +
		"(who it is for, what is wrong today, what will be different, what it delivers, when, how success is known, who runs the result), only where the decision model is sure. " +
		"Start each answer from their own sentence, and ask about the questions it leaves out.", Annotations: readOnly},
		func(c call, in fromIdeaIn) (any, error) {
			answers, ok := e.FromIdea(c.ctx, in.Kind, in.Idea)
			return map[string]any{"available": ok, "answers": answers}, nil
		})

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
			c = c.reading(in.ChangeSet)
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

	tool(s, o, person, &sdk.Tool{Name: "change_sets", Description: "The change sets open for your person, theirs and their agents', each with its title, " +
		"who works in it, its drafts, and how many checks are still open on them, as propose would count them. When your person asks you to help with a piece of " +
		"their work (to refine it, or to meet its checks before they merge it), find it here and pass its changeSet to every tool you call for it: checks with " +
		"no kind and id lists what is open, and save_draft, edit_draft and leave_open work in it beside them, live. A change set you were brought into is " +
		"theirs: you never propose it, you tell your person when nothing is open, and they propose and merge it.", Annotations: readOnly},
		func(c call, _ none) (any, error) {
			sets, err := e.ChangeSets(c.ctx, store.ChangeSetOpen, false)
			if err != nil {
				return nil, err
			}
			changeSets := []map[string]any{}
			for _, cs := range sets {
				view, err := e.ViewChangeSet(c.ctx, cs.ID)
				if err != nil {
					return nil, err
				}
				var parts []string
				for _, it := range view.Items {
					if it.Item.Included {
						parts = append(parts, it.Item.Kind+"/"+it.Item.ID)
					}
				}
				workedBy := "your person"
				if cs.Agent != "" {
					workedBy = "the agent " + cs.Agent
				}
				out := map[string]any{"changeSet": cs.ID, "title": cs.Title, "description": cs.Description, "workedBy": workedBy,
					"yours": e.WorksIn(c.ctx, cs), "items": parts, "updated": cs.Updated}
				in, err := e.InChangeSet(c.ctx, cs.ID)
				if err != nil {
					return nil, err
				}
				open, err := e.OpenInChangeSet(in, cs.ID)
				var invalid *engine.ValidationError
				switch {
				case errors.As(err, &invalid):
					out["notValid"] = len(invalid.Problems)
				case err != nil:
					return nil, err
				default:
					unmet, left := 0, 0
					for _, oc := range open {
						if oc.Left != "" {
							left++
						} else {
							unmet++
						}
					}
					out["open"], out["leftForYourPerson"] = unmet, left
				}
				changeSets = append(changeSets, out)
			}
			return map[string]any{"changeSets": changeSets}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "my_proposals", Description: "What is waiting for your person to decide: the change sets proposed (changeSets, each with " +
		"its items) and the single items proposed, such as a KPI reading (items). A proposed change set's drafts still stand: checks and next read them " +
		"until your person accepts it.", Annotations: readOnly},
		func(c call, _ none) (any, error) {
			items, err := e.Proposals(c.ctx, store.ProposalFilter{Status: store.ProposalOpen})
			if err != nil {
				return nil, err
			}
			sets, err := e.ChangeSets(c.ctx, store.ChangeSetProposed, false)
			if err != nil {
				return nil, err
			}
			changeSets := []map[string]any{}
			for _, cs := range sets {
				view, err := e.ViewChangeSet(c.ctx, cs.ID)
				if err != nil {
					return nil, err
				}
				var parts []string
				for _, it := range view.Items {
					if it.Item.Included {
						parts = append(parts, it.Item.Kind+"/"+it.Item.ID)
					}
				}
				changeSets = append(changeSets, map[string]any{"changeSet": cs.ID, "title": cs.Title, "reason": cs.Reason, "items": parts})
			}
			return map[string]any{"changeSets": changeSets, "items": items}, nil
		})

	if o.Reports != nil {
		tool(s, o, person, &sdk.Tool{Name: "report", Description: "A report over the record: projects, kpi-readings, alignment or teams.", Annotations: readOnly},
			func(c call, in reportIn) (any, error) {
				t, err := o.Reports.Run(c.ctx, in.Name)
				return map[string]any{"columns": t.Columns, "rows": t.Rows}, err
			})
	}

	tool(s, o, person, &sdk.Tool{Name: "save_draft", Description: "Create a manifest in your change set: your own draft of it, apart from the record and from every other agent's work, " +
		"which your person reviews with the rest of the change set before anything is saved. Use it to create; change fields with edit_draft.", Annotations: drafting},
		func(c call, in saveIn) (any, error) {
			text, err := e.Codec().Encode(in.Manifest)
			if err != nil {
				return nil, err
			}
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			before, drafted, _ := e.ChangeSetText(c.ctx, cs.ID, in.Kind, in.ID)
			if drafted && !in.Replace {
				// A whole manifest put back over a draft undoes every field
				// changed since, the person's edits among them.
				return nil, fmt.Errorf("%s/%s is already drafted in this change set: change its fields with edit_draft, or pass replace true to put this whole manifest in its place", in.Kind, in.ID)
			}
			if err := e.SaveInChangeSet(c.ctx, cs.ID, in.Kind, in.ID, text); err != nil {
				return nil, err
			}
			if c.ctx, err = e.InChangeSet(c.ctx, cs.ID); err != nil {
				return nil, err
			}
			problems, err := e.DraftProblems(c.ctx, in.Kind, in.ID)
			if err != nil {
				return nil, err
			}
			checks, err := e.DraftChecks(c.ctx, in.Kind, in.ID)
			if err != nil {
				return nil, err
			}
			c.announce(step{Step: "draft", Kind: in.Kind, ID: in.ID, Text: text, Fields: e.ChangedFields(before, text, 32), Checks: checks, ChangeSet: cs.ID})
			out := withAround(c, leaving(c, checkReport(withProblems(checks, problems)), in.Kind, in.ID), in.Kind, in.ID, in.Work)
			out["saved"], out["problems"], out["changeSet"] = "draft", problems, cs.ID
			if len(problems) > 0 {
				out["saved"] = "draft, not valid yet: kept as you sent it, and propose refuses it until each problem is fixed with edit_draft"
			}
			return out, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "taxonomy", Description: "Every kind Cartograph keeps, in the order of the strategy: what each is in one sentence, its levels, " +
		"and what plans and documents often call it instead. Read it before recording anything from a document, and map each thing the document says " +
		"onto the kind it is by definition, whatever the document calls it.", Annotations: readOnly},
		func(c call, in localeOnly) (any, error) {
			t, err := e.Taxonomy(in.Locale)
			if err != nil {
				return nil, err
			}
			return map[string]any{"kinds": t, "rule": "Map by what a thing is, against each summary and the guide's definition, never by the word a document uses. " +
				"Record it as Cartograph's kind and level, keep the document's wording in its statement, and cite the document as its source. " +
				"porting says where each part of a charter or plan goes, and what stays out.", "porting": porting}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "next", Description: "What to do next, in the order of work: the record is a directed acyclic graph, written from the top down " +
		"(purpose, goals, objectives, outcomes, KPIs, gaps, then portfolios, programmes, operations and projects), each thing naming only what comes before it. " +
		"With work (every manifest you are working on), the next open check across it: each manifest is finished in one visit, what it is, its numbers " +
		"(from the documents your person gave you), then its links to what is already there, before the next one down. Without work, the stage of the workspace " +
		"to write now, and how far each has got: an empty workspace starts at its purpose. Call it whenever you are unsure what comes next.", Annotations: readOnly},
		func(c call, in nextIn) (any, error) {
			cs, c, found, err := c.inChangeSet(in.ChangeSet, false)
			if err != nil {
				return nil, err
			}
			if found && len(in.Work) == 0 {
				view, err := e.ViewChangeSet(c.ctx, cs.ID)
				if err != nil {
					return nil, err
				}
				for _, it := range view.Items {
					in.Work = append(in.Work, it.Item.Kind+"/"+it.Item.ID)
				}
			}
			var work []engine.Ref
			for _, w := range in.Work {
				if k, i, ok := strings.Cut(w, "/"); ok && k != "" && i != "" {
					work = append(work, engine.Ref{Kind: k, ID: i})
				}
			}
			if len(work) == 0 {
				return workspaceNext(c.ctx, e)
			}
			w, err := e.Work(c.ctx, work, in.Locale)
			if err != nil {
				return nil, err
			}
			out := map[string]any{"open": w.Open}
			if len(w.Tasks) == 0 {
				out["next"] = allMet(c.ctx, e)
				return out, nil
			}
			out["next"] = firstNext(c.ctx, e, w.Tasks[0])
			then := w.Tasks[1:]
			if len(then) > 8 {
				then = then[:8]
			}
			out["task"], out["then"] = w.Tasks[0], then
			return out, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "edit_draft", Description: "Set or clear single fields of your change set's draft of a manifest, by JSON pointer, leaving every other field as it stands, " +
		"so changes your person makes in the change set at the same time are kept. Returns the draft as it now stands, their changes included, and its checks. " +
		"Starts the draft from the record when your change set has none.", Annotations: drafting},
		func(c call, in editIn) (any, error) {
			if len(in.Set) == 0 && len(in.Unset) == 0 {
				return nil, fmt.Errorf("name at least one field to set or unset")
			}
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			text, err := e.EditInChangeSet(c.ctx, cs.ID, in.Kind, in.ID, in.Set, in.Unset)
			if err != nil {
				return nil, err
			}
			if c.ctx, err = e.InChangeSet(c.ctx, cs.ID); err != nil {
				return nil, err
			}
			problems, err := e.DraftProblems(c.ctx, in.Kind, in.ID)
			if err != nil {
				return nil, err
			}
			checks, err := e.DraftChecks(c.ctx, in.Kind, in.ID)
			if err != nil {
				return nil, err
			}
			fields := make([]string, 0, len(in.Set)+len(in.Unset))
			for p := range in.Set {
				fields = append(fields, p)
			}
			fields = append(fields, in.Unset...)
			sort.Strings(fields)
			c.announce(step{Step: "draft", Kind: in.Kind, ID: in.ID, Text: text, Fields: fields, Checks: checks, ChangeSet: cs.ID})
			out := withAround(c, leaving(c, checkReport(withProblems(checks, problems)), in.Kind, in.ID), in.Kind, in.ID, in.Work)
			// The whole draft, as everyone in the change set now has it:
			// what the person changed is in here to build on.
			out["saved"], out["problems"], out["draft"], out["changeSet"] = "draft", problems, string(text), cs.ID
			if len(text) > draftEcho {
				// A large draft echoed whole on every edit buries the answer;
				// get reads it when the agent needs to build on it.
				out["draft"] = fmt.Sprintf("%d bytes, not repeated here: read it whole with get (kind %s, id %s) before building on what others changed", len(text), in.Kind, in.ID)
			}
			if len(problems) > 0 {
				out["saved"] = "draft, not valid yet: kept as you sent it, and propose refuses it until each problem is fixed with edit_draft"
			}
			return out, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "leave_open", Description: "Leave a check on one of your drafts for your person, with the reason they will read: only after asking them, for what they cannot settle yet, " +
		"a figure or date no document gives, a score nobody has made, a choice that is theirs. next then passes it by, and propose waives it with this reason, " +
		"so you say why once, as you go. An empty reason takes it back. Never leave a check you could meet from the documents.", Annotations: drafting},
		func(c call, in leaveIn) (any, error) {
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.Reason) != "" && strings.TrimSpace(in.Asked) == "" {
				// A check is left for the person only once they were asked:
				// they may know the figure, or decide it now.
				return nil, fmt.Errorf("ask your person first: say what the check needs and why you ask (what the document says or leaves out), " +
					"offer the options you have, and pass what they answered as asked; \"not available\" only when you were told to work without them")
			}
			items := in.Also
			if in.Check != "" {
				items = append([]leaveItem{{Kind: in.Kind, ID: in.ID, Check: in.Check}}, items...)
			}
			if len(items) == 0 {
				return nil, fmt.Errorf("name the check to leave: check, or also for several")
			}
			var left []string
			for _, it := range items {
				if err := e.LeaveOpen(c.ctx, cs.ID, it.Kind, it.ID, it.Check, in.Reason, in.Correct); err != nil {
					return nil, err
				}
				left = append(left, it.Kind+"/"+it.ID+" "+it.Check)
			}
			if strings.TrimSpace(in.Reason) == "" {
				return map[string]any{"takenBack": left, "changeSet": cs.ID,
					"next": "These checks are open again: meet each, or leave it with a reason, before you propose."}, nil
			}
			return map[string]any{"left": left, "reason": in.Reason, "changeSet": cs.ID,
				"next": "Carry on with next: it passes this check by, and propose waives it with your reason."}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "discard_draft", Description: "Drop a draft from your change set, as if it had never been drafted there: one saved under the wrong id, " +
		"or one the work no longer needs. Refused while another draft in the set names it; change or discard that one first.", Annotations: drafting},
		func(c call, in manifestRef) (any, error) {
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			if err := e.DiscardChangeItem(c.ctx, cs.ID, in.Kind, in.ID); err != nil {
				return nil, err
			}
			return map[string]any{"discarded": in.Kind + "/" + in.ID, "changeSet": cs.ID}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "start_work", Description: "Open a new change set for a new piece of work, with a title and a description of what it is for, as you would open a branch for a task. " +
		"Everything you draft afterwards goes into it, apart from your other work and every other agent's, and your person reviews and accepts it whole.", Annotations: drafting},
		func(c call, in startWorkIn) (any, error) {
			cs, err := e.StartChangeSet(c.ctx, in.Title, in.Description)
			if err != nil {
				return nil, err
			}
			return map[string]any{"changeSet": cs.ID, "title": cs.Title, "next": "Draft into it with save_draft and edit_draft; propose it with propose when every check is met."}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "propose", Description: "Propose your change set for your person to review and accept in Cartograph, whole: every draft in it is checked with the others, " +
		"as a person's save would be, and saved together, in the order their references need, when they accept.", Annotations: proposal},
		func(c call, in proposeIn) (any, error) {
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			return proposeChangeSet(c, cs, in.Reason, in.OpenChecks)
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_save", Description: "Propose your change set, after saving this manifest into it when you pass one: the same as propose, kept for agents that know it.", Annotations: proposal},
		func(c call, in proposeSaveIn) (any, error) {
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			if in.Manifest != nil {
				text, err := e.Codec().Encode(in.Manifest)
				if err != nil {
					return nil, err
				}
				if err := e.SaveInChangeSet(c.ctx, cs.ID, in.Kind, in.ID, text); err != nil {
					return nil, err
				}
			}
			waive := map[string]map[string]string{}
			if len(in.OpenChecks) > 0 {
				waive[in.Kind+"/"+in.ID] = in.OpenChecks
			}
			return proposeChangeSet(c, cs, in.Reason, waive)
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_set", Description: "Propose your change set, after saving these manifests into it when you pass them: the same as propose, kept for agents that know it.", Annotations: proposal},
		func(c call, in proposeSetIn) (any, error) {
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			for _, m := range in.Manifests {
				if m.Manifest == nil {
					continue
				}
				text, err := e.Codec().Encode(m.Manifest)
				if err != nil {
					return nil, err
				}
				if err := e.SaveInChangeSet(c.ctx, cs.ID, m.Kind, m.ID, text); err != nil {
					return nil, err
				}
			}
			return proposeChangeSet(c, cs, in.Reason, in.OpenChecks)
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
// withProblems puts what a save would refuse in front of the checks, as
// open checks of their own: a draft with a missing required field or an
// over-long name is not ready to propose, and an agent reading only the
// open checks must hear it now rather than at propose.
func withProblems(checks []engine.Check, problems []engine.Problem) []engine.Check {
	if len(problems) == 0 {
		return checks
	}
	out := make([]engine.Check, 0, len(problems)+len(checks))
	for _, p := range problems {
		msg := p.Message
		if p.Path != "" {
			msg = p.Path + ": " + msg
		}
		out = append(out, engine.Check{ID: "schema", State: "block", Message: msg})
	}
	return append(out, checks...)
}

// withSet adds the checks still open on the change set's other drafts,
// as propose will find them, so checks and propose never disagree: a
// draft's own checks can all be met while an outcome drafted beside it
// is not SMART yet, and propose refuses the set for that.
func withSet(c call, out map[string]any, set, kind, id string) (map[string]any, error) {
	open, err := c.o.Engine.OpenInChangeSet(c.ctx, set)
	var invalid *engine.ValidationError
	if errors.As(err, &invalid) {
		// propose would refuse the set before checking it: say why.
		var refused []map[string]any
		for _, p := range invalid.Problems {
			refused = append(refused, map[string]any{"check": "schema", "state": "block", "message": strings.TrimPrefix(p.Path+": "+p.Message, ": ")})
		}
		out["openInChangeSet"] = refused
		out["setNext"] = "propose refuses this change set as it stands: fix what is not valid (openInChangeSet) first."
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var elsewhere, left []map[string]any
	for _, oc := range open {
		if oc.Left != "" {
			left = append(left, map[string]any{"kind": oc.Kind, "id": oc.ManifestID, "check": oc.ID, "reason": oc.Left})
			continue
		}
		if oc.Kind == kind && oc.ManifestID == id {
			continue
		}
		elsewhere = append(elsewhere, map[string]any{"kind": oc.Kind, "id": oc.ManifestID, "check": oc.ID, "state": oc.State, "message": oc.Message, "section": oc.Section})
	}
	out["openInChangeSet"] = elsewhere
	switch {
	case len(left) > 0 && kind == "":
		out["leftForYourPerson"] = left
	case len(left) > 0:
		// Checking one manifest: its own left checks are in left; the
		// set's are counted, and listed by checks without kind and id.
		out["leftForYourPersonInChangeSet"] = len(left)
	}
	if unnamed, err := c.o.Engine.UnnamedInChangeSet(c.ctx, set); err == nil && len(unnamed) > 0 {
		names := make([]string, len(unnamed))
		for i, u := range unnamed {
			names[i] = u.Kind + "/" + u.ID
		}
		out["namedByNothing"] = names
		out["namedByNothingNext"] = "Nothing names these drafts: name each where it belongs (a role in resources, a body on the escalation route, a source a KPI reads), or discard_draft it."
	}
	if len(elsewhere) > 0 {
		out["setNext"] = fmt.Sprintf("%d check%s still open on other drafts in this change set (openInChangeSet). "+
			"Meet each, or leave it with a reason your person can read, then %s.", len(elsewhere), map[bool]string{true: " is", false: "s are"}[len(elsewhere) == 1], finish(c.ctx))
	} else if len(open) == len(left) {
		out["setNext"] = "Nothing is open across the change set but what you left for your person. Next, " + finish(c.ctx) + "."
	}
	return out, nil
}

// leaving moves the checks left for the person out of a report's open
// list, into left with their reasons.
func leaving(c call, out map[string]any, kind, id string) map[string]any {
	open, _ := out["open"].([]engine.Check)
	var still []engine.Check
	var left []map[string]any
	for _, ch := range open {
		if why := engine.LeftFor(c.ctx, kind, id, ch.ID); why != "" {
			left = append(left, map[string]any{"check": ch.ID, "reason": why})
		} else {
			still = append(still, ch)
		}
	}
	if len(left) > 0 {
		if still == nil {
			still = []engine.Check{}
		}
		out["open"], out["left"] = still, left
	}
	return out
}

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
		next = "Not ready to propose. Meet each open check: ask your person for what only they know, never invent it, and change the draft with edit_draft; when your person is not there, leave_open what only they can answer."
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
	// What to do next across the whole piece of work, so the agent is led
	// one step at a time without being told the order.
	// What a save would refuse comes first: the work's own order knows
	// nothing of it.
	if open, _ := out["open"].([]engine.Check); len(open) > 0 && open[0].ID == "schema" {
		out["next"] = "Fix what is not valid first (the schema checks in open), with edit_draft on the fields they name."
	} else if w, err := c.o.Engine.Work(c.ctx, append([]engine.Ref{{Kind: kind, ID: id}}, also...), ""); err == nil {
		if len(w.Tasks) > 0 {
			out["next"] = firstNext(c.ctx, c.o.Engine, w.Tasks[0])
		} else {
			out["next"] = allMet(c.ctx, c.o.Engine)
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
			"The work is not finished until they are: settle each with your person, then %s.",
			unfinished, map[bool]string{true: "", false: "s"}[unfinished == 1], map[bool]string{true: "s", false: ""}[unfinished == 1], finish(c.ctx))
	}
	return out
}

// nextLine is one task as an agent reads it: what, where, and how.
// workspaceNext is where a workspace stands in the order of work, and the
// stage to write now.
func workspaceNext(ctx context.Context, e *engine.Engine) (any, error) {
	o, err := e.WorkspaceOrder(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"stages": o.Stages, "registers": o.Registers}
	if o.Next == "" {
		out["next"] = "Every stage a plan needs has a record. Ask your person what they want to define, and start with what it names."
		return out, nil
	}
	for _, st := range o.Stages {
		if st.Key != o.Next {
			continue
		}
		what := st.Kind
		if st.Level != "" {
			what += " at level " + st.Level
		}
		out["next"] = fmt.Sprintf("Next: the %s stage. Nothing after it can be written well until it has a record, because what comes later names it. "+
			"Call guide for %s and define one with your person, in the change set you are working in (start_work only when you have none open).", st.Key, what)
	}
	return out, nil
}

// allMet is what to do when nothing in the work named has an open check:
// the workspace's next stage while one is still unwritten; else the
// checks still open on the change set's other drafts; else the optional
// stages with no record yet; else propose.
func allMet(ctx context.Context, e *engine.Engine) string {
	// What is still open on the change set's other drafts comes before
	// any new stage: the work drafted so far is not finished.
	set := ""
	if w, ok := workingOn(ctx); ok {
		set = w.set
	} else if sets, err := e.ChangeSets(ctx, "open", false); err == nil && len(sets) > 0 {
		set = sets[0].ID
	}
	if set != "" {
		if open, err := e.OpenNow(ctx, set); err == nil {
			n := len(open)
			if n > 0 {
				return fmt.Sprintf("Nothing is open in this draft, but %d check%s still open on other drafts in this change set: "+
					"call next with work naming them, or checks without kind and id, and settle each before you go on.", n, map[bool]string{true: " is", false: "s are"}[n == 1])
			}
		}
	}
	o, err := e.WorkspaceOrder(ctx)
	if err == nil && o.Next != "" {
		stage, _ := workspaceNext(ctx, e)
		if m, ok := stage.(map[string]any); ok {
			return "Nothing in what you have drafted can be settled before this stage. " + fmt.Sprint(m["next"]) +
				" Once the work your person asked for is drafted, " + finish(ctx) + "."
		}
	}
	var optional []string
	for _, st := range o.Stages {
		if st.Optional && st.Count == 0 && st.State == "ready" {
			optional = append(optional, st.Key+" ("+st.Kind+")")
		}
	}
	if e.HasAny(ctx, "KPI") && !e.HasAny(ctx, "KPIReadings") {
		optional = append(optional, "a KPI's past readings (KPIReadings)")
	}
	if len(optional) > 0 {
		return "Every check across this work is met. Stages a plan may have and this workspace does not yet: " + strings.Join(optional, ", ") +
			". Port each the document gives, then " + finish(ctx) + "."
	}
	return "Every check across this work is met. Next, " + finish(ctx) + "."
}

// firstNext is the first task, unless it waits on a stage the workspace
// has not reached: a gap to close when no KPI exists yet to measure it.
// Then the stage comes first, and the task after it.
func firstNext(ctx context.Context, e *engine.Engine, t engine.Task) string {
	if t.By == "" {
		return nextLine(t)
	}
	o, err := e.WorkspaceOrder(ctx)
	if err != nil || o.Next == "" {
		return nextLine(t)
	}
	at := map[string]int{}
	for i, st := range o.Stages {
		if _, ok := at[st.Kind]; !ok {
			at[st.Kind] = i
		}
	}
	next, by := -1, -1
	for i, st := range o.Stages {
		if st.Key == o.Next {
			next = i
		}
	}
	if i, ok := at[t.By]; ok {
		by = i
	}
	if next < 0 || by < 0 || next >= by {
		return nextLine(t)
	}
	stage, _ := workspaceNext(ctx, e)
	m, _ := stage.(map[string]any)
	return fmt.Sprint(m["next"]) + " After it: " + nextLine(t)
}

func nextLine(t engine.Task) string {
	name := t.Name
	if name == "" {
		name = t.ID
	}
	line := fmt.Sprintf("Next (%s): %s %q (%s/%s), step %s: %s", t.Phase, t.Kind, name, t.Kind, t.ID, orNone(t.Step), t.Message)
	if t.Do != "" {
		line += " " + t.Do
	}
	line += " Where the documents do not give it, ask your person in their document's words, saying why you ask and offering the options you have."
	if len(t.Choices) > 0 {
		names := make([]string, 0, len(t.Choices))
		for _, c := range t.Choices {
			names = append(names, fmt.Sprintf("%s (%s)", c.Name, c.ID))
		}
		line += " Existing to choose from: " + strings.Join(names, "; ") + "."
	}
	return line
}

// guided is a guide as an agent reads it: the guide, then what to do with
// it, where a small model reads it last and remembers it best.
// draftEcho is the largest draft edit_draft repeats whole.
const draftEcho = 8000

type guided struct {
	engine.Guide
	// Prepare is what must exist before a kind nothing names (a project)
	// is written, from its flow: its plan is empty, since nothing waits
	// on it.
	Prepare json.RawMessage `json:"prepare,omitempty"`
	Next    []string        `json:"next"`
}

// withPrepare adds the flow's prepare list to a guide with no plan.
func withPrepare(e *engine.Engine, g engine.Guide) guided {
	out := guided{Guide: g, Next: nextSteps}
	if len(g.Plan) > 0 {
		return out
	}
	if raw, found, err := e.FlowJSON(g.Kind); err == nil && found {
		var flow struct {
			Spec struct {
				Prepare json.RawMessage `json:"prepare"`
			} `json:"spec"`
		}
		if json.Unmarshal(raw, &flow) == nil {
			out.Prepare = flow.Spec.Prepare
		}
	}
	return out
}

// nextSteps is the method, named by tool, at the end of every guide.
var nextSteps = []string{
	"If an existing record already says what your person wants, work on it (get it, then edit_draft your changes) instead of defining another: call match with what the new thing would say to find one.",
	"Follow plan in its order: its before items are what this kind names, which must exist first (ask its question with the existing records offered by name; " +
		"when none fits, define one with guide for its kind); then this kind's own steps; then its after items, each naming what came before it. " +
		"For an objective that means the objective, then its outcomes, the KPI measuring each, and the gaps they close. Never go back to a finished record to link it to a later one: the later one names it. " +
		"A before item with no existing record must be defined first, in this change set: a draft that names what does not exist yet is refused.",
	"Settle each step with your person, in order, using the field guides and their examples; never invent a figure, date, source or owner.",
	"Create each draft with save_draft as soon as you know its name, then edit_draft field by field; pass work (every Kind/id you are defining together) and meet every open check it returns, its own and around.",
	"Then propose the change set with propose. Your work is not done until it is proposed; your person accepts it in Cartograph, not in this conversation.",
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
