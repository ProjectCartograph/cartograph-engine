package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/document"
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

// Source is a brought document: its title and sections.
type Source struct {
	Title    string             `json:"title"`
	Sections []document.Section `json:"sections"`
}

type storedSource struct {
	Title    string `json:"title"`
	Sections []struct {
		document.Section
		Text string `json:"text"`
	} `json:"sections"`
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
	sections := document.Split(title, text)
	stored := storedSource{Title: strings.TrimSpace(title)}
	for _, sec := range sections {
		stored.Sections = append(stored.Sections, struct {
			document.Section
			Text string `json:"text"`
		}{sec, sec.Text})
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
			sec.Section.Text = sec.Text
			src.Sections = append(src.Sections, sec.Section)
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
				out = append(out, SectionText{ID: id, Heading: s.Heading, Text: s.Text})
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
	return document.RegisterItems(field, document.ReadRegister(secs[0].Text)), nil
}
