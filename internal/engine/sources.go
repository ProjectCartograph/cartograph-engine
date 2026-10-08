package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// A document an agent brings into a change set to port (docs/adr/0027):
// kept with the work, as one of its items, hidden from everything that
// reads the change set's drafts, and gone with the change set. It is
// split into sections by its headings, each marked with the fields it
// most likely answers, so an agent reads a section when it writes what
// the section gives, and never has to hold the whole document.

// sourceKind is the item kind a brought document is kept under. No
// manifest kind starts with an underscore, so it can never be merged.
const sourceKind = "_Source"

// SourceSection is one section of a brought document.
type SourceSection struct {
	ID      string `json:"id"`
	Heading string `json:"heading"`
	Words   int    `json:"words"`
	// Feeds are the fields it most likely answers, as JSON pointers on a
	// project, or "pieces" for the parts that name pieces of work.
	Feeds []string `json:"feeds,omitempty"`
	text  string
}

// Source is a brought document: its title and sections.
type Source struct {
	Title    string          `json:"title"`
	Sections []SourceSection `json:"sections"`
}

type storedSource struct {
	Title    string `json:"title"`
	Sections []struct {
		SourceSection
		Text string `json:"text"`
	} `json:"sections"`
}

// heading is a line that starts a section: "C1. Logic Model", "A. Scope",
// "2.3 Budget", "# Risks", "SECTION 4".
var heading = regexp.MustCompile(`^\s*(#{1,4}\s+\S.*|[A-Z][0-9]{0,2}\.\s+[A-Z].{2,80}|[0-9]{1,2}(\.[0-9]{1,2}){1,2}\.?\s+[A-Z][^.;,]{2,60}|[0-9]{1,2}\.\s+[A-Z][^.;,]{2,45}|(SECTION|Section|PART|Part)\s+[0-9A-Z]+.{0,80})\s*$`)

// feeds maps words a heading uses to the fields its section answers.
var feeds = []struct {
	words  []string
	fields []string
}{
	{[]string{"workstream", "implementation approach", "work breakdown", "components"}, []string{"pieces"}},
	{[]string{"scope", "deliverable", "output"}, []string{"pieces", "/spec/deliverables", "/spec/summary/scopeIn", "/spec/summary/scopeOut"}},
	{[]string{"identification", "authority", "mandate", "background", "document control"}, []string{"/spec/mandate", "/spec/summary/about"}},
	{[]string{"problem", "strategic case", "rationale", "context", "need"}, []string{"/spec/summary/problems", "/spec/summary/about"}},
	{[]string{"result", "objective", "outcome", "logic model", "aim", "goal"}, []string{"/spec/objectives", "/spec/alignment/goals"}},
	{[]string{"benefit", "success"}, []string{"/spec/successCriteria"}},
	{[]string{"governance", "role", "raci", "responsib", "team", "organisation", "organization"}, []string{"/spec/resources", "/spec/responsibilities", "/spec/escalationRoute", "/spec/team"}},
	{[]string{"schedule", "milestone", "timeline", "timetable", "phasing"}, []string{"/spec/milestones", "/spec/timeline"}},
	{[]string{"monitoring", "evaluation", "indicator", "kpi", "performance", "measure"}, []string{"/spec/kpis", "/spec/objectives"}},
	{[]string{"budget", "cost", "resource", "staffing", "funding", "finance"}, []string{"/spec/costs", "/spec/funding", "/spec/resources"}},
	{[]string{"procurement", "contract"}, []string{"/spec/procurement"}},
	{[]string{"risk", "issue", "dependenc", "assumption", "constraint"}, []string{"/spec/risks"}},
	{[]string{"stakeholder", "beneficiar", "communication", "engagement"}, []string{"/spec/summary/beneficiaries"}},
	{[]string{"legal", "regulatory", "safeguarding", "compliance", "data"}, []string{"/spec/compliance", "/spec/data"}},
	{[]string{"handover", "closure", "sustainab", "transition"}, []string{"/spec/operation", "/spec/successCriteria"}},
	{[]string{"decision", "condition", "sign-off", "sign off", "approval"}, []string{"/spec/conditions", "/spec/signOffs"}},
}

// SplitSource splits a document into sections at its headings. A section
// too short to hold anything (a contents line) is folded into the one
// before it.
func SplitSource(title, text string) []SourceSection {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	type cut struct {
		heading string
		from    int
	}
	cuts := []cut{{"Opening", 0}}
	for i, l := range lines {
		if heading.MatchString(l) {
			cuts = append(cuts, cut{strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "# ")), i})
		}
	}
	var out []SourceSection
	for i, c := range cuts {
		to := len(lines)
		if i+1 < len(cuts) {
			to = cuts[i+1].from
		}
		body := strings.TrimSpace(strings.Join(lines[c.from:to], "\n"))
		words := len(strings.Fields(body))
		if len(out) > 0 && words < 25 {
			prev := &out[len(out)-1]
			prev.text += "\n" + body
			prev.Words += words
			continue
		}
		out = append(out, SourceSection{Heading: c.heading, Words: words, text: body})
	}
	for i := range out {
		out[i].ID = fmt.Sprintf("s%d", i+1)
		h := strings.ToLower(out[i].Heading)
		seen := map[string]bool{}
		for _, f := range feeds {
			for _, w := range f.words {
				if strings.Contains(h, w) {
					for _, field := range f.fields {
						if !seen[field] {
							seen[field] = true
							out[i].Feeds = append(out[i].Feeds, field)
						}
					}
					break
				}
			}
		}
	}
	return out
}

