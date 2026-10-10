// Package evaluate judges a change agents use the way docs/EVALUATING.md
// says: by criteria scored from the server and the trace, never from an
// agent's report, run after run on one frozen build, until enough pass
// in a row.
//
// It reads the server through Server, a port its MCP client fills, and
// the trace as calls the trace port defines. A document's own criteria
// (a real organisation's charter, say) are a file kept beside its runs,
// never in this repository.
package evaluate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace"
)

// Server is what scoring needs of a Cartograph server: one tool call,
// answered as the tool's text, or an error the tool refused with.
type Server interface {
	Call(ctx context.Context, tool string, args map[string]any) (string, error)
}

// Criteria say what a passing run is, for one document. Every check is
// read from the server or the trace; a check left out is not scored.
type Criteria struct {
	// Name is what is being judged, for the report.
	Name string `json:"name"`
	// Agent is the name the agent under test gives the server (its
	// client's), whose calls the trace is read for.
	Agent string `json:"agent"`
	// Runs is how many fresh runs in a row must pass on one build.
	Runs int `json:"runs"`
	// WholeDocument: the agent brought the whole document, not a part.
	WholeDocument bool `json:"wholeDocument,omitempty"`
	// Proposed: the change set was proposed.
	Proposed bool `json:"proposed,omitempty"`
	// OneObjectiveEach: every project has exactly one objective.
	OneObjectiveEach bool `json:"oneObjectiveEach,omitempty"`
	// NoPeople: no record names a person by their title, nor any name
	// the document gives a person.
	NoPeople bool `json:"noPeople,omitempty"`
	// Structure is the layout the work must have, when given.
	Structure *Structure `json:"structure,omitempty"`
	// Totals are the least each list holds across every project
	// (deliverables, milestones, risks, kpis, ...).
	Totals map[string]int `json:"totals,omitempty"`
	// KPIs are the indicators that must exist, by name pattern, and the
	// names none may have.
	KPIs *KPIs `json:"kpis,omitempty"`
	// Rounds judges the asking, with a person present.
	Rounds *Rounds `json:"rounds,omitempty"`
	// Triangle: every risk is placed on scope, schedule or cost, and each
	// project says how it holds them, or the person was asked
	// (TAXONOMY.md D60).
	Triangle bool `json:"triangle,omitempty"`
}

// Structure is the main project's components, the services the work
// hands over to, what it leaves out, and what no component may be.
type Structure struct {
	// Components are one pattern for each component project the main
	// project lists, matched by name; no more, no fewer.
	Components []string `json:"components"`
	// Operations are a pattern each for an operation that must exist;
	// others may.
	Operations []string `json:"operations,omitempty"`
	// ScopeOut is a pattern one of the main project's out-of-scope lines
	// must match.
	ScopeOut string `json:"scopeOut,omitempty"`
	// NotComponents is a pattern no component's name may match: a
	// workstream, a phase.
	NotComponents string `json:"notComponents,omitempty"`
}

// KPIs are indicators by name pattern.
type KPIs struct {
	Require []string `json:"require,omitempty"`
	Forbid  string   `json:"forbid,omitempty"`
}

// Rounds judges a run with a person present, whose decisions are asked
// in rounds (docs/adr/0032).
type Rounds struct {
	// NotLeft are checks, by id, the document states: none may be left
	// for the person.
	NotLeft []string `json:"notLeft,omitempty"`
	// MaxCalls is the last streak's median calls per run, the most a run
	// may make; 0 leaves it unjudged, as on the first streak.
	MaxCalls int `json:"maxCalls,omitempty"`
}

// ReadCriteria reads a criteria file.
func ReadCriteria(path string) (Criteria, error) {
	var c Criteria
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if c.Runs <= 0 {
		c.Runs = 3
	}
	return c, nil
}

// Check is one criterion's result.
type Check struct {
	Name string `json:"name"`
	Pass bool   `json:"pass"`
	Why  string `json:"why"`
}

// Score is a run judged: each check, whether all passed, and the trace
// figures of the agent's own calls.
type Score struct {
	ChangeSet string       `json:"changeSet"`
	Checks    []Check      `json:"checks"`
	Pass      bool         `json:"pass"`
	Trace     trace.Report `json:"trace"`
}

