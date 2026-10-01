package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Who is addressing which part of a gap.
//
// A gap enumerates the slices its shortfall was observed in; work that
// cites it names which of those slices it reaches. Put together across a
// vault, that answers the question a gap register exists to answer and
// could not be asked before: what is nobody working on? (GAP_DESIGN.md.)
//
// The word throughout is *addressed*, never *closed*. Coverage says
// somebody is working on a slice, which is contribution. Whether the
// shortfall actually narrowed is what the gap's measure reads, and the two
// are worth keeping apart: an evaluation literature's worth of trouble
// comes from letting one stand for the other.

// GapClaim is one piece of work addressing one gap.
type GapClaim struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
	// Segments of the gap this work reaches. Empty means the whole gap,
	// which is what an unqualified citation says.
	Segments []string `json:"segments,omitempty"`
	// Whole is true when the citation named no segments.
	Whole bool `json:"whole"`
}

// GapSegmentCoverage is one of a gap's segments and who is addressing it.
type GapSegmentCoverage struct {
	Segment   string     `json:"segment"`
	Name      string     `json:"name"`
	Addressed []GapClaim `json:"addressedBy,omitempty"`
}

// GapCoverage is a gap's scope and what reaches each part of it.
type GapCoverage struct {
	Gap string `json:"gap"`
	// Segments in the gap's own order, each with the work addressing it.
	Segments []GapSegmentCoverage `json:"segments"`
	// Claims on the gap as a whole: citations that named no segments.
	Whole []GapClaim `json:"whole,omitempty"`
}

// citingKinds are the kinds whose problems can cite a gap.
var citingKinds = []string{"Project", "Programme"}

// problemsOf returns a manifest's problems, wherever that kind keeps them.
func problemsOf(kind string, spec map[string]any) []any {
	switch kind {
	case "Project":
		summary, _ := spec["summary"].(map[string]any)
		out, _ := summary["problems"].([]any)
		return out
	case "Programme":
		out, _ := spec["problems"].([]any)
		return out
	}
	return nil
}

// GapCoverageFor reads one gap's scope and everything that cites it.
func (e *Engine) GapCoverageFor(ctx context.Context, id string) (GapCoverage, error) {
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}

	gaps, err := l.Documents("Gap")
	if err != nil {
		return GapCoverage{}, err
	}
	doc, found := gaps[id]
	if !found {
		return GapCoverage{}, fmt.Errorf("%w: Gap/%s", ErrNotFound, id)
	}
	spec, _ := doc["spec"].(map[string]any)
	out := GapCoverage{Gap: id}

	segmentNames := map[string]string{}
	if segs, err := l.Documents("Segment"); err == nil {
		for segID, segDoc := range segs {
			segmentNames[segID] = docName(segDoc, segID)
		}
	}

	byID := map[string]int{}
	list, _ := spec["segments"].([]any)
	for _, s := range list {
		segID, _ := s.(string)
		if segID == "" {
			continue
		}
		name := segmentNames[segID]
		if name == "" {
			name = segID
		}
		byID[segID] = len(out.Segments)
		out.Segments = append(out.Segments, GapSegmentCoverage{Segment: segID, Name: name})
	}

	for _, kind := range citingKinds {
		docs, err := l.Documents(kind)
		if err != nil {
			return GapCoverage{}, err
		}
		ids := make([]string, 0, len(docs))
		for docID := range docs {
			ids = append(ids, docID)
		}
		// Sorted, so two runs report the same work in the same order.
		sort.Strings(ids)

		for _, docID := range ids {
			citer := docs[docID]
			citerSpec, _ := citer["spec"].(map[string]any)
			if citerSpec == nil {
				continue
			}
			named, whole := citedSegments(problemsOf(kind, citerSpec), id)
			if !whole && len(named) == 0 {
				continue
			}
			claim := GapClaim{Kind: kind, ID: docID, Name: docName(citer, docID), Whole: whole}
			if whole {
				out.Whole = append(out.Whole, claim)
				continue
			}
			sort.Strings(named)
			claim.Segments = named
			for _, segID := range named {
				if at, ok := byID[segID]; ok {
					out.Segments[at].Addressed = append(out.Segments[at].Addressed, claim)
				}
			}
		}
	}
	return out, nil
}

