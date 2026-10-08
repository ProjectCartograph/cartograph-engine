package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/structure"
)

// The port chain: a document's text to its structure, its registers and every record written.

// structureDescription is the structure tool's description: the questions
// themselves, in order, so an agent answers them as a person would.
// structureExample is a whole structure call, the shape a small agent copies.
const structureExample = `{"pieces":[{"name":"Rollout","none":true},{"name":"Handbook","outputOf":"Rollout"},` +
	`{"name":"Baseline survey","changeOfItsOwn":true,"change":"sets the baseline the targets are set from","dependedOnBy":["Rollout"]},{"name":"Compliance checks","ongoing":true},` +
	`{"name":"Standards policy","policy":true},{"name":"Farm supply scheme","outOfScope":true}]}`

func structureDescription() string {
	var b strings.Builder
	b.WriteString("Call this first, before any draft, whenever you port a document or start something new (TAXONOMY.md D56). " +
		"What a document calls a project, a workstream or a programme is never trusted: list every piece of work it names " +
		"(the project itself, each workstream, sub-project, phase, survey, system, service, policy and partner strand), " +
		"then answer these questions for each piece, in order; the first yes decides:\n")
	for i, q := range structure.Questions {
		if q.Field == "" {
			fmt.Fprintf(&b, "%d. %s %s\n", i+1, q.Question, q.Then)
			continue
		}
		fmt.Fprintf(&b, "%d. %s (%s) %s\n", i+1, q.Question, q.Field, q.Then)
	}
	b.WriteString("Example: " + structureExample + ". Leave out every answer that is no. ")
	b.WriteString("A piece is a piece of work: the whole project, each workstream, phase or sub-project, each survey, system, service, policy or " +
		"scheme the documents name, one piece each. Milestones, risks, KPIs, objectives, costs and roles are not pieces: they go inside the records. ")
	b.WriteString("Give outputOf as the name of the piece it is an output of, and dependedOnBy as the names of the pieces that depend on it. " +
		"The answer says what each piece is, what it belongs to, and the order to write the records in; fix every problem it lists and " +
		"call it again until there are none, then open one change set and write the records in that order.")
	return b.String()
}

// portWrites are the tools that write a record one field or one check at
// a time: in a port, port writes every record at once instead.
var portWrites = map[string]bool{"edit_draft": true, "save_draft": true, "save_drafts": true, "settle": true, "leave_open": true}

// portOnly refuses a one-at-a-time write in a change set that holds a
// document: a port has one path, port with records, so an agent cannot
// wander off it into writes that leave the rest of the port unwritten.
func portOnly(c call, name string, in any) error {
	if !portWrites[name] {
		return nil
	}
	var args struct {
		ChangeSet string `json:"changeSet"`
	}
	if b, err := json.Marshal(in); err == nil {
		_ = json.Unmarshal(b, &args)
	}
	cs, cc, found, err := c.inChangeSet(args.ChangeSet, false)
	if err != nil || !found {
		return nil
	}
	if srcs, err := c.o.Engine.Sources(cc.ctx, cs.ID); err != nil || len(srcs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: change set %s is a port of a document: write its records with port and records, "+
		`[{"record": "Kind/id", "set": {"/spec/...": ...}, "unset": ["/spec/..."], "open": [{"check": "...", "reason": "..."}]}], every record in one call; `+
		"port answers what is still open on each. Call port with records now", engine.ErrBadEdit, cs.ID)
}