// BringSource keeps a document in a change set, split into sections, and
// answers with its outline. A second document is kept beside the first.
func (e *Engine) BringSource(ctx context.Context, set, title, text string) (Source, error) {
	if strings.TrimSpace(text) == "" {
		return Source{}, fmt.Errorf("%w: the document has no text", ErrBadEdit)
	}
	s, err := e.rawChangeSetStore()
	if err != nil {
		return Source{}, err
	}
	cs, err := e.WorkingChangeSet(ctx, set)
	if err != nil {
		return Source{}, err
	}
	sections := SplitSource(title, text)
	stored := storedSource{Title: strings.TrimSpace(title)}
	for _, sec := range sections {
		stored.Sections = append(stored.Sections, struct {
			SourceSection
			Text string `json:"text"`
		}{sec, sec.text})
	}
	b, err := json.Marshal(stored)
	if err != nil {
		return Source{}, err
	}
	items, err := s.ListChangeItems(ctx, cs.ID)
	if err != nil {
		return Source{}, err
	}
	n := 1
	for _, it := range items {
		if it.Kind == sourceKind {
			n++
		}
	}
	if err := s.PutChangeItem(ctx, store.ChangeItem{Set: cs.ID, Kind: sourceKind, ID: fmt.Sprintf("document-%d", n), Text: b, Included: false}); err != nil {
		return Source{}, err
	}
	return Source{Title: stored.Title, Sections: sections}, nil
}

// Sources are the documents brought into a change set, with every
// section's text.
func (e *Engine) Sources(ctx context.Context, set string) ([]Source, error) {
	s, err := e.rawChangeSetStore()
	if err != nil {
		return nil, err
	}
	items, err := s.ListChangeItems(ctx, set)
	if err != nil {
		return nil, err
	}
	var out []Source
	for _, it := range items {
		if it.Kind != sourceKind {
			continue
		}
		var st storedSource
		if err := json.Unmarshal(it.Text, &st); err != nil {
			continue
		}
		src := Source{Title: st.Title}
		for _, sec := range st.Sections {
			sec.SourceSection.text = sec.Text
			src.Sections = append(src.Sections, sec.SourceSection)
		}
		out = append(out, src)
	}
	return out, nil
}

// SectionText is a section's text, for an agent to read.
type SectionText struct {
	ID, Heading, Text string
}

// ReadSections reads sections of the documents in a change set by id
// ("s4", or "2:s4" for the second document).
func (e *Engine) ReadSections(ctx context.Context, set string, ids []string) ([]SectionText, error) {
	srcs, err := e.Sources(ctx, set)
	if err != nil {
		return nil, err
	}
	if len(srcs) == 0 {
		return nil, fmt.Errorf("%w: no document has been brought into this change set: bring it with bring_document", ErrNotFound)
	}
	var out []SectionText
	for _, id := range ids {
		doc, sec := 0, id
		if d, rest, ok := strings.Cut(id, ":"); ok {
			fmt.Sscanf(d, "%d", &doc)
			doc--
			sec = rest
		}
		if doc < 0 || doc >= len(srcs) {
			return nil, fmt.Errorf("%w: no document %q", ErrNotFound, id)
		}
		found := false
		for _, s := range srcs[doc].Sections {
			if s.ID == sec {
				out = append(out, SectionText{ID: id, Heading: s.Heading, Text: s.text})
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: no section %q", ErrNotFound, id)
		}
	}
	return out, nil
}

// SectionsFor are the ids of the sections of a change set's documents
// most likely to answer a field.
func (e *Engine) SectionsFor(ctx context.Context, set, field string) []string {
	srcs, err := e.Sources(ctx, set)
	if err != nil {
		return nil
	}
	var out []string
	for d, src := range srcs {
		for _, s := range src.Sections {
			for _, f := range s.Feeds {
				if f == field || strings.HasPrefix(field, f+"/") || strings.HasPrefix(f, field+"/") {
					id := s.ID
					if d > 0 {
						id = fmt.Sprintf("%d:%s", d+1, s.ID)
					}
					out = append(out, id)
					break
				}
			}
		}
	}
	return out
}

// sourceHiding is the change set store with brought documents hidden from
// everything that reads drafts: views, checks, proposals and merges see
// only manifests.
type sourceHiding struct{ store.ChangeSetStore }

func (h sourceHiding) ListChangeItems(ctx context.Context, set string) ([]store.ChangeItem, error) {
	items, err := h.ChangeSetStore.ListChangeItems(ctx, set)
	if err != nil {
		return nil, err
	}
	out := items[:0:0]
	for _, it := range items {
		if it.Kind != sourceKind {
			out = append(out, it)
		}
	}
	return out, nil
}

func (h sourceHiding) GetChangeItem(ctx context.Context, set, kind, id string) (store.ChangeItem, bool, error) {
	if kind == sourceKind {
		return store.ChangeItem{}, false, nil
	}
	return h.ChangeSetStore.GetChangeItem(ctx, set, kind, id)
}

// rawChangeSetStore is the store with documents in view.
func (e *Engine) rawChangeSetStore() (store.ChangeSetStore, error) {
	cs, ok := e.manifests.(store.ChangeSetStore)
	if !ok {
		return nil, ErrNoChangeSets
	}
	return cs, nil
}

// RegisterOf reads the rows of a section's tables as items of a project's
// list field (milestones, deliverables, risks, the KPIs it names).
func (e *Engine) RegisterOf(ctx context.Context, set, section, field string) ([]map[string]any, error) {
	secs, err := e.ReadSections(ctx, set, []string{section})
	if err != nil {
		return nil, err
	}
	return RegisterItems(field, ReadRegister(secs[0].Text)), nil
}
