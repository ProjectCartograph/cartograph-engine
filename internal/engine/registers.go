package engine

import (
	"regexp"
	"strconv"
	"strings"
)

// A document's registers read as rows (docs/adr/0027): the tables a
// charter lists its milestones, deliverables, indicators and risks in,
// each row led by a code (M1, D10, R9), its cells wrapped over several
// lines, its columns named by a header that a printed page may repeat at
// other offsets. A row's cells go to the header column nearest them.

// RegisterRow is one row: its code and its cells by header.
type RegisterRow struct {
	Code  string
	Cells map[string]string
}

var rowCode = regexp.MustCompile(`^\s{0,4}([A-Z]{1,3}-?[0-9]{1,2}[a-z]?)(\s{2,}\S|\s*$)`)

// furniture is a page's header or footer, never a row.
var furniture = regexp.MustCompile(`(?i)^\s*page\s+[0-9]+\s*$|\|`)

// ReadRegister reads the rows of the tables in a section's text.
func ReadRegister(text string) []RegisterRow {
	lines := strings.Split(text, "\n")
	// Blocks of lines with no blank line between them.
	var blocks [][]string
	var cur []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if len(cur) > 0 {
				blocks = append(blocks, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, l)
	}
	if len(cur) > 0 {
		blocks = append(blocks, cur)
	}
	type column struct {
		name   string
		centre float64
	}
	var cols []column
	var out []RegisterRow
	coded := false
	for _, b := range blocks {
		code := ""
		for _, l := range b {
			if m := rowCode.FindStringSubmatch(l); m != nil {
				code = m[1]
				break
			}
		}
		if code == "" {
			// A header: words that name columns, read across its lines.
			if hdr := headerColumns(b); len(hdr) >= 3 && headerish(hdr) {
				// The same header again is the table carried over a page.
				same := len(hdr) == len(cols)
				for i := range hdr {
					if same && headerRole(hdr[i].text) != headerRole(cols[i].name) {
						same = false
					}
				}
				if !same {
					coded = false
				}
				cols = cols[:0]
				for _, h := range hdr {
					cols = append(cols, column{h.text, h.centre})
				}
				continue
			}
			// A table without codes: a block under a header that fills
			// two columns or more is a row, numbered in order.
			if len(cols) == 0 || isFurniture(b) || hasHeading(b) {
				continue
			}
			filled := map[int]bool{}
			for _, l := range b {
				for _, g := range groups(l) {
					filled[nearest(g.centre, func(i int) float64 { return cols[i].centre }, len(cols))] = true
				}
			}
			if len(filled) < 2 {
				continue
			}
			// In a table whose rows have codes, a block without one is the
			// row before it, carried over a page.
			if coded && len(out) > 0 {
				prev := &out[len(out)-1]
				for _, l := range b {
					for _, g := range groups(l) {
						name := cols[nearest(g.centre, func(i int) float64 { return cols[i].centre }, len(cols))].name
						prev.Cells[name] = strings.TrimSpace(prev.Cells[name] + " " + g.text)
					}
				}
				continue
			}
			code = "row" + strconv.Itoa(len(out)+1)
		} else {
			coded = true
		}
		if len(cols) == 0 {
			continue
		}
		row := RegisterRow{Code: code, Cells: map[string]string{}}
		for _, l := range b {
			for _, g := range groups(l) {
				if g.text == code {
					continue
				}
				name := cols[nearest(g.centre, func(i int) float64 { return cols[i].centre }, len(cols))].name
				if row.Cells[name] != "" {
					row.Cells[name] += " "
				}
				row.Cells[name] += g.text
			}
		}
		out = append(out, row)
	}
	return out
}

type group struct {
	text   string
	centre float64
}

