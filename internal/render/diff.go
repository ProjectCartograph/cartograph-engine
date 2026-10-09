package render

import (
	"context"
	"errors"
	"html"
	"regexp"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// A charter's diff: the charter a change set leaves a project with,
// against the record, part by part and line by line, so a person reviewing
// the change set reads what it changes in the document it adds up to.
// The diff is the engine's, so every interface draws the same one.

// Part states and line ops in a charter's diff.
const (
	DiffSame    = "same"
	DiffAdded   = "added"
	DiffRemoved = "removed"
	DiffChanged = "changed"
)

// PartDiff is one part of a charter against the record.
type PartDiff struct {
	Title string `json:"title"`
	Step  string `json:"step,omitempty"`
	// State is same, added, removed or changed.
	State string     `json:"state"`
	Lines []DiffLine `json:"lines"`
}

// DiffLine is one line of a part's text: same, added or removed.
type DiffLine struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

// CharterDiff is project id's charter as the change set on changed reads
// it, against the record on record. A project new in the change set is
// added whole.
func CharterDiff(record, changed context.Context, e *engine.Engine, id string) ([]PartDiff, error) {
	after, err := CharterParts(changed, e, id)
	if err != nil {
		return nil, err
	}
	before, err := CharterParts(record, e, id)
	if err != nil && !errors.Is(err, engine.ErrNotFound) {
		return nil, err
	}
	return diffParts(before, after), nil
}

// diffParts matches parts by title and step, in the order of the charter
// as changed, with any part only the record has kept where it stood.
func diffParts(before, after []Part) []PartDiff {
	key := func(p Part) string { return p.Step + "\x00" + p.Title }
	was := map[string]Part{}
	for _, p := range before {
		if !p.Empty {
			was[key(p)] = p
		}
	}
	now := map[string]bool{}
	for _, p := range after {
		if !p.Empty {
			now[key(p)] = true
		}
	}
	var out []PartDiff
	removed := func(p Part) PartDiff {
		return PartDiff{Title: p.Title, Step: p.Step, State: DiffRemoved, Lines: opLines(DiffRemoved, textLines(p.HTML))}
	}
	bi := 0
	for _, p := range after {
		// Parts the record had before this one, and the change set drops.
		for bi < len(before) && !now[key(before[bi])] {
			if !before[bi].Empty {
				out = append(out, removed(before[bi]))
			}
			bi++
		}
		if bi < len(before) && key(before[bi]) == key(p) {
			bi++
		}
		if p.Empty {
			continue
		}
		old, had := was[key(p)]
		lines := diffLines(textLines(old.HTML), textLines(p.HTML))
		state := DiffSame
		switch {
		case !had:
			state = DiffAdded
		default:
			for _, l := range lines {
				if l.Op != DiffSame {
					state = DiffChanged
					break
				}
			}
		}
		out = append(out, PartDiff{Title: p.Title, Step: p.Step, State: state, Lines: lines})
	}
	for ; bi < len(before); bi++ {
		if !before[bi].Empty && !now[key(before[bi])] {
			out = append(out, removed(before[bi]))
		}
	}
	return out
}

var (
	blockEnd = regexp.MustCompile(`(?i)</(p|li|dd|dt|tr|h[1-6]|div|caption)>|<br\s*/?>`)
	cellEnd  = regexp.MustCompile(`(?i)</t[dh]>`)
	anyTag   = regexp.MustCompile(`<[^>]*>`)
	spaces   = regexp.MustCompile(`[ \t]+`)
)

// textLines is a part's HTML as the lines a person reads: one a
// paragraph, list item, term, value or table row, its cells apart.
func textLines(h string) []string {
	h = cellEnd.ReplaceAllString(h, " · ")
	h = blockEnd.ReplaceAllString(h, "\n")
	h = html.UnescapeString(anyTag.ReplaceAllString(h, ""))
	var out []string
	for _, l := range strings.Split(h, "\n") {
		l = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(spaces.ReplaceAllString(l, " ")), "·"))
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func opLines(op string, lines []string) []DiffLine {
	out := make([]DiffLine, len(lines))
	for i, l := range lines {
		out[i] = DiffLine{Op: op, Text: l}
	}
	return out
}

// diffLines is the longest common subsequence of a and b, each line
// outside it removed (from a) or added (from b), removals first.
func diffLines(a, b []string) []DiffLine {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	out := []DiffLine{}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, DiffLine{Op: DiffSame, Text: a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, DiffLine{Op: DiffRemoved, Text: a[i]})
			i++
		default:
			out = append(out, DiffLine{Op: DiffAdded, Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{Op: DiffRemoved, Text: a[i]})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{Op: DiffAdded, Text: b[j]})
	}
	return out
}
