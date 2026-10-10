package render

import (
	"fmt"
	"math"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// The triple constraint in the charter (TAXONOMY.md D60): scope,
// schedule and cost as a triangle, each corner sized by how much
// weighted risk it carries and drawn by the stance the project takes on
// it, and a table of what each side carries. Every mark is said in
// words as well, so nothing rests on colour.

var sideTitle = map[string]string{"scope": "Scope", "schedule": "Schedule", "cost": "Cost"}

var stanceWord = map[string]string{
	engine.StanceHold:    "held",
	engine.StanceAdjust:  "adjusted first",
	engine.StanceConcede: "conceded",
}

// constraintsBrief prints the triangle, when the project has said
// anything about it: a stance, or a risk placed on a side.
func (d *doc) constraintsBrief(spec map[string]any) {
	tri := engine.ConstraintsOf(spec)
	said := false
	for _, s := range tri.Sides {
		said = said || s.Stance != "" || len(s.Risks) > 0
	}
	if !said {
		return
	}
	d.h2("Scope, schedule and cost")
	d.raw(triangleSVG(tri))

	desc := map[string]string{}
	for _, r := range list(spec["risks"]) {
		desc[str(r["id"])] = str(r["description"])
	}
	var rows [][]string
	for _, s := range tri.Sides {
		stance := stanceWord[s.Stance]
		if stance == "" {
			stance = "not said"
		}
		var carried []string
		for i, r := range s.Risks {
			if i == 3 {
				carried = append(carried, fmt.Sprintf("and %d more", len(s.Risks)-3))
				break
			}
			carried = append(carried, strings.TrimRight(desc[r.ID], ". "))
		}
		exposure := "none"
		if s.Exposure > 0 {
			exposure = fmt.Sprintf("%d (%.0f%%)", s.Exposure, s.Share*100)
		}
		rows = append(rows, []string{sideTitle[s.Constraint], stance, exposure, strings.Join(carried, "; ")})
	}
	d.table([]string{"Side", "When something gives", "Weighted risk", "Risks it carries"}, rows)
	if mc := tri.MostConstrained; mc != "" {
		for _, s := range tri.Sides {
			if s.Constraint == mc {
				note := fmt.Sprintf("%s carries the most weighted risk.", sideTitle[mc])
				if s.Stance == engine.StanceHold {
					note += " It is also the side held: its risks need responses that spend another side."
				}
				d.note(note)
			}
		}
	}
	if n := len(tri.Unplaced); n > 0 {
		d.note(fmt.Sprintf("%d risk%s and issues name no side yet.", n, plural(n)))
	}
}

// triangleSVG draws the three corners: scope at the top, schedule and
// cost below. A corner's circle grows with its share of the weighted
// risk; a held corner is filled, an adjusted one half, a conceded one
// left open; the most constrained is ringed.
func triangleSVG(tri engine.Constraints) string {
	at := map[string][2]float64{"scope": {240, 88}, "schedule": {120, 270}, "cost": {360, 270}}
	var b strings.Builder
	fmt.Fprintf(&b, `<figure class="diagram"><svg class="triangle" viewBox="0 0 480 360" width="100%%" role="img" aria-label="%s">`,
		esc("Scope, schedule and cost, each sized by the weighted risk it carries"))
	fmt.Fprintf(&b, `<path class="tri-edge" d="M%.0f,%.0f L%.0f,%.0f L%.0f,%.0f Z"/>`,
		at["scope"][0], at["scope"][1], at["schedule"][0], at["schedule"][1], at["cost"][0], at["cost"][1])
	for _, s := range tri.Sides {
		p := at[s.Constraint]
		r := 12 + 30*math.Sqrt(s.Share)
		cls := "tri-node " + s.Stance
		if s.Constraint == tri.MostConstrained {
			cls += " most"
		}
		fmt.Fprintf(&b, `<g class="%s"><circle cx="%.1f" cy="%.1f" r="%.1f"/>`, strings.TrimSpace(cls), p[0], p[1], r)
		stance := stanceWord[s.Stance]
		if stance == "" {
			stance = "stance not said"
		}
		ly := p[1] + r + 18
		if s.Constraint == "scope" {
			ly = p[1] - r - 20
		}
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" text-anchor="middle">%s</text>`, p[0], ly, esc(sideTitle[s.Constraint]))
		tag := fmt.Sprintf("%s · %d", stance, s.Exposure)
		if s.Constraint == tri.MostConstrained {
			tag += " · most constrained"
		}
		fmt.Fprintf(&b, `<text class="tag" x="%.1f" y="%.1f" text-anchor="middle">%s</text></g>`, p[0], ly+14, esc(tag))
	}
	b.WriteString(`</svg><figcaption>Each corner grows with the weighted risk it carries: likelihood times impact on that side. A filled corner is held, a half-filled one is adjusted first, an open one is conceded; the ring marks the most constrained.</figcaption></figure>`)
	return b.String()
}

const triangleCSS = `
figure.diagram svg.triangle { max-width: 480px; margin: 0 auto; }
.tri-edge { fill: none; stroke: var(--line); stroke-width: 1.5; }
.tri-node circle { fill: var(--paper); stroke: var(--muted); stroke-width: 1.5; }
.tri-node.hold circle { fill: var(--fg); fill-opacity: .8; stroke: var(--fg); }
.tri-node.adjust circle { fill: var(--accent); fill-opacity: .35; stroke: var(--accent); }
.tri-node.concede circle { fill: var(--paper); stroke: var(--muted); stroke-dasharray: 4 3; }
.tri-node.most circle { stroke: var(--warn); stroke-width: 4; }
.tri-node text { fill: var(--fg); font-size: 13px; font-weight: 600; }
.tri-node text.tag { fill: var(--muted); font-size: 11px; font-weight: 400; }
`

// movesOf says, per risk, which sides it would move and where: "Schedule
// (high, Pilot)".
func movesOf(spec map[string]any) map[string]string {
	names := map[string]string{}
	for _, l := range []string{"deliverables", "milestones"} {
		for _, it := range list(spec[l]) {
			names[str(it["id"])] = str(it["name"])
		}
	}
	for _, it := range list(spec["costs"]) {
		name := str(it["basis"])
		if name == "" {
			name = str(it["category"])
		}
		names[str(it["id"])] = name
	}
	out := map[string]string{}
	for _, s := range engine.ConstraintsOf(spec).Sides {
		for _, r := range s.Risks {
			var detail []string
			if r.Impact != "" {
				detail = append(detail, r.Impact)
			}
			if n := names[r.On]; n != "" {
				detail = append(detail, n)
			}
			m := sideTitle[s.Constraint]
			if len(detail) > 0 {
				m += " (" + strings.Join(detail, ", ") + ")"
			}
			if out[r.ID] != "" {
				out[r.ID] += "; "
			}
			out[r.ID] += m
		}
	}
	return out
}

var responseWord = map[string]string{"avoid": "Avoid", "mitigate": "Mitigate", "transfer": "Transfer", "accept": "Accept"}

// responseOf is a risk's response: the strategy, then what is done.
func responseOf(r map[string]any) string {
	word, what := responseWord[str(r["response"])], str(r["mitigation"])
	switch {
	case word != "" && what != "":
		return word + ": " + what
	case word != "":
		return word
	}
	return what
}