// piecesOf reads structure answers as an agent sent them, strictly: a
// piece sent as text, a key the questions do not ask or a kind that is
// not work is refused with what to send instead, never ignored.
func piecesOf(raws []any) ([]structure.Piece, map[string]any) {
	// A small agent sends names where pieces go: say how, by example,
	// rather than refusing in the words of a schema.
	pieces := make([]structure.Piece, 0, len(raws))
	var bad, problems []string
	for _, raw := range raws {
		b, _ := json.Marshal(raw)
		var p structure.Piece
		m, isObject := raw.(map[string]any)
		if !isObject || json.Unmarshal(b, &p) != nil {
			bad = append(bad, string(b))
			continue
		}
		// What a piece is, is the answer, never the agent's to say:
		// a key the questions do not ask is refused, not ignored.
		for k := range m {
			if !structure.Keys[k] {
				problems = append(problems, fmt.Sprintf("%q: %q is not an answer; give only name and the yes answers", p.Name, k))
			}
		}
		if k, _ := m["kind"].(string); k != "" && k != "Project" && k != "Programme" && k != "Portfolio" && k != "Operation" {
			problems = append(problems, fmt.Sprintf("%q is a %s, not a piece of work: leave it out, it is written inside the records", p.Name, k))
		}
		pieces = append(pieces, p)
	}
	if len(bad) > 0 {
		problems = append(problems, "each piece is an object with its name and its yes answers, not text: "+strings.Join(bad, ", "))
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, map[string]any{"problems": problems,
			"next": "Call structure again. A piece is a piece of work (the project, each workstream, phase, survey, system, service, policy or scheme), " +
				"each an object with name and only the answers that are yes, like " + structureExample + "."}
	}
	return pieces, nil
}

// onlySources reports whether a change set holds documents and nothing
// drafted yet.
func onlySources(view engine.ChangeSetView) bool {
	for _, it := range view.Items {
		if !strings.HasPrefix(it.Item.Kind, "_") {
			return false
		}
	}
	return true
}

// sectionsFor are the sections of the change set's documents that most
// likely answer a field, when a document was brought in.
func sectionsFor(c call, field string) []string {
	view, ok := engine.ChangeSetOf(c.ctx)
	if !ok {
		return nil
	}
	return c.o.Engine.SectionsFor(c.ctx, view, field)
}

// portPieces lays out a port's pieces (engine.PortPieces) and words the
// answer: the problems to fix, or the structure, the registers written,
// and every record's fill for the third call. port and start_work both
// end here, so a port comes out the same whichever an agent calls.
func portPieces(c call, set string, src engine.Source, raws []any) (any, error) {
	pieces, refused := piecesOf(raws)
	if refused != nil {
		return refused, nil
	}
	layout, err := c.o.Engine.PortPieces(c.ctx, set, src, pieces)
	if err != nil {
		return nil, err
	}
	if len(layout.Problems) > 0 {
		return map[string]any{"problems": layout.Problems, "next": "Fix every problem and call port again with the pieces; nothing was drafted."}, nil
	}
	for _, d := range layout.Drafts {
		c.announce(step{Step: "draft", Kind: d.Kind, ID: d.ID, Text: d.Text, ChangeSet: set})
	}
	main := ""
	for _, p := range layout.Structure.Pieces {
		if p.Kind == "Project" && len(p.Of) == 0 {
			main, _, _ = strings.Cut(p.Record, "/")
			break
		}
	}
	for i := range layout.Registers {
		layout.Registers[i].Refused = taught(c, main, layout.Registers[i].Refused)
	}
	return map[string]any{"changeSet": set, "work": layout.Structure.Work, "pieces": layout.Structure.Pieces, "registers": layout.Registers, "records": layout.Records,
		"next": "The structure is drafted and the registers written. Now call port a third time with records: every record listed here, " +
			"each with set (every field in its fill, written from the sections read names) and open (each check the document does not answer, with its reason). " +
			"One call for all of them; then propose; report from work_summary."}, nil
}

// portRecords writes every record of a port (engine.PortRecords) and
// words the answer: what each kept and left, and what is still open.
func portRecords(c call, set string, records []engine.PortRecord) (any, error) {
	results, still, err := c.o.Engine.PortRecords(c.ctx, set, records)
	if err != nil {
		return nil, err
	}
	for i, r := range results {
		kind, _, _ := strings.Cut(r.Record, "/")
		results[i].Refused = taught(c, kind, r.Refused)
		if r.Err != nil {
			results[i].Error = withFields(c, kind, r.Err).Error()
		}
	}
	out := map[string]any{"changeSet": set, "records": results}
	if len(still) > 0 {
		out["stillOpen"] = still
		out["next"] = "Call port again with records for what is still open: set it from the document, or open it with the reason the document does not say it. " +
			"Refused fields are sent again in the shape each says. Then propose."
	} else {
		out["next"] = "Every record is settled. Propose the change set with propose; report from work_summary."
	}
	return out, nil
}