// Passed is how many checks passed.
func (s Score) Passed() int {
	n := 0
	for _, c := range s.Checks {
		if c.Pass {
			n++
		}
	}
	return n
}

// leftCheck is a check the change set leaves for the person, as
// work_summary lists it.
type leftCheck struct {
	On    string `json:"on"`
	Check string `json:"check"`
	Asked string `json:"asked"`
}

// record is a change set's record as work_summary lists it.
type record struct {
	Record string
	Name   string
	Counts map[string]int
}

// ScoreRun scores one run's change set against the criteria: the server
// for what was written, calls for what the agent did, and the document
// for the names it gives people.
func ScoreRun(ctx context.Context, srv Server, c Criteria, changeSet string, calls []trace.Call, document string) (Score, error) {
	out := Score{ChangeSet: changeSet}
	var mine []trace.Call
	for _, call := range calls {
		if c.Agent == "" || call.Agent == c.Agent {
			mine = append(mine, call)
		}
	}
	out.Trace = trace.Analyse(mine)
	add := func(name string, pass bool, why string, args ...any) {
		out.Checks = append(out.Checks, Check{Name: name, Pass: pass, Why: fmt.Sprintf(why, args...)})
	}

	status, recs, left, err := summary(ctx, srv, changeSet)
	if err != nil {
		return out, err
	}
	var projects, operations, kpis []record
	for _, r := range recs {
		switch kindOf(r.Record) {
		case "Project":
			projects = append(projects, r)
		case "Operation":
			operations = append(operations, r)
		case "KPI":
			kpis = append(kpis, r)
		}
	}
	var main *record
	for i := range projects {
		p := &projects[i]
		if main == nil || p.Counts["components"]*1000+p.Counts["milestones"] > main.Counts["components"]*1000+main.Counts["milestones"] {
			main = p
		}
	}
	texts, err := records(ctx, srv, changeSet, recs)
	if err != nil {
		return out, err
	}

	if c.WholeDocument {
		biggest := 0
		for _, call := range mine {
			if (call.Tool == "port" || call.Tool == "bring_document") && call.Outcome == trace.OK && call.InputBytes > biggest {
				biggest = call.InputBytes
			}
		}
		add("whole document", biggest*100 >= len(document)*98, "the largest text the server took is %d bytes; the document is %d", biggest, len(document))
	}
	if s := c.Structure; s != nil {
		var names []string
		for _, p := range projects {
			if main == nil || p.Record != main.Record {
				names = append(names, p.Name)
			}
		}
		var missing, stray, missingOps []string
		for _, pat := range s.Components {
			if !anyMatch(pat, names) {
				missing = append(missing, pat)
			}
		}
		if s.NotComponents != "" {
			for _, n := range names {
				if match(s.NotComponents, n) {
					stray = append(stray, n)
				}
			}
		}
		var opNames []string
		for _, o := range operations {
			opNames = append(opNames, o.Name)
		}
		for _, pat := range s.Operations {
			if !anyMatch(pat, opNames) {
				missingOps = append(missingOps, pat)
			}
		}
		scope := true
		if s.ScopeOut != "" && main != nil {
			scope = match(s.ScopeOut, section(texts[main.Record], "scopeOut"))
		}
		listed := 0
		if main != nil {
			listed = main.Counts["components"]
		}
		ok := len(missing) == 0 && len(stray) == 0 && len(missingOps) == 0 && scope &&
			len(names) == len(s.Components) && listed == len(s.Components)
		add("structure", ok, "components %q (missing %q, not components %q), main lists %d; operations %q (missing %q); scope-out matched %v",
			names, missing, stray, listed, opNames, missingOps, scope)
	}
	if c.OneObjectiveEach {
		bad := map[string]int{}
		for _, p := range projects {
			if p.Counts["objectives"] != 1 {
				bad[p.Name] = p.Counts["objectives"]
			}
		}
		add("one objective each", len(projects) > 0 && len(bad) == 0, "%d projects; not one objective: %v", len(projects), bad)
	}
	if len(c.Totals) > 0 || c.KPIs != nil {
		got := map[string]int{}
		for k := range c.Totals {
			for _, p := range projects {
				got[k] += p.Counts[k]
			}
		}
		ok := true
		for k, least := range c.Totals {
			ok = ok && got[k] >= least
		}
		var kpiNames, missing, junk []string
		for _, k := range kpis {
			kpiNames = append(kpiNames, k.Name)
		}
		if c.KPIs != nil {
			for _, pat := range c.KPIs.Require {
				if !anyMatch(pat, kpiNames) {
					missing = append(missing, pat)
				}
			}
			if c.KPIs.Forbid != "" {
				for _, n := range kpiNames {
					if match(c.KPIs.Forbid, n) {
						junk = append(junk, n)
					}
				}
			}
		}
		ok = ok && len(missing) == 0 && len(junk) == 0
		add("registers placed", ok, "totals %v (least %v); KPIs missing %q, not KPIs %q", got, c.Totals, missing, junk)
	}
	if c.NoPeople {
		names := documentNames(document)
		var hits []string
		refs := make([]string, 0, len(texts))
		for r := range texts {
			refs = append(refs, r)
		}
		sort.Strings(refs)
		for _, r := range refs {
			t := texts[r]
			if titled.MatchString(t) {
				hits = append(hits, r+" (a titled name)")
			}
			for _, n := range names {
				if regexp.MustCompile(`\b` + regexp.QuoteMeta(n) + `\b`).MatchString(t) {
					hits = append(hits, r+" ("+n+")")
				}
			}
		}
		add("no people", len(hits) == 0, "%d records name a person: %q", len(hits), hits)
	}
	if c.Proposed {
		add("proposed", status == "proposed", "status %s", status)
	}
	if r := c.Rounds; r != nil {
		scoreRounds(r, left, mine, add)
	}
	if c.Triangle {
		// A risk's side is read from the risk, so it is never left; a stance
		// is the person's, so it may be left only once they were asked.
		var unplaced, unasked []string
		for _, l := range left {
			switch l.Check {
			case "risks-constrained":
				unplaced = append(unplaced, l.On)
			case "constraints-stated":
				if a := strings.TrimSpace(l.Asked); a == "" || strings.EqualFold(a, "not available") {
					unasked = append(unasked, l.On)
				}
			}
		}
		add("triangle placed", len(projects) > 0 && len(unplaced) == 0 && len(unasked) == 0,
			"%d projects; risks left on no side: %v; stances left without asking the person: %v", len(projects), unplaced, unasked)
	}
	out.Pass = len(out.Checks) > 0 && out.Passed() == len(out.Checks)
	return out, nil
}

