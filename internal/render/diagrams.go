package render

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// Diagrams drawn into a charter as SVG, so the document carries its shape
// on paper as well as on screen: the work a piece of work depends on, and
// its milestones on time. Each is drawn from what the engine resolves;
// nothing here decides anything.

// wrap splits a name into at most two lines of about width characters.
func wrap(s string, width int) []string {
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for _, w := range words {
		if cur == "" {
			cur = w
			continue
		}
		if len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
			if len(lines) == 2 {
				break
			}
			continue
		}
		cur += " " + w
	}
	if cur != "" && len(lines) < 2 {
		lines = append(lines, cur)
	}
	if len(lines) == 2 {
		joined := strings.Join(lines, " ")
		if len(joined) < len(s) {
			l := lines[1]
			if len(l) > width-1 {
				l = l[:width-1]
			}
			lines[1] = strings.TrimRight(l, " ,;:") + "…"
		}
	}
	return lines
}

// dependencyDiagram draws the work connected to focus: what it depends on
// at any depth below it, and what depends on it above, each piece of work
// a box, each line running down from the work to what it depends on. The
// critical path is drawn heavier; the work most depended on and any loop
// are marked in words in the box.
func dependencyDiagram(n names, g engine.ComponentGraph, focus engine.Ref) string {
	down := map[engine.Ref][]engine.Ref{}
	up := map[engine.Ref][]engine.Ref{}
	for _, e := range g.Edges {
		down[e.From] = append(down[e.From], e.To)
		up[e.To] = append(up[e.To], e.From)
	}
	keep := map[engine.Ref]bool{focus: true}
	var walk func(r engine.Ref, next map[engine.Ref][]engine.Ref)
	walk = func(r engine.Ref, next map[engine.Ref][]engine.Ref) {
		for _, x := range next[r] {
			if !keep[x] {
				keep[x] = true
				walk(x, next)
			}
		}
	}
	walk(focus, down)
	walk(focus, up)
	if len(keep) < 2 {
		return ""
	}
	nodes := map[engine.Ref]engine.ComponentNode{}
	for _, nd := range g.Nodes {
		nodes[nd.Ref] = nd
	}
	// Rows: a piece of work one row below the deepest work that lists it.
	row := map[engine.Ref]int{}
	for r := range keep {
		row[r] = 0
	}
	for pass := 0; pass < len(keep); pass++ {
		moved := false
		for _, e := range g.Edges {
			if keep[e.From] && keep[e.To] && row[e.From]+1 > row[e.To] && row[e.From]+1 < len(keep) {
				row[e.To] = row[e.From] + 1
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	var rows [][]engine.Ref
	for r := range keep {
		for len(rows) <= row[r] {
			rows = append(rows, nil)
		}
		rows[row[r]] = append(rows[row[r]], r)
	}
	pos := map[engine.Ref]float64{}
	for i := range rows {
		sort.Slice(rows[i], func(a, b int) bool {
			pa, pb := parentPos(up[rows[i][a]], pos), parentPos(up[rows[i][b]], pos)
			if pa != pb {
				return pa < pb
			}
			return n.of(rows[i][a].Kind, rows[i][a].ID) < n.of(rows[i][b].Kind, rows[i][b].ID)
		})
		for j, r := range rows[i] {
			pos[r] = float64(j) - float64(len(rows[i])-1)/2
		}
	}
	const W, H, GX, GY, width = 196.0, 52.0, 18.0, 46.0, 860.0
	widest := 1
	for _, r := range rows {
		widest = max(widest, len(r))
	}
	scale := 1.0
	if need := float64(widest)*(W+GX) - GX; need > width {
		scale = width / need
	}
	at := map[engine.Ref][2]float64{}
	for i, list := range rows {
		total := float64(len(list))*(W+GX) - GX
		left := (width/scale - total) / 2
		for j, r := range list {
			at[r] = [2]float64{left + float64(j)*(W+GX), float64(i)*(H+GY) + 8}
		}
	}
	height := (float64(len(rows))*(H+GY) - GY + 16) * scale
	critical := map[[2]engine.Ref]bool{}
	for i := 1; i < len(g.CriticalPath); i++ {
		critical[[2]engine.Ref{g.CriticalPath[i-1], g.CriticalPath[i]}] = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<figure class="diagram"><svg viewBox="0 0 %.0f %.0f" width="100%%" role="img" aria-label="%s"><g transform="scale(%.3f)">`, width, height, esc("The work this depends on, and what depends on it"), scale)
	b.WriteString(`<defs><marker id="dep-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerUnits="userSpaceOnUse" markerWidth="9" markerHeight="9" orient="auto-start-reverse"><path d="M0 0 8 4 0 8Z" fill="currentColor"/></marker></defs>`)
	for _, e := range g.Edges {
		if !keep[e.From] || !keep[e.To] {
			continue
		}
		p, q := at[e.From], at[e.To]
		x1, y1, x2, y2 := p[0]+W/2, p[1]+H, q[0]+W/2, q[1]
		cls := "dep-edge"
		if critical[[2]engine.Ref{e.From, e.To}] {
			cls += " critical"
		}
		if nodes[e.From].InLoop && nodes[e.To].InLoop {
			cls += " loop"
		}
		fmt.Fprintf(&b, `<path class="%s" d="M%.1f,%.1f C%.1f,%.1f %.1f,%.1f %.1f,%.1f" marker-end="url(#dep-arrow)"/>`, cls, x1, y1, x1, (y1+y2)/2, x2, (y1+y2)/2, x2, y2-2)
	}
	for r := range keep {
		p := at[r]
		nd := nodes[r]
		cls := "dep-node"
		if r == focus {
			cls += " focus"
		}
		if nd.Critical {
			cls += " critical"
		}
		if r.Kind == "Programme" {
			cls += " programme"
		}
		if nd.InLoop {
			cls += " loop"
		}
		fmt.Fprintf(&b, `<g class="%s"><rect x="%.1f" y="%.1f" width="%.0f" height="%.0f" rx="7"/>`, cls, p[0], p[1], W, H)
		lines := wrap(n.of(r.Kind, r.ID), 30)
		for i, l := range lines {
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f">%s</text>`, p[0]+10, p[1]+18+float64(i)*15, esc(l))
		}
		var tags []string
		if r.Kind == "Programme" {
			tags = append(tags, "Programme")
		}
		if nd.Months > 0 {
			tags = append(tags, fmt.Sprintf("%d months", nd.Months))
		}
		if nd.MostDependedOn {
			tags = append(tags, "most depended on")
		}
		if nd.InLoop {
			tags = append(tags, "in a loop")
		}
		if len(tags) > 0 && len(lines) < 2 {
			fmt.Fprintf(&b, `<text class="tag" x="%.1f" y="%.1f">%s</text>`, p[0]+10, p[1]+40, esc(strings.Join(tags, " · ")))
		}
		b.WriteString(`</g>`)
	}
	b.WriteString(`</g></svg><figcaption>Lines run from each piece of work down to what it depends on. The heavier line is the critical path: the chain that runs longest on the calendar.</figcaption></figure>`)
	return b.String()
}

func parentPos(parents []engine.Ref, pos map[engine.Ref]float64) float64 {
	if len(parents) == 0 {
		return 0
	}
	t := 0.0
	for _, p := range parents {
		t += pos[p]
	}
	return t / float64(len(parents))
}

func monthNum(m string) (int, bool) {
	t, err := time.Parse("2006-01", m)
	if err != nil {
		return 0, false
	}
	return t.Year()*12 + int(t.Month()) - 1, true
}

// scheduleChart draws milestones on time (TAXONOMY.md D48): a diamond
// where each falls, a band for a window, a hatched run to the month a
// milestone set once something happens is expected by, a line from what
// each waits on, the chain that sets the last date heavier.
func scheduleChart(items []engine.ScheduleItem, labels map[string]string) string {
	if len(items) < 2 {
		return ""
	}
	lo, hi := 1<<30, -1
	for _, it := range items {
		for _, m := range []string{it.Month, it.NotBefore, it.NotAfter} {
			if v, ok := monthNum(m); ok {
				lo, hi = min(lo, v), max(hi, v)
			}
		}
	}
	if hi < 0 {
		return ""
	}
	lo--
	hi++
	const label, width, rowH = 250.0, 860.0, 22.0
	span := float64(hi - lo + 1)
	x := func(m string) (float64, bool) {
		v, ok := monthNum(m)
		if !ok {
			return 0, false
		}
		return label + (float64(v-lo)+0.5)/span*(width-label-12), true
	}
	height := float64(len(items))*rowH + 30
	rowOf := map[string]int{}
	for i, it := range items {
		rowOf[it.ID] = i
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<figure class="diagram"><svg viewBox="0 0 %.0f %.0f" width="100%%" role="img" aria-label="%s">`, width, height, esc("The milestones on a time line"))
	b.WriteString(`<defs><pattern id="sched-hatch" width="5" height="5" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><line x1="0" y1="0" x2="0" y2="5" class="hatch"/></pattern></defs>`)
	for v := lo; v <= hi; v++ {
		xx := label + float64(v-lo)/span*(width-label-12)
		if v%12 == 0 {
			fmt.Fprintf(&b, `<line class="year" x1="%.1f" x2="%.1f" y1="14" y2="%.0f"/><text class="axis" x="%.1f" y="11">%d</text>`, xx, xx, height, xx+3, v/12)
		} else if v%3 == 0 {
			fmt.Fprintf(&b, `<line class="quarter" x1="%.1f" x2="%.1f" y1="14" y2="%.0f"/>`, xx, xx, height)
		}
	}
	for i, it := range items {
		y := 26 + float64(i)*rowH + rowH/2
		name := labels[it.ID]
		if name == "" {
			name = it.Name
		}
		if len(name) > 40 {
			name = name[:39] + "…"
		}
		fmt.Fprintf(&b, `<text class="row" x="4" y="%.1f">%s</text>`, y+4, esc(name))
		if a, ok := x(it.NotBefore); ok {
			if z, ok := x(it.NotAfter); ok && z > a {
				fmt.Fprintf(&b, `<rect class="window" x="%.1f" y="%.1f" width="%.1f" height="8" rx="3"/>`, a, y-4, z-a)
			}
		}
		cx, ok := x(it.Month)
		if !ok {
			fmt.Fprintf(&b, `<text class="axis" x="%.0f" y="%.1f" text-anchor="end">follows something outside the project</text>`, width-12, y+4)
			continue
		}
		if it.Pending {
			fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="34" height="8" fill="url(#sched-hatch)"/>`, cx-34, y-4)
		}
		for _, w := range it.WaitsOn {
			j, ok := rowOf[w]
			if !ok {
				continue
			}
			fx, ok := x(items[j].Month)
			if !ok {
				continue
			}
			fy := 26 + float64(j)*rowH + rowH/2
			cls := "dep-edge"
			if it.Critical && items[j].Critical {
				cls += " critical"
			}
			fmt.Fprintf(&b, `<path class="%s" d="M%.1f,%.1f H%.1f V%.1f H%.1f"/>`, cls, fx+5, fy, (fx+cx)/2, y, cx-6)
		}
		cls := "ms"
		if it.Critical {
			cls += " critical"
		}
		if it.Pending {
			cls += " pending"
		}
		fmt.Fprintf(&b, `<path class="%s" d="M%.1f,%.1f l6,6 -6,6 -6,-6Z"/>`, cls, cx, y-6)
	}
	b.WriteString(`</svg><figcaption>Each diamond is where a milestone falls; a band is its window, a hatched run one still to be set by an event. Lines run from what a milestone waits on; the heavier chain sets the last date.</figcaption></figure>`)
	return b.String()
}

const diagramCSS = `
figure.diagram { margin: 1rem 0 1.5rem; color: var(--muted); }
figure.diagram svg { display: block; font-family: var(--sans); }
figure.diagram figcaption { font-size: .78rem; margin-top: .4rem; }
.dep-edge { fill: none; stroke: var(--muted); stroke-opacity: .55; stroke-width: 1.3; }
.dep-edge.critical { stroke: var(--warn); stroke-opacity: .95; stroke-width: 2.6; }
.dep-edge.loop { stroke: var(--bad); stroke-dasharray: 5 4; }
.dep-node rect { fill: var(--paper); stroke: var(--line); stroke-width: 1.2; }
.dep-node.programme rect { fill: var(--accent-soft); stroke: var(--accent); }
.dep-node.focus rect { stroke: var(--accent); stroke-width: 2.4; }
.dep-node.critical rect { stroke: var(--warn); }
.dep-node.loop rect { stroke: var(--bad); stroke-dasharray: 5 4; }
.dep-node text { fill: var(--fg); font-size: 12.5px; }
.dep-node text.tag { fill: var(--muted); font-size: 10.5px; }
.year { stroke: var(--line); } .quarter { stroke: var(--line); stroke-dasharray: 2 3; stroke-opacity: .7; }
text.axis { fill: var(--muted); font-size: 10.5px; } text.row { fill: var(--fg); font-size: 11.5px; }
.window { fill: var(--accent); fill-opacity: .18; }
.hatch { stroke: var(--muted); stroke-width: 2; stroke-opacity: .45; }
.ms { fill: var(--accent); stroke: var(--accent); stroke-width: 1.2; }
.ms.critical { fill: var(--warn); stroke: var(--warn); }
.ms.pending { fill: var(--paper); }
`
