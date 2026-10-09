// Package document reads a document's text: its sections, by their
// headings, and the registers its tables hold. It is pure: it knows no
// store, no port and no kind, only text.
package document

import (
	"fmt"
	"regexp"
	"strings"
)

// Section is one section of a brought document.
type Section struct {
	ID      string `json:"id"`
	Heading string `json:"heading"`
	Words   int    `json:"words"`
	// Feeds are the fields it most likely answers, as JSON pointers on a
	// project, or "pieces" for the parts that name pieces of work.
	Feeds []string `json:"feeds,omitempty"`
	// Text is the section's words, kept apart from what it is listed
	// with.
	Text string `json:"-"`
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

// Split splits a document into sections at its headings. A heading with
// nothing under it (a contents line) is folded into the section before
// it.
func Split(title, text string) []Section {
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
	var out []Section
	for i, c := range cuts {
		to := len(lines)
		if i+1 < len(cuts) {
			to = cuts[i+1].from
		}
		body := strings.TrimSpace(strings.Join(lines[c.from:to], "\n"))
		words := len(strings.Fields(body))
		// A heading with nothing under it (a contents line) is folded into
		// the section before; one with even a sentence under it ("Budget:
		// to be confirmed") is a section of its own.
		under := 0
		if c.from+1 <= to {
			under = len(strings.Fields(strings.Join(lines[min(c.from+1, to):to], " ")))
		}
		if len(out) > 0 && under == 0 {
			prev := &out[len(out)-1]
			prev.Text += "\n" + body
			prev.Words += words
			continue
		}
		out = append(out, Section{Heading: c.heading, Words: words, Text: body})
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