// summary is the change set's status and records, from work_summary.
func summary(ctx context.Context, srv Server, changeSet string) (string, []record, []leftCheck, error) {
	text, err := srv.Call(ctx, "work_summary", map[string]any{"changeSet": changeSet})
	if err != nil {
		return "", nil, nil, fmt.Errorf("work_summary: %w", err)
	}
	var raw struct {
		Status  string           `json:"status"`
		Records []map[string]any `json:"records"`
		Left    []leftCheck      `json:"leftForYourPerson"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return "", nil, nil, fmt.Errorf("work_summary: %w", err)
	}
	var out []record
	for _, m := range raw.Records {
		r := record{Counts: map[string]int{}}
		r.Record, _ = m["record"].(string)
		r.Name, _ = m["name"].(string)
		for k, v := range m {
			if f, ok := v.(float64); ok {
				r.Counts[k] = int(f)
			}
		}
		out = append(out, r)
	}
	return raw.Status, out, raw.Left, nil
}

// records are every record's YAML as the change set holds it, read a
// hundred at a time.
func records(ctx context.Context, srv Server, changeSet string, recs []record) (map[string]string, error) {
	out := map[string]string{}
	for from := 0; from < len(recs); from += 100 {
		to := min(from+100, len(recs))
		var refs []string
		for _, r := range recs[from:to] {
			refs = append(refs, r.Record)
		}
		text, err := srv.Call(ctx, "get", map[string]any{"changeSet": changeSet, "records": refs})
		if err != nil {
			return nil, fmt.Errorf("get: %w", err)
		}
		var got struct {
			Items []struct {
				Record string `json:"record"`
				YAML   string `json:"yaml"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			return nil, fmt.Errorf("get: %w", err)
		}
		for _, it := range got.Items {
			out[it.Record] = it.YAML
		}
	}
	return out, nil
}

