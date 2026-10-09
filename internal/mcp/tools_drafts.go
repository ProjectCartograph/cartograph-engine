package mcp

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/structure"
	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerDraftTools adds the tools that draft: a change set's records, a field or a check at a time.
func registerDraftTools(s *sdk.Server, o Options, person identity.Principal) {
	e := o.Engine

	tool(s, o, person, &sdk.Tool{Name: "structure", Description: structureDescription(), Annotations: readOnly},
		func(_ call, in structureIn) (any, error) {
			pieces, refused := piecesOf(in.Pieces)
			if refused != nil {
				return refused, nil
			}
			return structure.Classify(pieces), nil
		})

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
			var refused []engine.Problem
			var created []string
			if err := e.SaveInChangeSet(c.ctx, cs.ID, in.Kind, in.ID, text); err != nil {
				var invalid *engine.ValidationError
				if !errors.As(err, &invalid) || !schemaOnly(invalid.Problems) {
					return nil, withFields(c, in.Kind, err)
				}
				// Refused whole, it is built field by field as every agent
				// write is: names found or drafted, formats read as meant,
				// every field that can be kept kept.
				meta, _ := in.Manifest["metadata"].(map[string]any)
				name, _ := meta["name"].(string)
				start := map[string]any{"apiVersion": "cartograph/v1", "kind": in.Kind, "metadata": map[string]any{"id": in.ID, "name": name}, "spec": map[string]any{}}
				first, _ := e.Codec().Encode(start)
				if err := e.SaveInChangeSet(c.ctx, cs.ID, in.Kind, in.ID, first); err != nil {
					return nil, withFields(c, in.Kind, err)
				}
				spec, _ := in.Manifest["spec"].(map[string]any)
				put := map[string]any{}
				for k, v := range spec {
					put["/spec/"+k] = v
				}
				if refused, created, err = applyFields(c, cs.ID, in.Kind, in.ID, put, nil); err != nil {
					return nil, err
				}
				text, _, _ = e.ChangeSetText(c.ctx, cs.ID, in.Kind, in.ID)
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
			if len(refused) > 0 {
				out["refused"] = refused
				out["fix"] = "Every other field was kept. Send the refused ones again with settle, in the shape each says."
			}
			if len(created) > 0 {
				out["drafted"] = created
			}
			return out, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "save_drafts", Description: "Create several manifests in your change set in one call, as save_draft does each: " +
		"for writing a whole piece of work quickly, such as every outcome, KPI or register entry a document gives. Each manifest carries its kind and metadata.id. " +
		"Answers with what each saved, its problems and open checks, and with work, what to do next across it.", Annotations: drafting},
		func(c call, in saveManyIn) (any, error) {
			if len(in.Manifests) == 0 {
				return nil, fmt.Errorf("give at least one manifest")
			}
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			var saved []map[string]any
			for _, m := range in.Manifests {
				kind, _ := m["kind"].(string)
				meta, _ := m["metadata"].(map[string]any)
				id, _ := meta["id"].(string)
				if kind == "" || id == "" {
					saved = append(saved, map[string]any{"error": "a manifest needs kind and metadata.id"})
					continue
				}
				text, err := e.Codec().Encode(m)
				if err == nil {
					if _, drafted, _ := e.ChangeSetText(c.ctx, cs.ID, kind, id); drafted {
						err = fmt.Errorf("already drafted in this change set: change its fields with edit_draft")
					} else {
						err = e.SaveInChangeSet(c.ctx, cs.ID, kind, id, text)
					}
				}
				if err != nil {
					saved = append(saved, map[string]any{"record": kind + "/" + id, "error": err.Error()})
					continue
				}
				c.announce(step{Step: "draft", Kind: kind, ID: id, Text: text, ChangeSet: cs.ID})
				saved = append(saved, map[string]any{"record": kind + "/" + id})
			}
			if c.ctx, err = e.InChangeSet(c.ctx, cs.ID); err != nil {
				return nil, err
			}
			for _, sv := range saved {
				r, _ := sv["record"].(string)
				kind, id, ok := strings.Cut(r, "/")
				if !ok || sv["error"] != nil {
					continue
				}
				problems, _ := e.DraftProblems(c.ctx, kind, id)
				checks, _ := e.DraftChecks(c.ctx, kind, id)
				open := 0
				for _, ch := range checks {
					if ch.Open() {
						open++
					}
				}
				sv["saved"], sv["openChecks"] = "draft", open
				if len(problems) > 0 {
					sv["saved"], sv["problems"] = "draft, not valid yet: fix each problem with edit_draft", problems
				}
			}
			out := map[string]any{"changeSet": cs.ID, "saved": saved}
			if len(in.Work) > 0 {
				next, err := nextOf(c, cs.ID, true, in.Work, "")
				if err != nil {
					return nil, err
				}
				out["then"] = next
			}
			return out, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "next", Description: "What to do next, one thing at a time. With work (the records of this piece of work as Kind/id, in the order structure gave), " +
		"the first one not written yet, or else the next open check across them; call it after every save until it says every check is met. " +
		"The record is a directed acyclic graph, written from the top down " +
		"(purpose, goals, objectives, outcomes, KPIs, gaps, then portfolios, programmes, operations and projects), each thing naming only what comes before it. " +
		"With work (every manifest you are working on), the next open check across it: each manifest is finished in one visit, what it is, its numbers " +
		"(from the documents your person gave you), then its links to what is already there, before the next one down. Without work, the stage of the workspace " +
		"to write now, and how far each has got: an empty workspace starts at its purpose. Call it whenever you are unsure what comes next.", Annotations: readOnly},
		func(c call, in nextIn) (any, error) {
			cs, c, found, err := c.inChangeSet(in.ChangeSet, false)
			if err != nil {
				return nil, err
			}
			return nextOf(c, cs.ID, found, in.Work, in.Locale)
		})

	tool(s, o, person, &sdk.Tool{Name: "round", Description: "The next round of questions for your person, for work (every record of this piece of work as Kind/id), " +
		"computed by Cartograph from the order of the work: every decision whose prerequisites are settled, and nothing that hangs on an answer " +
		"not given yet. settle is what the documents state, yours to write; ask is your person's decisions, each with the records that could " +
		"answer it and the one recommended; write is records to write next. Ask the whole of ask at once, as how says, save the answers, and " +
		"call round again until it says to confirm with your person. Use it whenever your person is there to answer.", Annotations: readOnly},
		func(c call, in nextIn) (any, error) {
			cs, c, found, err := c.inChangeSet(in.ChangeSet, false)
			if err != nil {
				return nil, err
			}
			return roundOf(c, cs.ID, found, in.Work, in.Locale)
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
			refused, created, err := applyFields(c, cs.ID, in.Kind, in.ID, in.Set, in.Unset)
			if err != nil {
				return nil, err
			}
			text, _, _ := e.ChangeSetText(c.ctx, cs.ID, in.Kind, in.ID)
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
			if len(refused) > 0 {
				out["refused"] = refused
				out["fix"] = "Every other field was kept. Send the refused ones again, in the shape each says."
			}
			if len(created) > 0 {
				out["drafted"] = created
			}
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

	// settle reads its fields wherever a small agent puts them: under set,
	// under fields, or as JSON pointers beside kind and id. The shape of
	// the call is not the record's; the record's shape is held strictly.
	settleSchema, _ := jsonschema.For[settleIn](nil)
	settleSchema.AdditionalProperties = &jsonschema.Schema{}
	tool(s, o, person, &sdk.Tool{Name: "settle", Description: "Settle one record in one call: set every field the documents give (by JSON pointer, under set), " +
		"leave open each check they do not answer with its reason, and get what comes next across the work, the next record's checks or draft included. " +
		"Porting a document? Write every record at once with port and records instead; settle is for one record at a time. " +
		`Example: {"kind":"Project","id":"project-1a2b","set":{"/spec/summary/about":"...","/spec/objectives/0/objective":"..."},` +
		`"open":[{"check":"aim-mandate","reason":"No mandate is named"}],"asked":"not available","work":["Project/project-1a2b"]}.`, Annotations: drafting, InputSchema: settleSchema},
		func(c call, raw map[string]any) (any, error) {
			in, err := settleOf(raw)
			if err != nil {
				return nil, err
			}
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			refused, created, err := applyFields(c, cs.ID, in.Kind, in.ID, in.Set, in.Unset)
			if err != nil {
				return nil, err
			}
			// The fields are kept whatever happens to the checks left open:
			// a check that cannot be left is said, never the call refused.
			var left, notLeft []string
			for _, o := range in.Open {
				switch {
				case strings.TrimSpace(in.Asked) == "":
					notLeft = append(notLeft, o.Check+`: pass asked, what you asked your person and what they answered, or "not available" when you were told to work without them`)
				case strings.TrimSpace(o.Reason) == "":
					notLeft = append(notLeft, o.Check+": give the reason your person will read")
				default:
					if err := engine.MayLeave(o.Check, in.Asked); err != nil {
						notLeft = append(notLeft, err.Error())
					} else if err := e.LeaveOpen(c.ctx, cs.ID, in.Kind, in.ID, o.Check, o.Reason, in.Asked, false); err != nil {
						notLeft = append(notLeft, o.Check+": "+err.Error())
					} else {
						left = append(left, o.Check)
					}
				}
			}
			// What it decided for its person, kept apart for them to review.
			if err := e.Assume(c.ctx, cs.ID, in.Kind, in.ID, in.Assumed); err != nil {
				notLeft = append(notLeft, "assumed: "+err.Error())
			}
			if c.ctx, err = e.InChangeSet(c.ctx, cs.ID); err != nil {
				return nil, err
			}
			problems, err := e.DraftProblems(c.ctx, in.Kind, in.ID)
			if err != nil {
				return nil, err
			}
			out := map[string]any{"changeSet": cs.ID, "record": in.Kind + "/" + in.ID, "left": left}
			if len(notLeft) > 0 {
				out["notLeft"] = notLeft
			}
			var drafted, cut []string
			for _, c := range created {
				if note, ok := strings.CutPrefix(c, "cut: "); ok {
					cut = append(cut, note)
				} else if why, ok := strings.CutPrefix(c, "refused: "); ok {
					refused = append(refused, engine.Problem{Message: why})
				} else {
					drafted = append(drafted, c)
				}
			}
			if len(drafted) > 0 {
				out["drafted"] = drafted
			}
			if len(cut) > 0 {
				out["cut"] = cut
			}
			if len(refused) > 0 {
				out["refused"] = refused
				out["fix"] = "Every other field was kept. Send the refused ones again, in the shape each says, with the next settle of this record."
			}
			if len(problems) > 0 {
				out["notValidYet"] = problems
			}
			work := in.Work
			if len(work) == 0 {
				work = []string{in.Kind + "/" + in.ID}
			}
			next, err := nextOf(c, cs.ID, true, work, "")
			if err != nil {
				return nil, err
			}
			out["then"] = next
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
				if strings.TrimSpace(in.Reason) != "" {
					if err := engine.MayLeave(it.Check, in.Asked); err != nil {
						return nil, err
					}
				}
				if err := e.LeaveOpen(c.ctx, cs.ID, it.Kind, it.ID, it.Check, in.Reason, in.Asked, in.Correct); err != nil {
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
		"Everything you draft afterwards goes into it, apart from your other work and every other agent's, and your person reviews and accepts it whole. " +
		"Pass pieces, the same answers you gave structure, and it also writes the first draft of every record the structure names (each named, with its components, " +
		"its deliverables and its scope-out lines) and says what to fill in first.", Annotations: drafting},
		func(c call, in startWorkIn) (any, error) {
			var st structure.Result
			if len(in.Pieces) > 0 {
				pieces, refused := piecesOf(in.Pieces)
				if refused != nil {
					return refused, nil
				}
				if st = structure.Classify(pieces); len(st.Problems) > 0 {
					return map[string]any{"problems": st.Problems, "next": "Fix every problem and call start_work again with the pieces; nothing was started."}, nil
				}
			}
			// A document already brought into the agent's open change set,
			// with nothing drafted beside it yet, makes this that port: the
			// work goes on in it, not in a new change set without the
			// document.
			if len(in.Pieces) > 0 {
				if open, oc, found, err := c.inChangeSet("", false); err == nil && found {
					if srcs, err := e.Sources(oc.ctx, open.ID); err == nil && len(srcs) > 0 {
						if view, err := e.ViewChangeSet(oc.ctx, open.ID); err == nil && onlySources(view) {
							return portPieces(oc, open.ID, srcs[0], in.Pieces)
						}
					}
				}
			}
			cs, err := e.StartChangeSet(c.ctx, in.Title, in.Description)
			if err != nil {
				return nil, err
			}
			// A document brought into it makes this a port: the same chain
			// as port's, registers and all.
			if len(in.Pieces) > 0 {
				if srcs, err := e.Sources(c.ctx, cs.ID); err == nil && len(srcs) > 0 {
					return portPieces(c, cs.ID, srcs[0], in.Pieces)
				}
			}
			out := map[string]any{"changeSet": cs.ID, "title": cs.Title, "next": "Draft into it with save_draft and edit_draft; propose it with propose when every check is met."}
			if len(in.Pieces) == 0 {
				out["first"] = "For a port or a new piece of work, call structure, then start_work again with the same pieces: it drafts every record for you."
				return out, nil
			}
			// The structure's records, written as first drafts: the agent fills
			// them in, never lays them out.
			for _, d := range st.Drafts() {
				kind, _ := d["kind"].(string)
				id, _ := d["metadata"].(map[string]any)["id"].(string)
				text, err := e.Codec().Encode(d)
				if err != nil {
					return nil, err
				}
				if err := e.SaveInChangeSet(c.ctx, cs.ID, kind, id, text); err != nil {
					return nil, err
				}
				c.announce(step{Step: "draft", Kind: kind, ID: id, Text: text, ChangeSet: cs.ID})
			}
			if c.ctx, err = e.InChangeSet(c.ctx, cs.ID); err != nil {
				return nil, err
			}
			next, err := nextOf(c, cs.ID, true, st.Work, "")
			if err != nil {
				return nil, err
			}
			out["work"], out["written"], out["then"] = st.Work, st.Pieces, next
			out["next"] = "Every record the structure names is drafted. Pass work, exactly as here, to every save and edit; each answer says what to fill in next, " +
				"until every check is met. Then propose."
			return out, nil
		})
}
