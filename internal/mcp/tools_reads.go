package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerReadTools adds the tools that read: the workspace, its records, their checks and links, and the work in hand.
func registerReadTools(s *sdk.Server, o Options, person identity.Principal) {
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

	tool(s, o, person, &sdk.Tool{Name: "get", Description: "Manifests as they stand for you: your change set's draft of each, where you have one, else the record, as YAML. " +
		"Give kind and id for one, or records (each Kind/id) for every one you need in a single call.", Annotations: readOnly},
		func(c call, in getIn) (any, error) {
			cs, c, found, err := c.inChangeSet(in.ChangeSet, false)
			if err != nil {
				return nil, err
			}
			// Many at once: one call, never one a record (a read repeated
			// per record is a port's largest waste).
			if len(in.Records) > 0 {
				if len(in.Records) > 100 {
					return nil, fmt.Errorf("%w: at most 100 records a call", engine.ErrBadEdit)
				}
				items := make([]map[string]any, 0, len(in.Records))
				for _, r := range in.Records {
					kind, id, ok := strings.Cut(r, "/")
					if !ok {
						items = append(items, map[string]any{"record": r, "error": "want Kind/id"})
						continue
					}
					item := map[string]any{"record": r}
					if found {
						text, inSet, err := e.ChangeSetText(c.ctx, cs.ID, kind, id)
						if err != nil {
							item["error"] = err.Error()
						} else {
							item["yaml"], item["inChangeSet"] = string(text), inSet
						}
					} else if v, err := e.Get(c.ctx, kind, id); err != nil {
						item["error"] = err.Error()
					} else {
						item["yaml"], item["version"] = string(v.YAML), v.Number
					}
					items = append(items, item)
				}
				out := map[string]any{"items": items}
				if found {
					out["changeSet"] = cs.ID
				}
				return out, nil
			}
			if in.Kind == "" || in.ID == "" {
				return nil, fmt.Errorf("%w: give kind and id, or records", engine.ErrBadEdit)
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

	tool(s, o, person, &sdk.Tool{Name: "semantic_layer", Description: "The KPIs with a metric, and the data sources they measure from, written as a semantic layer " +
		"(TAXONOMY.md D57) for an analytics engineer: dbt's semantic models and metrics. Notes say what the record leaves to defaults, " +
		"such as a data source with no semantic model. Ask it when your person wants their KPIs built in the warehouse.", Annotations: readOnly},
		func(c call, in semanticIn) (any, error) {
			c = c.reading(in.ChangeSet)
			format := in.Format
			if format == "" {
				if fs := e.SemanticFormats(); len(fs) > 0 {
					format = fs[0]
				}
			}
			files, notes, err := e.ExportSemantic(c.ctx, format)
			if err != nil {
				return nil, err
			}
			out := make([]map[string]string, len(files))
			for i, f := range files {
				out[i] = map[string]string{"path": f.Path, "content": string(f.Content)}
			}
			return map[string]any{"format": format, "files": out, "notes": notes}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "dmaic", Description: "Whether a project can be taken through DMAIC, Lean Six Sigma's Define, Measure, Analyze, Improve and Control " +
		"(TAXONOMY.md D58): each phase's deliverables, which the project holds, where each is held and what is lacking; and for a KPI, its readings " +
		"as a control chart with its limits, the readings that signal a change and its capability against its specification limits. " +
		"Ask it when your person wants to improve a process, or to know whether a project is ready to be.", Annotations: readOnly},
		func(c call, in manifestRef) (any, error) {
			c = c.reading(in.ChangeSet)
			if in.Kind == "KPI" {
				return e.ControlChartOf(c.ctx, in.ID)
			}
			return e.DMAICOf(c.ctx, in.ID)
		})

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

	tool(s, o, person, &sdk.Tool{Name: "waits", Description: "What a project's dated items wait on, across kinds, as your change set reads it (TAXONOMY.md D47, D48): " +
		"its milestones, deliverables, conditions, purchases and dependencies on other projects, and KPI targets or baselines set when one of them happens, " +
		"each with its month, what it follows, the risks that could move it, and whether it is on the chain that decides the last date (critical), " +
		"no longer fits its own date or what it needs (conflict), or late. edges run from what comes first to what waits on it, by index. " +
		"Use it to tell your person what a date depends on, or which item a slip would move.", Annotations: readOnly},
		func(c call, in manifestRef) (any, error) {
			c = c.reading(in.ChangeSet)
			g, err := e.Waits(c.ctx, in.ID)
			if err != nil {
				return nil, err
			}
			// Where it sits on a canvas is the interface's, not the agent's.
			for i := range g.Nodes {
				g.Nodes[i].X, g.Nodes[i].Y = 0, 0
			}
			return g, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "what_happened", Description: "When your person tells you something happened to a project (a risk occurred, a delivery slipped, a milestone was reached), " +
		"the items of the project it is about, likeliest first, ranked by the decision model, each with what can be recorded as happening to it (TAXONOMY.md D59). " +
		"Offer the likeliest and let your person confirm; without a model (available false) every item is listed: ask which. " +
		"Then affects says what it reaches, and you record the event, and any that follow from it with cause, in one change set.", Annotations: readOnly},
		func(c call, in happenedIn) (any, error) {
			c = c.reading(in.ChangeSet)
			return e.WhatHappened(c.ctx, in.Project, in.Text)
		})

	tool(s, o, person, &sdk.Tool{Name: "affects", Description: "Every item a trigger on one of a project's items reaches (TAXONOMY.md D59): what waits on it, directly or through others, " +
		"and for a risk every item whose timing names it, each with its month and what it follows. Tell your person what it reaches; " +
		"what changes is theirs to decide, recorded as events with cause naming the trigger, or as changes, in their change set. Nothing moves by itself.", Annotations: readOnly},
		func(c call, in affectsIn) (any, error) {
			c = c.reading(in.ChangeSet)
			nodes, err := e.Affects(c.ctx, in.Project, in.Item)
			if err != nil {
				return nil, err
			}
			for i := range nodes {
				nodes[i].X, nodes[i].Y = 0, 0
			}
			return map[string]any{"reaches": nodes}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "match", Description: "Before defining anything, the existing records of a kind that already say what it would say, most likely first: " +
		"judged by Cartograph's decision model; without one, only a record with the same name or its initials (by says which). Work on a match instead of defining another.", Annotations: readOnly},
		func(c call, in matchIn) (any, error) {
			c = c.reading("")
			return map[string]any{"matches": e.MatchExisting(c.ctx, in.Kind, in.Level, in.Text)}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "decision_model", Description: "Call first, once a session: whether Cartograph has a decision model configured and answering now, whatever backs it. " +
		"When it is ready, relevant, match and understand rank by meaning, and the checks judge meaning; when it is not, off lists what is not judged, which you must then judge yourself, reading the registers in full.", Annotations: readOnly},
		func(c call, _ struct{}) (any, error) {
			return e.DecisionModel(c.ctx), nil
		})

	tool(s, o, person, &sdk.Tool{Name: "relevant", Description: "What in the workspace is relevant to a piece of work: the likeliest few records of each kind, likeliest first, " +
		"ranked by the decision model; without one, nothing (available says which). Call it with what the work is about before choosing what a draft names, " +
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

	tool(s, o, person, &sdk.Tool{Name: "taxonomy", Description: "Every kind Cartograph keeps, in the order of the strategy: what each is in one sentence, its levels, " +
		"and what plans and documents often call it instead. Read it before recording anything from a document, and map each thing the document says " +
		"onto the kind it is by definition, whatever the document calls it. Its porting map says where each part of a document goes; " +
		"ask with part for how to write the parts you are porting now.", Annotations: readOnly},
		func(c call, in taxonomyIn) (any, error) {
			t, err := e.Taxonomy(in.Locale)
			if err != nil {
				return nil, err
			}
			out := map[string]any{"kinds": t, "rule": "Map by what a thing is, against each summary and the guide's definition, never by the word a document uses. " +
				"Record it as Cartograph's kind and level, keep the document's wording in its statement, and cite the document as its source. " +
				"porting says where each part of a charter or plan goes, and what stays out."}
			// The whole map is long: a small agent's context is better spent
			// on the document. Where each part goes, by default; how, for
			// the parts asked about.
			switch {
			case in.Full:
				out["porting"] = porting
			case strings.TrimSpace(in.Part) != "":
				var hit []portingRule
				for _, r := range porting {
					if containsFold(r.Part+" "+r.Goes, in.Part) {
						hit = append(hit, r)
					}
				}
				out["porting"] = hit
				delete(out, "kinds")
			default:
				brief := make([]map[string]string, len(porting))
				for i, r := range porting {
					brief[i] = map[string]string{"part": r.Part, "goes": r.Goes}
				}
				out["porting"] = brief
				out["how"] = "Call taxonomy with part set to a few words of a part (such as milestone, budget or workstream) for how to write it, before you write it."
			}
			return out, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "work_summary", Description: "What your change set really holds, record by record: each record's name, how many objectives, " +
		"deliverables, milestones, risks, components and key results it has, its checks still open, and each check left for your person with what they were asked. " +
		"Report to your person from this answer only: never say a record holds what it does not show.", Annotations: readOnly},
		func(c call, in readingIn) (any, error) {
			cs, c, found, err := c.inChangeSet(in.ChangeSet, false)
			if err != nil {
				return nil, err
			}
			if !found {
				return map[string]any{"records": []any{}, "said": "You have no change set yet: nothing is drafted."}, nil
			}
			view, err := e.ViewChangeSet(c.ctx, cs.ID)
			if err != nil {
				return nil, err
			}
			leftBy := map[string]int{}
			for _, w := range view.ChangeSet.Waivers {
				leftBy[w.On]++
			}
			var records []map[string]any
			for _, it := range view.Items {
				var doc map[string]any
				_ = e.Codec().DecodeInto(it.Item.Text, &doc)
				spec, _ := doc["spec"].(map[string]any)
				r := map[string]any{"record": it.Item.Kind + "/" + it.Item.ID, "name": it.Name}
				for _, f := range []string{"objectives", "deliverables", "milestones", "risks", "components", "keyResults", "costs", "kpis"} {
					if l, ok := spec[f].([]any); ok && len(l) > 0 {
						r[f] = len(l)
					}
				}
				open := 0
				for _, ch := range it.Checks {
					if ch.Open() {
						open++
					}
				}
				r["openChecks"], r["leftForPerson"] = open, leftBy[it.Item.Kind+"/"+it.Item.ID]
				records = append(records, r)
			}
			// What is left for the person, with what they were asked, for
			// the report: a check left without asking them shows here.
			left := make([]map[string]any, len(view.ChangeSet.Waivers))
			for i, w := range view.ChangeSet.Waivers {
				left[i] = map[string]any{"on": w.On, "check": w.Check, "reason": w.Reason, "asked": w.Asked}
			}
			assumed := assumedOut(view.ChangeSet.Assumptions)
			out := map[string]any{"changeSet": cs.ID, "status": view.ChangeSet.Status, "records": records, "leftForYourPerson": left, "decidedForYourPerson": assumed,
				"said": "This is all the change set holds. Report from it only."}
			// While it is open, what the person confirms is named, for
			// propose to carry.
			// The ask travels with the token: an agent that reads out only
			// the token still reads it (run 005 printed confirm alone, and
			// proposed without asking).
			if token, err := e.ConfirmToken(c.ctx, cs.ID); err == nil {
				out["confirm"] = map[string]string{"token": token,
					"first": "Show your person this summary and ask them to confirm it says what they meant. End your turn and wait for their yes; " +
						"then propose with confirm set to this token. Any change after this needs a new summary, and their confirmation of it."}
			}
			return out, nil
		})
}
