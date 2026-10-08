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
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace"
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
	// Trace records every tool call by its shape (docs/adr/0028); nil
	// records none.
	Trace trace.Recorder
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

const instructions = `Porting a document? Do exactly this, in one pass:
1. port with the document's title, its text straight from its file and
   fileSize, the file's size in bytes: have a script build the call
   (os.path.getsize for the size), never
   retype, summarise or read the document whole. It answers with the
   sections that name pieces of work.
2. read_section those sections; list every piece of work they name
   (the whole, each survey, system, service, policy or scheme; never a
   workstream), answer the structure questions for each, and call port
   again with the pieces. It drafts every record and writes every
   register (milestones, deliverables, risks, indicators) for you.
3. port a third time with records: every record its answer lists,
   each with set (every field in its fill, in the shape each shows,
   read from the sections each names under read) and open (only the
   checks the document does not answer, each with its reason). One
   call for every record; it answers what is still open, and you call
   it again for that until nothing is.
4. propose. Report from work_summary only.
Use no other tool unless an answer tells you to.

Starting new work, not from a document: structure, then start_work with
the pieces, then settle each record, then propose.

A shape the discipline refuses (a second objective on a project, a
person's name on a role, a field the schema does not have) is refused
when you save it, and nothing is saved: fix it and save again.

Cartograph is the organisation's record of what it has decided to do: its
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

A long document does not fit in your context with the work: never read
it whole. Read its contents list and the parts that name the pieces of
work first, for structure; then read each section only when you write
what it gives, and let it go.

Work this way, every time:
- Structure first. Before you draft anything, list every piece of work
  the documents or your person name, answer the structure questions for
  each (the structure tool lists them), and call structure. Never take a
  document's own word for what a piece is: a "project" may be a
  programme, a "workstream" a deliverable or a project of its own, a
  "programme" a standing service. Write the records in the order
  structure returns, in one change set, in one pass.
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
	// On the record's presence document only when the step was outside a
	// change set: work in a change set is its person's, and is followed on
	// the feed (docs/adr/0018).
	if st.Kind != "" && st.ID != "" && st.ChangeSet == "" {
		if docID, err := sh.RecordPresence(c.ctx, st.Kind, st.ID); err == nil {
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
	// What a document states is written, or left open with the person's
	// own answer through leave_open; never waived in passing here.
	for rec, checks := range waive {
		for check := range checks {
			if writtenFromTheDocument[check] {
				return nil, fmt.Errorf("%w: %s on %s is what the document itself says: write it with settle, or leave it open with leave_open "+
					"saying what your person answered", engine.ErrBadEdit, check, rec)
			}
		}
	}
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
		start := time.Now()
		c, err := o.begin(ctx, person, req)
		if err != nil {
			o.record(t.Name, req, c, in, nil, err, start)
			return failed(err), nil, nil
		}
		if err := portOnly(c, t.Name, in); err != nil {
			o.record(t.Name, req, c, in, nil, err, start)
			return failed(err), nil, nil
		}
		out, err := h(c, in)
		o.record(t.Name, req, c, in, out, err, start)
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

func newServer(o Options, person identity.Principal) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "cartograph", Version: o.Version}, &sdk.ServerOptions{Instructions: instructions})

	registerReadTools(s, o, person)
	registerDraftTools(s, o, person)
	registerPortTools(s, o, person)
	registerProposeTools(s, o, person)
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