// groups splits a line into runs of text two or more spaces apart.
func groups(l string) []group {
	var out []group
	start := -1
	spaces := 0
	for i, r := range l + "  " {
		if r == ' ' || r == '\t' {
			spaces++
			if spaces >= 2 && start >= 0 {
				end := i - spaces + 1
				out = append(out, group{strings.TrimSpace(l[start:end]), float64(start+end) / 2})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
		spaces = 0
	}
	return out
}

// headerColumns reads a header's columns across its lines: groups that
// overlap left to right are one column, their words joined top to bottom.
func headerColumns(b []string) []group {
	type span struct {
		from, to float64
		text     string
	}
	var spans []span
	for _, l := range b {
		// A section's own heading above the table is not a header.
		if heading.MatchString(l) {
			continue
		}
		for _, g := range groups(l) {
			w := float64(len(g.text)) / 2
			placed := false
			for i := range spans {
				if g.centre+w >= spans[i].from && g.centre-w <= spans[i].to {
					spans[i].text += " " + g.text
					if g.centre-w < spans[i].from {
						spans[i].from = g.centre - w
					}
					if g.centre+w > spans[i].to {
						spans[i].to = g.centre + w
					}
					placed = true
					break
				}
			}
			if !placed {
				spans = append(spans, span{g.centre - w, g.centre + w, g.text})
			}
		}
	}
	var out []group
	for _, s := range spans {
		// A header names columns; a line of prose is not one.
		if len(strings.Fields(s.text)) > 4 {
			return nil
		}
		out = append(out, group{s.text, (s.from + s.to) / 2})
	}
	return out
}

// headerRole is what a column holds, by the words its header uses.
func headerRole(h string) string {
	h = strings.ToLower(h)
	for _, r := range []struct{ role, words string }{
		{"code", "no.|ref|id|#|code"},
		{"mitigation", "mitigation|response|treatment|action"},
		{"impact", "impact|severity"},
		{"likelihood", "likelihood|probability"},
		{"type", "type|category"},
		{"evidence", "evidence|verification|acceptance|means of"},
		{"owner", "owner|lead|responsible|accountable"},
		{"date", "date|due|deadline|when|target"},
		{"baseline", "baseline"},
		{"dependency", "dependenc|depends|predecessor"},
		{"name", "milestone|deliverable|description|risk|issue|item|name|title|indicator|kpi|output"},
	} {
		for _, w := range strings.Split(r.words, "|") {
			if strings.Contains(h, w) {
				return r.role
			}
		}
	}
	return ""
}

// RegisterItems maps a register's rows onto a list field of a project, in
// the field's own shape: milestones, deliverables, risks or the KPIs it
// names. What a row does not give is left out, for the checks to ask.
func RegisterItems(field string, rows []RegisterRow) []map[string]any {
	var out []map[string]any
	for _, r := range rows {
		byRole := map[string]string{}
		for h, v := range r.Cells {
			if role := headerRole(h); role != "" && byRole[role] == "" {
				byRole[role] = strings.TrimSpace(v)
			}
		}
		id := strings.ToLower(strings.ReplaceAll(r.Code, "-", ""))
		name := byRole["name"]
		if name == "" {
			continue
		}
		item := map[string]any{}
		switch strings.TrimPrefix(field, "/spec/") {
		case "milestones":
			item["id"], item["name"] = id, clip(name, 120)
			item["timing"] = cellTiming(byRole["date"])
			if byRole["owner"] != "" {
				item["owner"] = byRole["owner"]
			}
			if byRole["evidence"] != "" {
				item["evidence"] = clip(byRole["evidence"], 240)
			}
		case "deliverables":
			item["id"], item["name"] = id, clip(name, 60)
			if len([]rune(name)) > 60 {
				item["description"] = clip(name, 240)
			}
			if byRole["date"] != "" {
				item["due"] = cellTiming(byRole["date"])
			}
			if byRole["owner"] != "" {
				item["owner"] = byRole["owner"]
			}
		case "risks":
			item["id"], item["description"] = id, clip(name, 160)
			item["type"] = "risk"
			for _, t := range []string{"issue", "dependency", "constraint"} {
				if strings.Contains(strings.ToLower(byRole["type"]), t) {
					item["type"] = t
				}
			}
			for _, k := range []string{"impact", "likelihood"} {
				if lvl := level(byRole[k]); lvl != "" {
					item[k] = lvl
				}
			}
			if byRole["mitigation"] != "" {
				item["mitigation"] = clip(byRole["mitigation"], 160)
			}
			if byRole["owner"] != "" {
				item["owner"] = byRole["owner"]
			}
		case "kpis":
			// An indicator row carries a baseline or a target; a fragment
			// carried from another row does not.
			if byRole["baseline"] == "" && byRole["date"] == "" {
				continue
			}
			item["kpi"] = clip(name, 120)
			reason := "Named in the document's indicator register"
			if byRole["baseline"] != "" {
				reason += "; baseline " + byRole["baseline"]
			}
			item["reason"] = clip(reason, 240)
		default:
			return nil
		}
		out = append(out, item)
	}
	return out
}

// cellTiming reads a date cell as a timing: the last date in it, as its
// month, else a timing to be set.
func cellTiming(cell string) map[string]any {
	var last string
	for _, w := range strings.FieldsFunc(cell, func(r rune) bool { return r == ' ' || r == ';' || r == ',' }) {
		// A range of days ("25-26/02/2026") is read at its last day.
		if slash := strings.Index(w, "/"); slash > 0 && strings.Count(w, "/") == 2 {
			if dash := strings.LastIndex(w[:slash], "-"); dash >= 0 {
				w = w[dash+1:]
			}
		}
		w = strings.Trim(w, "().")
		if m, ok := readMonth(w); ok {
			last = m
		}
	}
	if last == "" {
		return map[string]any{"form": "date"}
	}
	return map[string]any{"form": "date", "date": last}
}

func level(cell string) string {
	c := strings.ToLower(cell)
	for _, l := range []string{"high", "medium", "low"} {
		if strings.Contains(c, l) {
			return l
		}
	}
	if strings.Contains(c, "med") {
		return "medium"
	}
	return ""
}

// nearest is the index of the centre closest to c.
func nearest(c float64, centre func(int) float64, n int) int {
	best, dist := 0, -1.0
	for i := 0; i < n; i++ {
		d := c - centre(i)
		if d < 0 {
			d = -d
		}
		if dist < 0 || d < dist {
			best, dist = i, d
		}
	}
	return best
}

// headerish reports whether columns read as a header: most of them name
// what a column holds.
func headerish(cols []group) bool {
	named := 0
	for _, c := range cols {
		if headerRole(c.text) != "" {
			named++
		}
	}
	return named*2 >= len(cols)
}

func isFurniture(b []string) bool {
	for _, l := range b {
		if furniture.MatchString(l) {
			return true
		}
	}
	return false
}

func hasHeading(b []string) bool {
	for _, l := range b {
		if heading.MatchString(l) {
			return true
		}
	}
	return false
}
