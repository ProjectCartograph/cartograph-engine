package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// What comes next: the checks, fills and steps an answer hands the agent.

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
	// The work as planned, and the part of it written so far: only what
	// is written can be checked.
	var planned, also []engine.Ref
	for _, w := range work {
		if k, i, ok := strings.Cut(w, "/"); ok && k != "" && i != "" && (k != kind || i != id) {
			r := engine.Ref{Kind: k, ID: i}
			planned = append(planned, r)
			if _, err := c.o.Engine.Get(c.ctx, k, i); err == nil {
				also = append(also, r)
			}
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
			withFill(c, out, w.Tasks)
		} else {
			out["next"] = allMet(c.ctx, c.o.Engine)
		}
	}
	// This record finished, the next one the work plans is handed over
	// in the same answer, so a whole port takes a call a record.
	if open, _ := out["open"].([]engine.Check); len(open) == 0 {
		if h, err := firstUnwritten(c, "", false, planned, ""); err == nil && h != nil {
			out["next"], out["write"], out["draft"] = h["next"], h["write"], h["draft"]
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

// nextOf is what to do next in a piece of work: the first record of it
// not written yet, else the next open check across it, else that every
// check is met; without work, the stage of the workspace to write now.
func nextOf(c call, set string, found bool, workIn []string, locale string) (any, error) {
	e := c.o.Engine
	if found && len(workIn) == 0 {
		view, err := e.ViewChangeSet(c.ctx, set)
		if err != nil {
			return nil, err
		}
		for _, it := range view.Items {
			workIn = append(workIn, it.Item.Kind+"/"+it.Item.ID)
		}
	}
	var work []engine.Ref
	for _, w := range workIn {
		if k, i, ok := strings.Cut(w, "/"); ok && k != "" && i != "" {
			work = append(work, engine.Ref{Kind: k, ID: i})
		}
	}
	if len(work) == 0 {
		out, err := workspaceNext(c.ctx, e)
		if m, ok := out.(map[string]any); ok {
			// Asked with no work at all: a port or a new piece of work
			// starts at its structure, not at the workspace's next stage.
			m["first"] = "Porting a document or starting a piece of work? Call structure first with every piece of work it names, then start_work " +
				"with the same pieces: it drafts every record, named and linked, and hands you the work list. Do not write records one by one from here."
		}
		return out, err
	}
	if out, err := firstUnwritten(c, set, found, work, locale); out != nil || err != nil {
		return out, err
	}
	w, err := e.Work(c.ctx, work, locale)
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
	withFill(c, out, w.Tasks)
	return out, nil
}

// withFill puts every open check on the first task's record in out, so the
// record is settled in one visit: one edit_draft with every field the
// documents give, one leave_open for each they do not. A check at a time
// costs a small agent its room.
func withFill(c call, out map[string]any, tasks []engine.Task) {
	if len(tasks) == 0 {
		return
	}
	first := tasks[0]
	var fill []map[string]any
	shaped := map[string]bool{}
	for _, t := range tasks {
		if t.Kind != first.Kind || t.ID != first.ID {
			continue
		}
		f := map[string]any{"check": t.Check, "do": t.Do, "now": t.Message}
		if t.Field != "" {
			f["field"] = t.Field
			if read := sectionsFor(c, t.Field); len(read) > 0 {
				f["read"] = read
			}
			// What goes there, when it is a list or an object, so the whole
			// of it is written in one settle without reading the guide;
			// once a field.
			if sh, ok := c.o.Engine.FieldShapeAt(t.Kind, t.Field); ok && !shaped[t.Field] {
				shaped[t.Field] = true
				switch sh.Example.(type) {
				case []any, map[string]any:
					f["shape"] = sh
				}
			}
		}
		if t.By != "" {
			f["writeFirst"] = t.By
		}
		fill = append(fill, f)
	}
	if len(fill) > 1 {
		out["fill"] = fill
		delete(out, "then")
		// A port is in full when every list the documents give is written,
		// checked or not: the record's other lists, with their shapes.
		also := map[string]any{}
		for _, f := range c.o.Engine.ListFields(first.Kind) {
			if shaped[f] {
				continue
			}
			if sh, ok := c.o.Engine.FieldShapeAt(first.Kind, f); ok {
				if read := sectionsFor(c, f); len(read) > 0 {
					also[f] = map[string]any{"example": sh.Example, "optional": sh.Optional, "read": read}
				} else {
					also[f] = sh
				}
			}
		}
		if len(also) > 0 {
			out["alsoFromTheDocuments"] = also
		}
		out["next"] = fmt.Sprintf("Settle %s/%s now, every check in fill at once, with one settle call: set every field the documents give, "+
			"open each check they do not answer with its reason, and pass the work list. %s", first.Kind, first.ID, firstNext(c.ctx, c.o.Engine, first))
	}
}

// firstUnwritten hands over the first record the work names that is not
// written yet, in the order given (the structure's order), with its
// starting draft; nil when every one is written. The work is a plan,
// written one record at a time.
func firstUnwritten(c call, set string, found bool, work []engine.Ref, locale string) (map[string]any, error) {
	// A record the work names that is not drafted yet is the next
	// thing to write, in the order given (the structure's order):
	// the work is a plan, written one record at a time.
	e := c.o.Engine
	var drafted []engine.Ref
	for _, w := range work {
		var err error
		if found {
			_, _, err = e.ChangeSetText(c.ctx, set, w.Kind, w.ID)
		} else {
			_, err = e.Get(c.ctx, w.Kind, w.ID)
		}
		if errors.Is(err, engine.ErrNotFound) || errors.Is(err, engine.ErrUnknownKind) {
			written := len(drafted)
			out := map[string]any{
				"next": fmt.Sprintf("Write %s/%s now (%d of %d in the work are written): call save_draft with kind %s, id %s, the same work, "+
					"and manifest set to the draft here, its name and every field you can filled from the documents; its answer says what comes next.", w.Kind, w.ID, written, len(work), w.Kind, w.ID),
				"write": w.Kind + "/" + w.ID,
			}
			if g, gerr := e.Guide(c.ctx, w.Kind, "", locale); gerr == nil && g.Template != nil {
				draft := g.Template
				if meta, ok := draft["metadata"].(map[string]any); ok {
					meta["id"] = w.ID
				}
				out["draft"] = draft
			}
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		drafted = append(drafted, w)
	}
	return nil, nil
}

// containsFold reports whether s holds sub, ignoring case.
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(strings.TrimSpace(sub)))
}