// citedSegments reads one manifest's problems for citations of one gap:
// the segments they name, and whether any citation claimed the whole.
func citedSegments(problems []any, gapID string) (named []string, whole bool) {
	seen := map[string]bool{}
	for _, p := range problems {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		cites, _ := pm["gaps"].([]any)
		for _, c := range cites {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if id, _ := cm["gap"].(string); id != gapID {
				continue
			}
			segs, _ := cm["segments"].([]any)
			if len(segs) == 0 {
				whole = true
				continue
			}
			for _, s := range segs {
				if segID, _ := s.(string); segID != "" && !seen[segID] {
					seen[segID] = true
					named = append(named, segID)
				}
			}
		}
	}
	return named, whole
}

// GapChecks says whether a gap can be used for what a gap register is for.
//
// All advisory, and none of them blocks: every one reads a manifest other
// than this gap, so a gap that saved yesterday must not be refused today
// because somebody edited a project.
func (e *Engine) GapChecks(ctx context.Context, id string) ([]ProgrammeCheck, error) {
	v, found, err := e.manifests.GetCurrent(ctx, "Gap", id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: Gap/%s", ErrNotFound, id)
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		return nil, fmt.Errorf("parse Gap/%s: %w", id, err)
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		spec = map[string]any{}
	}

	var out []ProgrammeCheck
	add := func(checkID, section, state, message string) {
		out = append(out, ProgrammeCheck{ID: checkID, Section: section, State: state, Message: message})
	}

	// A gap is the distance between a current and a desired state
	// (Kaufman). Both can be written as a sentence, or both come from a KPI
	// (its baseline and target). Without the desired state it is an
	// observation, not a gap.
	text := func(k string) bool { s, _ := spec[k].(string); return strings.TrimSpace(s) != "" }
	measured := text("measure")
	switch {
	case measured || (text("current") && text("desired")):
		add("gap-states", "shortfall", programmeCheckOK, "Current and desired states are stated.")
	case !text("current"):
		add("gap-states", "shortfall", programmeCheckWarn, "No current state yet.")
	default:
		add("gap-states", "shortfall", programmeCheckWarn, "No desired state yet.")
	}
	// The outcome that would close it: where the desired state is reached
	// (D24). Without one, nothing says what this gap is a gap in.
	if outs, _ := spec["outcomes"].([]any); len(outs) > 0 {
		add("gap-outcome", "shortfall", programmeCheckOK, "Linked to the outcome that would close it.")
	} else {
		add("gap-outcome", "shortfall", programmeCheckWarn, "Not linked to an outcome yet.")
	}
	if measured {
		add("gap-measured", "shortfall", programmeCheckOK, "An indicator tracks this gap.")
	} else {
		add("gap-measured", "shortfall", programmeCheckWarn, "No indicator tracks this gap yet.")
	}

	if source, _ := spec["source"].(string); strings.TrimSpace(source) != "" {
		add("gap-source", "evidence", programmeCheckOK, "Source named.")
	} else {
		add("gap-source", "evidence", programmeCheckWarn, "No source yet.")
	}

	coverage, err := e.GapCoverageFor(ctx, id)
	if err != nil {
		return nil, err
	}
	segs, _ := spec["segments"].([]any)
	if len(segs) == 0 {
		add("gap-segments", "scope", programmeCheckWarn, "No segments named yet.")
	} else {
		var unaddressed []string
		for _, s := range coverage.Segments {
			if len(s.Addressed) == 0 {
				unaddressed = append(unaddressed, s.Name)
			}
		}
		switch {
		case len(coverage.Whole) > 0 && len(unaddressed) > 0:
			// Somebody claimed the whole gap while parts of it have
			// nobody: the claim is what is hiding them.
			add("gap-covered", "scope", programmeCheckWarn, fmt.Sprintf(
				"%d of %d segments have nobody, and %d citation%s claims the whole gap without naming segments.",
				len(unaddressed), len(coverage.Segments), len(coverage.Whole), plural(len(coverage.Whole))))
		case len(unaddressed) == len(coverage.Segments):
			add("gap-covered", "scope", programmeCheckWarn, "Nothing is working on any part of this yet.")
		case len(unaddressed) > 0:
			add("gap-covered", "scope", programmeCheckWarn, fmt.Sprintf(
				"Nobody is addressing %s.", englishList(unaddressed)))
		default:
			add("gap-covered", "scope", programmeCheckOK, fmt.Sprintf(
				"Every one of %d segments is addressed by somebody.", len(coverage.Segments)))
		}
	}
	return out, nil
}

// englishList joins names the way a sentence would, so a check reads as
// one rather than as a comma-separated dump.
func englishList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " or " + names[1]
	}
	if len(names) > 4 {
		return fmt.Sprintf("%s and %d others", strings.Join(names[:3], ", "), len(names)-3)
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}
