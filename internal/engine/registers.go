package engine

import (
	"fmt"
	"regexp"
	"slices"
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

var workstreamCode = regexp.MustCompile(`^\s{0,4}((?i:ws)\s?[0-9]{1,2})\b`)

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
					// A table with other columns after the register's rows
					// is another table (a form of fields and responses),
					// never more of the register.
					if len(out) > 0 {
						return out
					}
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
			// After the register's rows, a block of short labels with no
			// figure (Field, Requirement, Project Response) heads another
			// table: the register has ended.
			if len(out) > 0 && labelsOnly(b) {
				return out
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
		{"source", "data source|source of data|source"},
		{"cycle", "frequency|cycle|how often"},
		{"dependency", "dependenc|depends|predecessor"},
		{"name", "milestone|deliverable|description|risk|issue|item|name|title|indicator|kpi|output"},
	} {
		for _, w := range strings.Split(r.words, "|") {
			// A code's words are short and stand alone: "id" inside
			// "evidence" is no code.
			if r.role == "code" {
				if slices.Contains(strings.FieldsFunc(h, func(c rune) bool { return c == ' ' || c == '/' || c == '(' || c == ')' }), w) {
					return r.role
				}
				continue
			}
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
//
// The server's porting map is applied to the rows as they are read, so a
// port comes out the same whoever runs it: a milestone already completed
// is not ported unless one still to come waits on it, and a measure taken
// once is not an indicator. Such a row is returned with _skip, its reason.
func RegisterItems(field string, rows []RegisterRow) []map[string]any {
	var out []map[string]any
	waitedOn := map[string]bool{}
	for _, r := range rows {
		// Only a milestone still to come keeps a completed one it waits on.
		done := false
		for h, v := range r.Cells {
			if headerRole(h) == "evidence" && completed.MatchString(v) {
				done = true
			}
		}
		if done {
			continue
		}
		for h, v := range r.Cells {
			if headerRole(h) == "dependency" {
				for _, m := range milestoneCode.FindAllString(v, -1) {
					waitedOn[strings.ToLower(m)] = true
				}
			}
		}
	}
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
			if completed.MatchString(byRole["evidence"]) && !waitedOn[id] {
				item["_skip"] = "already completed, and no milestone still to come waits on it: not ported (porting map)"
			}
			if t := cellTiming(byRole["date"]); t != nil {
				item["timing"] = t
			} else {
				item["timing"] = map[string]any{"form": "date", "note": clip(byRole["date"], 240)}
			}
			if o := leadOf(byRole["owner"]); o != "" {
				item["owner"] = o
			}
			if byRole["evidence"] != "" {
				item["evidence"] = clip(byRole["evidence"], 240)
			}
		case "deliverables":
			item["id"], item["name"] = id, clip(name, 60)
			if len([]rune(name)) > 60 {
				item["description"] = clip(name, 240)
			}
			if t := cellTiming(byRole["date"]); t != nil {
				item["due"] = t
			}
			if o := leadOf(byRole["owner"]); o != "" {
				item["owner"] = o
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
			if o := leadOf(byRole["owner"]); o != "" {
				item["owner"] = o
			}
		case "kpis":
			// An indicator row carries a baseline or a target; a fragment
			// carried from another row does not.
			if byRole["baseline"] == "" && byRole["date"] == "" {
				continue
			}
			item["kpi"] = clip(name, 120)
			if once.MatchString(byRole["cycle"]) {
				item["_skip"] = "a measure taken once is not an indicator: write it as the acceptance of the deliverable it is about (porting map)"
			}
			reason := "Named in the document's indicator register"
			if byRole["baseline"] != "" {
				reason += "; baseline " + byRole["baseline"]
			}
			item["reason"] = clip(reason, 240)
			// What the KPI itself holds, set on it once it is drafted.
			extra := map[string]any{}
			if src := leadOf(byRole["source"]); src != "" {
				extra["/spec/sources"] = []any{clip(src, 120)}
			}
			// A cycle of equal periods is named by its period; one in other
			// words (termly, once) is left for the person to set.
			switch cyclePeriod(byRole["cycle"]) {
			case 1:
				extra["/spec/cycle"] = "Monthly"
			case 3:
				extra["/spec/cycle"] = "Quarterly"
			case 6:
				extra["/spec/cycle"] = "Half-yearly"
			case 12:
				extra["/spec/cycle"] = "Yearly"
			}
			if b := baselineOf(byRole["baseline"]); b != nil {
				extra["/spec/baseline"] = b
			}
			if t := registerTarget(byRole["date"]); t != nil {
				extra["/spec/target"] = t
			}
			if w := strings.ToLower(byRole["date"]); strings.Contains(w, "decline") || strings.Contains(w, "reduc") || strings.Contains(w, "fewer") || strings.Contains(w, "lower") {
				extra["/spec/direction"] = "decrease"
			}
			// The body that owns the indicator keeps its source.
			if team := leadOf(byRole["owner"]); team != "" && extra["/spec/sources"] != nil {
				extra["_team"] = clip(team, 120)
			}
			if len(extra) > 0 {
				item["_kpi"] = extra
			}
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
		last = namedMonth(cell)
	}
	if last != "" {
		return map[string]any{"form": "date", "date": last}
	}
	// Only years ("Term I 2026/2027"): a window over them, honestly wide,
	// the cell kept as its note; nothing at all is left to the checks.
	years := regexp.MustCompile(`\b(19|20)[0-9]{2}\b`).FindAllString(cell, -1)
	if len(years) > 0 {
		return map[string]any{"form": "window", "notBefore": years[0] + "-01", "notAfter": years[len(years)-1] + "-12", "note": clip(cell, 240)}
	}
	return nil
}

// monthNamed is a month named in words with its year (June 2026, Jul
// 2026).
var monthNamed = regexp.MustCompile(`(?i)\b(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+((?:19|20)[0-9]{2})\b`)

// namedMonth is the last month a cell names in words, as YYYY-MM, or "".
func namedMonth(cell string) string {
	all := monthNamed.FindAllStringSubmatch(cell, -1)
	if len(all) == 0 {
		return ""
	}
	m := all[len(all)-1]
	months := "janfebmaraprmayjunjulaugsepoctnovdec"
	n := strings.Index(months, strings.ToLower(m[1]))/3 + 1
	return fmt.Sprintf("%s-%02d", m[2], n)
}

// leadingFigure is the figure a cell starts with (0, 100%, 49.4), the
// value it states; a cell that starts in words states none.
var leadingFigure = regexp.MustCompile(`^\s*(-?[0-9]+(?:\.[0-9]+)?)\s*%?(?:\s|$|[-(;,])`)

func figureOf(cell string) (float64, bool) {
	m := leadingFigure.FindStringSubmatch(cell)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	return v, err == nil
}

// baselineOf reads a register's baseline cell: a figure with its month,
// else the cell's words kept as why there is no figure with a date yet.
func baselineOf(cell string) map[string]any {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return nil
	}
	month := namedMonth(cell)
	if t := cellTiming(cell); t != nil && t["form"] == "date" {
		month, _ = t["date"].(string)
	}
	if v, ok := figureOf(cell); ok && month != "" {
		return map[string]any{"value": v, "date": month}
	}
	return map[string]any{"unknownReason": clip("No figure with a date yet; the document says: "+cell, 240)}
}

// registerTarget reads a register's target cell: a figure by a month, or by a
// window of years; nil when the cell gives no figure (a target to be set).
func registerTarget(cell string) map[string]any {
	v, ok := figureOf(cell)
	if !ok {
		return nil
	}
	t := cellTiming(cell)
	switch {
	case t == nil:
		return nil
	case t["form"] == "date":
		return map[string]any{"value": v, "date": t["date"]}
	default:
		return map[string]any{"value": v, "due": t}
	}
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

// leadOf is the body that leads where a cell names several ("EHSU /
// NMD-MoH / NSDSL"): the first, so one role is drafted for it, not one
// for every combination.
func leadOf(cell string) string {
	parts := regexp.MustCompile(`\s*[/;,]\s*`).Split(strings.TrimSpace(cell), -1)
	for _, p := range parts {
		// A person (Dr. Ada Mensah) is never an owner: Cartograph names
		// roles and bodies, so the first body the cell names leads.
		if p = strings.TrimSpace(p); p != "" && !personLed.MatchString(p) {
			return p
		}
	}
	return ""
}

// milestoneCode is a milestone's code as a dependency names it (M12,
// M6a); completed is an evidence cell that says the milestone is done;
// once is a frequency that says a measure is taken once.
var (
	milestoneCode = regexp.MustCompile(`(?i)\bM[0-9]{1,3}[a-z]?\b`)
	completed     = regexp.MustCompile(`(?i)^\s*(completed?|done|achieved)\b`)
	once          = regexp.MustCompile(`(?i)^\s*once\b`)
)

// labelsOnly reports whether a block is three or more short labels and
// nothing else: a table's header, not a row.
func labelsOnly(b []string) bool {
	n := 0
	for _, l := range b {
		for _, g := range groups(l) {
			if len(strings.Fields(g.text)) > 3 || strings.ContainsAny(g.text, "0123456789%") {
				return false
			}
			n++
		}
	}
	return n >= 3
}

// personLed matches a name led by a person's title.
var personLed = regexp.MustCompile(`^(?i:dr|mr|mrs|ms|miss|mx|prof|professor|sir|dame|hon)\.?\s`)

// cyclePeriod is the length in months of a cycle a register names in
// words, or 0 when the words name no equal period (termly, once).
func cyclePeriod(words string) int {
	w := strings.ToLower(words)
	switch {
	case strings.Contains(w, "month"):
		return 1
	case strings.Contains(w, "quarter"):
		return 3
	case strings.Contains(w, "half-year") || strings.Contains(w, "biannual") || strings.Contains(w, "semi-annual") || strings.Contains(w, "six-month"):
		return 6
	case strings.Contains(w, "annual") || strings.Contains(w, "year"):
		return 12
	}
	return 0
}

// WorkstreamNames are the names a document's workstream plan gives its
// workstreams: in a section headed as workstreams, the text that follows
// a workstream's code on its line (WS5  Monitoring, Data and ...).
func WorkstreamNames(src Source) []string {
	var out []string
	for _, sec := range src.Sections {
		if !strings.Contains(strings.ToLower(sec.Heading), "workstream") {
			continue
		}
		// A row is a block of lines; its name is the left-hand cell,
		// wrapped over the block's lines.
		lines := strings.Split(sec.text, "\n")
		for i := 0; i < len(lines); i++ {
			m := workstreamCode.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			from, to := i, i
			for from > 0 && strings.TrimSpace(lines[from-1]) != "" {
				from--
			}
			for to+1 < len(lines) && strings.TrimSpace(lines[to+1]) != "" {
				to++
			}
			first := groups(lines[i])
			if len(first) == 0 {
				continue
			}
			edge := strings.Index(lines[i], first[0].text) + len(first[0].text) + 2
			var parts []string
			for _, l := range lines[from : to+1] {
				for _, g := range groups(l) {
					if start := strings.Index(l, g.text); start >= 0 && start < edge {
						parts = append(parts, g.text)
					}
				}
			}
			name := strings.TrimSpace(strings.TrimPrefix(strings.Join(parts, " "), m[1]))
			if name != "" {
				out = append(out, name)
			}
			i = to
		}
	}
	return out
}

// NearName reports whether two names say the same thing.
func NearName(a, b string) bool { return nearName(a, b) }