// titled is a person named by their title.
var titled = regexp.MustCompile(`\b(Dr|Mr|Mrs|Ms|Mx|Prof)\.[ \t]+[A-Z]`)

// documentNames are the names the document gives people, read where it
// names them by a title: a word it never writes in lower case.
func documentNames(document string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range regexp.MustCompile(`\b(?:Dr|Mr|Mrs|Ms|Mx|Prof)\.[ \t]+(?:[A-Z]\.[ \t]*)*([A-Z][a-z]{2,})`).FindAllStringSubmatch(document, -1) {
		n := m[1]
		if seen[n] || regexp.MustCompile(`\b`+strings.ToLower(n)+`\b`).MatchString(document) {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// section is the text of a YAML key's block, from its line to the next
// key at its indent or less.
func section(yaml, key string) string {
	lines := strings.Split(yaml, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != key+":" {
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " "))
		var b strings.Builder
		for _, m := range lines[i+1:] {
			if strings.TrimSpace(m) != "" && len(m)-len(strings.TrimLeft(m, " ")) <= indent && !strings.HasPrefix(strings.TrimLeft(m, " "), "- ") {
				break
			}
			b.WriteString(m)
			b.WriteByte('\n')
		}
		return b.String()
	}
	return ""
}

func kindOf(ref string) string {
	k, _, _ := strings.Cut(ref, "/")
	return k
}

func match(pattern, s string) bool {
	re, err := regexp.Compile("(?i)" + pattern)
	return err == nil && re.MatchString(s)
}

func anyMatch(pattern string, names []string) bool {
	for _, n := range names {
		if match(pattern, n) {
			return true
		}
	}
	return false
}

// scoreRounds judges the asking (docs/adr/0032), from what the change set
// leaves for the person and the agent's calls. What the person was asked
// is read as each left check keeps it, an exchange to a question: a
// question left open names the exchange it was asked in, and several
// questions in one exchange share it.
func scoreRounds(r *Rounds, left []leftCheck, mine []trace.Call, add func(string, bool, string, ...any)) {
	notLeft := map[string]bool{}
	for _, c := range r.NotLeft {
		notLeft[c] = true
	}
	var unasked, stated []string
	questions := map[string]int{}
	for _, l := range left {
		asked := strings.TrimSpace(l.Asked)
		if asked == "" || strings.EqualFold(asked, "not available") {
			unasked = append(unasked, l.On+" "+l.Check)
		}
		if notLeft[l.Check] {
			stated = append(stated, l.On+" "+l.Check)
		}
		// An exchange is one the person had: "not available" is none.
		for _, ex := range strings.Split(asked, " Also: ") {
			if ex = strings.TrimSpace(ex); ex != "" && !strings.EqualFold(ex, "not available") {
				questions[ex]++
			}
		}
	}
	add("left with what was asked", len(unasked) == 0 && len(stated) == 0,
		"%d checks left; without what the person was asked %q; stated by the document %q", len(left), unasked, stated)

	round, propose := -1, -1
	for i, call := range mine {
		if call.Tool == "round" && call.Outcome == trace.OK && round < 0 {
			round = i
		}
		if call.Tool == "propose" && propose < 0 {
			propose = i
		}
	}
	add("round before propose", round >= 0 && (propose < 0 || round < propose), "first round at call %d, first propose at %d", round, propose)

	asked := 0
	for _, n := range questions {
		asked += n
	}
	// One question left open can only have been one exchange.
	together := asked <= 1 || asked > len(questions)
	add("asked together", together, "%d questions left open over %d exchanges", asked, len(questions))

	if r.MaxCalls > 0 {
		add("no more calls", len(mine) <= r.MaxCalls, "%d calls; the last streak's median is %d", len(mine), r.MaxCalls)
	}
}
