package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/contract"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
)

// A guide is how to define one kind well, put together for whoever is
// defining it, person or agent: the flow's steps and fields in order, the
// words for them in one language (contract/guidance), and the
// organisation's own records each reference and link can use. It holds no
// rule: the checks and the validation are the engine's, and a guide only
// says how to meet them. Consistency comes from the rules; the guide makes
// meeting them the obvious path.

// GuideBundle is a guidance file as written: words only, in one language.
type GuideBundle struct {
	Locale     string            `json:"locale"`
	Kind       string            `json:"kind"`
	Summary    string            `json:"summary"`
	Example    string            `json:"example,omitempty"`
	KnownAs    []string          `json:"knownAs,omitempty"`
	Definition string            `json:"definition"`
	Levels     map[string]string `json:"levels,omitempty"`
	// LevelExamples are one instance of each level, as Example is for the
	// kind.
	LevelExamples map[string]string `json:"levelExamples,omitempty"`
	// Terms are other words the kind holds, each defined as the kind is.
	Terms map[string]GuideTerm `json:"terms,omitempty"`
	// Judgements are checks a decision model answers, by check id.
	Judgements map[string]GuideJudgement `json:"judgements,omitempty"`
	// Cues are how a decision model recognises a typed text as this kind,
	// by stage key.
	Cues map[string]string `json:"cues,omitempty"`
	// FromIdea are the questions a rough idea is read for, by name.
	FromIdea   map[string]GuideIdeaQuestion `json:"fromIdea,omitempty"`
	Steps      map[string]GuideStepWords    `json:"steps,omitempty"`
	Fields     map[string]GuideFieldWords   `json:"fields"`
	Links      map[string]GuideLinkWords    `json:"links,omitempty"`
	Checks     map[string]string            `json:"checks,omitempty"`
	Vocabulary map[string][]string          `json:"vocabulary,omitempty"`
}

// GuideIdeaQuestion is one question a rough idea is read for: the
// question a step asks, the field it fills, and the cue a sentence is
// matched against.
type GuideIdeaQuestion struct {
	Question string `json:"question"`
	Field    string `json:"field"`
	Cue      string `json:"cue"`
}

// GuideTerm is one word a kind holds besides its own name: a project's
// component, say.
type GuideTerm struct {
	Summary string `json:"summary"`
	Example string `json:"example"`
}

// GuideStepWords are a step's title and guide in one language.
type GuideStepWords struct {
	Title string `json:"title,omitempty"`
	Guide string `json:"guide,omitempty"`
}

// GuideFieldWords are what a field asks for, with examples.
type GuideFieldWords struct {
	Guide  string                     `json:"guide"`
	Good   []string                   `json:"good,omitempty"`
	Poor   []GuidePoor                `json:"poor,omitempty"`
	Levels map[string]GuideFieldWords `json:"levels,omitempty"`
}

// GuidePoor is a plausible wrong value and why it is wrong.
type GuidePoor struct {
	Text string `json:"text"`
	Why  string `json:"why"`
}

// GuideLinkWords are what to ask for a link, and what to do when there is
// nothing to link to.
type GuideLinkWords struct {
	Ask    string `json:"ask"`
	IfNone string `json:"ifNone"`
}

// Guide is a kind's guide for one level, in one language, with the
// organisation's context.
type Guide struct {
	Kind       string `json:"kind"`
	Locale     string `json:"locale"`
	Definition string `json:"definition"`
	Level      string `json:"level,omitempty"`
	LevelIs    string `json:"levelIs,omitempty"`
	// Plan is the work around it, in the order of work: what it names,
	// before it, then what will name it (for an objective: its outcomes,
	// the KPI measuring each, then the gaps they close).
	Plan    []PlanItem        `json:"plan,omitempty"`
	Levels  map[string]string `json:"levels,omitempty"`
	Purpose *Purpose          `json:"purpose,omitempty"`
	// Existing is the records of this kind (at this level) already
	// defined: reuse or improve one before defining another.
	Existing []Candidate `json:"existing"`
	// Template is a manifest of this kind with its envelope and required
	// fields, to fill in.
	Template map[string]any    `json:"template"`
	Steps    []GuideStep       `json:"steps"`
	Checks   map[string]string `json:"checks,omitempty"`
	// Vocabulary is words an interface offers for writing a field in this
	// language; never used to judge one.
	Vocabulary map[string][]string `json:"vocabulary,omitempty"`
}

// GuideStep is one step: what it settles, its fields, and the links it
// makes on other manifests.
type GuideStep struct {
	Key    string       `json:"key"`
	Title  string       `json:"title"`
	Guide  string       `json:"guide,omitempty"`
	Fields []GuideField `json:"fields"`
	Links  []GuideLink  `json:"links,omitempty"`
}

// GuideField is one field: its path, what it asks for with examples, the
// checks it answers, and for a reference the kind it names and the
// organisation's records of that kind to choose from.
type GuideField struct {
	Path       string      `json:"path"`
	Control    string      `json:"control"`
	Guide      string      `json:"guide,omitempty"`
	Good       []string    `json:"good,omitempty"`
	Poor       []GuidePoor `json:"poor,omitempty"`
	Checks     []string    `json:"checks,omitempty"`
	References string      `json:"references,omitempty"`
	Candidates []Candidate `json:"candidates,omitempty"`
	// Required says the field must be filled: the schema requires it
	// where it sits, or one of its checks blocks a handoff while it is
	// empty. Every interface marks it the same way, and an agent knows
	// which answers it cannot leave out.
	Required bool `json:"required,omitempty"`
	// MaxLength, Values, Format and Shape are what the schema holds the
	// field to, so an agent writes a value it will take without reading
	// the schema: the longest it may be, the values it may take, how a
	// date is written, and the JSON a reference, a period or a list item
	// is written as.
	MaxLength int    `json:"maxLength,omitempty"`
	Values    []any  `json:"values,omitempty"`
	Format    string `json:"format,omitempty"`
	Shape     string `json:"shape,omitempty"`
}

// GuideLink is a link held on another kind, naming this manifest: what to
// ask, what to do when none fits, and that kind's records.
type GuideLink struct {
	Kind       string      `json:"kind"`
	Path       string      `json:"path"`
	Check      string      `json:"check,omitempty"`
	Ask        string      `json:"ask,omitempty"`
	IfNone     string      `json:"ifNone,omitempty"`
	Candidates []Candidate `json:"candidates,omitempty"`
}

// Candidate is an existing record a reference or link may use: reuse one
// before defining another.
type Candidate struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	// Under is a goal's parent, so where it already sits is plain.
	Under string `json:"under,omitempty"`
}

// maxCandidates bounds the records listed per field; search finds the rest.
const maxCandidates = 40

// DefaultLocale is the language guidance falls back to.
const DefaultLocale = "en"

// GuideBundleFor returns a kind's guidance in locale, or in the default
// language when there is none in locale.
func GuideBundleFor(kind, locale string) (GuideBundle, bool, error) {
	for _, l := range []string{locale, DefaultLocale} {
		if l == "" {
			continue
		}
		b, err := fs.ReadFile(contract.Guidance, "guidance/"+l+"/"+strings.ToLower(kind)+".guidance.json")
		if err != nil {
			continue
		}
		var g GuideBundle
		if err := json.Unmarshal(b, &g); err != nil {
			return GuideBundle{}, false, fmt.Errorf("guidance %s/%s: %w", l, kind, err)
		}
		return g, true, nil
	}
	return GuideBundle{}, false, nil
}

type flowDoc struct {
	Spec struct {
		Steps []struct {
			Key    string      `json:"key"`
			Title  string      `json:"title"`
			Guide  string      `json:"guide"`
			Fields []flowField `json:"fields"`
			Links  []struct {
				Kind  string    `json:"kind"`
				Path  string    `json:"path"`
				When  *flowWhen `json:"when"`
				Check string    `json:"check"`
			} `json:"links"`
		} `json:"steps"`
	} `json:"spec"`
}

type flowField struct {
	Path    string      `json:"path"`
	Control string      `json:"control"`
	Hint    string      `json:"hint"`
	Checks  []string    `json:"checks"`
	When    *flowWhen   `json:"when"`
	Fields  []flowField `json:"fields"`
}

type flowWhen struct {
	Path string   `json:"path"`
	In   []string `json:"in"`
}

// applies says whether a when applies at level; with no level known,
// everything does.
func (w *flowWhen) applies(level string) bool {
	if w == nil || level == "" {
		return true
	}
	for _, v := range w.In {
		if v == level {
			return true
		}
	}
	return false
}

// Guide puts a kind's guide together for one level (for a Goal: goal,
// objective or outcome; "" for every level), in locale, with the
// organisation's records as candidates.
func (e *Engine) Guide(ctx context.Context, kind, level, locale string) (Guide, error) {
	raw, found, err := e.FlowJSON(kind)
	if err != nil {
		return Guide{}, err
	}
	if !found {
		return Guide{}, fmt.Errorf("%w: no guide for %s", ErrNotFound, kind)
	}
	var flow flowDoc
	if err := json.Unmarshal(raw, &flow); err != nil {
		return Guide{}, fmt.Errorf("flow %s: %w", kind, err)
	}
	words, _, err := GuideBundleFor(kind, locale)
	if err != nil {
		return Guide{}, err
	}
	g := Guide{Kind: kind, Locale: words.Locale, Definition: words.Definition, Level: level, Checks: words.Checks, Vocabulary: words.Vocabulary}
	if level != "" {
		g.LevelIs = words.Levels[level]
	} else {
		g.Levels = words.Levels
	}
	if kind == "Goal" {
		if s, err := e.GetSettings(ctx); err == nil && s.Purpose != nil {
			g.Purpose = s.Purpose
		}
	}
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
	g.Existing = e.candidates(l, kind, kind, "", "")
	if kind == "Goal" && level != "" {
		same := g.Existing[:0]
		for _, c := range g.Existing {
			if c.Detail == level {
				same = append(same, c)
			}
		}
		g.Existing = same
	}
	g.Template = e.template(kind, level)
	if g.Plan, err = e.Plan(ctx, kind, level, locale); err != nil {
		return Guide{}, err
	}
	below := map[string]string{"goal": "objective", "objective": "outcome"}[level]
	for _, st := range flow.Spec.Steps {
		step := GuideStep{Key: st.Key, Title: st.Title, Guide: st.Guide, Fields: []GuideField{}}
		if w, ok := words.Steps[st.Key]; ok {
			if w.Title != "" {
				step.Title = w.Title
			}
			if w.Guide != "" {
				step.Guide = w.Guide
			}
		}
		var add func(prefix string, fields []flowField)
		add = func(prefix string, fields []flowField) {
			for _, f := range fields {
				if !f.When.applies(level) {
					continue
				}
				path := prefix + f.Path
				gf := GuideField{Path: path, Control: f.Control, Checks: f.Checks, Guide: f.Hint, Required: e.requiredAt(kind, path) || anyBlocking(f.Checks)}
				gf.MaxLength, gf.Values, gf.Format, gf.Shape = e.fieldFacts(kind, path)
				if w, ok := words.Fields[path]; ok {
					if lw, ok := w.Levels[level]; ok && level != "" {
						w = lw
					}
					gf.Guide, gf.Good, gf.Poor = w.Guide, w.Good, w.Poor
				}
				if ref := e.refKindAt(kind, path); ref != "" && ref != "*" {
					gf.References = ref
					gf.Candidates = e.candidates(l, ref, kind, path, level)
				}
				step.Fields = append(step.Fields, gf)
				if len(f.Fields) > 0 {
					add(path+"/-", f.Fields)
				}
			}
		}
		add("", st.Fields)
		for _, ln := range st.Links {
			if !ln.When.applies(level) {
				continue
			}
			link := GuideLink{Kind: ln.Kind, Path: ln.Path, Check: ln.Check}
			if w, ok := words.Links[ln.Kind+":"+ln.Path]; ok {
				link.Ask, link.IfNone = w.Ask, w.IfNone
			}
			link.Candidates = e.candidates(l, ln.Kind, "", "", "")
			// The goals under this one are of the level below it.
			if ln.Kind == "Goal" && ln.Path == "/spec/parent" {
				under := link.Candidates[:0]
				for _, c := range link.Candidates {
					if c.Detail == below {
						under = append(under, c)
					}
				}
				link.Candidates = under
			}
			step.Links = append(step.Links, link)
		}
		g.Steps = append(g.Steps, step)
	}
	return g, nil
}

// refKindAt is the kind a field at path references, from the schema's
// x-cartograph-ref; "" when it references none.
func (e *Engine) refKindAt(kind, path string) string {
	tokens := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, r := range e.refRules[kind] {
		if len(r.path) != len(tokens) {
			continue
		}
		match := true
		for i, step := range r.path {
			if step.wildcard {
				if tokens[i] != "-" {
					match = false
				}
			} else if step.prop != tokens[i] {
				match = false
			}
		}
		if match {
			return r.kind
		}
	}
	return ""
}

// candidates lists the organisation's records of kind ref, by name. A
// goal's parent lists only the level above (an outcome's parent is an
// objective); goals show their level, gaps their statement.
func (e *Engine) candidates(l *lookup, ref, kind, path, level string) []Candidate {
	docs, err := l.Documents(ref)
	if err != nil {
		return nil
	}
	parentLevel := map[string]string{"objective": "goal", "outcome": "objective"}
	out := []Candidate{}
	for id, doc := range docs {
		spec, _ := doc["spec"].(map[string]any)
		c := Candidate{ID: id, Name: docName(doc, id)}
		switch ref {
		case "Goal":
			lv, _ := spec["level"].(string)
			if kind == "Goal" && path == "/spec/parent" && level != "" && lv != parentLevel[level] {
				continue
			}
			c.Detail = lv
			c.Under, _ = spec["parent"].(string)
		case "Gap":
			c.Detail, _ = spec["statement"].(string)
		case "Resource":
			c.Detail, _ = spec["category"].(string)
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) > maxCandidates {
		out = out[:maxCandidates]
	}
	return out
}

// template is a kind's manifest with its envelope and every required spec
// field, empty, to fill in; a Goal's level is set when known.
func (e *Engine) template(kind, level string) map[string]any {
	spec := map[string]any{}
	if b, err := fs.ReadFile(contract.Schemas, "schemas/"+strings.ToLower(kind)+".schema.json"); err == nil {
		var schema struct {
			Properties struct {
				Spec struct {
					Required   []string                   `json:"required"`
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"spec"`
			} `json:"properties"`
		}
		if json.Unmarshal(b, &schema) == nil {
			for _, name := range schema.Properties.Spec.Required {
				spec[name] = nil
			}
		}
	}
	if kind == "Goal" && level != "" {
		spec["level"] = level
	}
	// A kind there is one of in a workspace has its id already: an agent
	// that guessed another had a draft no save would ever take.
	var id any
	if singletons[kind] {
		id = "default"
	}
	return map[string]any{
		"apiVersion": "cartograph/v1",
		"kind":       kind,
		"metadata":   map[string]any{"id": id, "name": nil},
		"spec":       spec,
	}
}

// TaxonomyEntry is one kind as a document is mapped onto it: what it is,
// its levels, and what plans often call it instead.
type TaxonomyEntry struct {
	Kind    string            `json:"kind"`
	Summary string            `json:"summary"`
	Example string            `json:"example,omitempty"`
	Levels  map[string]string `json:"levels,omitempty"`
	KnownAs []string          `json:"knownAs,omitempty"`
	// Names are the kinds it may name, which come before it in the order
	// of work; a register names only other registers.
	Names []string `json:"names,omitempty"`
	// Register is true for a root kind, added when a field asks for one.
	Register bool `json:"register,omitempty"`
}

// Taxonomy is every kind a person defines, in the order of work (the
// stages from the purpose down, then the registers), with what it is,
// what it names and what documents call it, so whatever a document says
// is recorded as the kind it is, by its definition, not by its label, and
// in an order where everything it names is already there.
func (e *Engine) Taxonomy(locale string) ([]TaxonomyEntry, error) {
	var order []string
	seen := map[string]bool{}
	for _, s := range stages {
		if !seen[s.Kind] {
			seen[s.Kind] = true
			order = append(order, s.Kind)
		}
	}
	order = append(order, registers...)
	var out []TaxonomyEntry
	for _, kind := range order {
		b, ok, err := GuideBundleFor(kind, locale)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		entry := TaxonomyEntry{Kind: kind, Summary: b.Summary, Example: b.Example, Levels: b.Levels, KnownAs: b.KnownAs, Register: rank(kind, "") < len(registers)}
		named := map[string]bool{}
		for _, r := range e.refRules[kind] {
			if r.kind != "*" && r.kind != kind && !named[r.kind] {
				named[r.kind] = true
				entry.Names = append(entry.Names, r.kind)
			}
		}
		sort.Slice(entry.Names, func(i, j int) bool { return rank(entry.Names[i], "") < rank(entry.Names[j], "") })
		out = append(out, entry)
	}
	return out, nil
}

// requiredAt reports whether the schema requires the field at path: the
// object holding it lists it as required. A field inside a list item
// (/spec/deliverables/-/name) is required of every item, though the list
// itself may be optional.
func (e *Engine) requiredAt(kind, path string) bool {
	spec, ok := kinds.ByName(kind)
	if !ok {
		return false
	}
	w := shapeWalker{all: e.schemas.raw, keys: map[string]string{}, seen: map[string]bool{}}
	node, file := w.resolve(e.schemas.raw[spec.SchemaFile], spec.SchemaFile, "")
	tokens := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, t := range tokens {
		if node == nil {
			return false
		}
		if t == "-" {
			items, _ := node["items"].(map[string]any)
			node, file = w.resolve(items, file, "")
			continue
		}
		if i == len(tokens)-1 {
			required, _ := node["required"].([]any)
			for _, r := range required {
				if r == t {
					return true
				}
			}
			return false
		}
		props, _ := node["properties"].(map[string]any)
		next, _ := props[t].(map[string]any)
		node, file = w.resolve(next, file, "")
	}
	return false
}

// singletons are the kinds a workspace holds one of, always under the id
// "default" (their kind rules refuse any other).
var singletons = map[string]bool{"Purpose": true, "Settings": true}

// schemaAt is the schema node at a guide path, its $refs followed, with
// the name of the last $def it came through ("Ref", "Horizon").
func (e *Engine) schemaAt(kind, path string) (map[string]any, string, string) {
	spec, ok := kinds.ByName(kind)
	if !ok {
		return nil, "", ""
	}
	w := shapeWalker{all: e.schemas.raw, keys: map[string]string{}, seen: map[string]bool{}}
	node, file := e.schemas.raw[spec.SchemaFile], spec.SchemaFile
	def := ""
	step := func(n map[string]any) {
		if ref, _ := n["$ref"].(string); ref != "" {
			def = ref[strings.LastIndex(ref, "/")+1:]
		} else {
			def = ""
		}
		node, file = w.resolve(n, file, "")
	}
	step(node)
	for _, t := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if node == nil {
			return nil, "", ""
		}
		var next map[string]any
		if t == "-" {
			next, _ = node["items"].(map[string]any)
		} else {
			props, _ := node["properties"].(map[string]any)
			next, _ = props[t].(map[string]any)
		}
		if next == nil {
			return nil, "", ""
		}
		step(next)
	}
	return node, file, def
}

// fieldFacts reads what the schema holds a field to: its longest, its
// allowed values, how a date in it is written, and, for anything that is
// not plain text or a number, the JSON it is written as.
func (e *Engine) fieldFacts(kind, path string) (maxLength int, values []any, format, shape string) {
	node, file, def := e.schemaAt(kind, path)
	if node == nil {
		return 0, nil, "", ""
	}
	if n, ok := node["maxLength"].(float64); ok {
		maxLength = int(n)
	}
	if v, ok := node["enum"].([]any); ok {
		values = v
	} else if v, ok := node["x-cartograph-enum"].([]any); ok {
		values = v
	}
	format = formatOf(node)
	if def == "Ref" || isStructured(node) {
		w := shapeWalker{all: e.schemas.raw, keys: map[string]string{}, seen: map[string]bool{}}
		shape = shapeText(w, node, file, def, 0)
	}
	return maxLength, values, format, shape
}

func isStructured(n map[string]any) bool {
	t, _ := n["type"].(string)
	_, props := n["properties"]
	_, one := n["oneOf"]
	_, any := n["anyOf"]
	return t == "object" || t == "array" || props || one || any
}

// formatOf names how a dated string is written, from its pattern.
func formatOf(n map[string]any) string {
	pattern, _ := n["pattern"].(string)
	switch {
	case pattern == "^[0-9]{4}-(0[1-9]|1[0-2])$":
		return "YYYY-MM"
	case pattern == "^[0-9]{4}(-(0[1-9]|1[0-2]))?$":
		return "YYYY or YYYY-MM"
	case strings.HasPrefix(pattern, "^[0-9]{4}-(0[1-9]|1[0-2])(-"):
		return "YYYY-MM-DD or YYYY-MM"
	}
	if f, _ := n["format"].(string); f == "date" {
		return "YYYY-MM-DD"
	}
	return ""
}

// shapeText writes a compact example of the JSON a field takes: an
// object's properties with what each holds, a list's item in brackets,
// alternatives joined by " | ".
func shapeText(w shapeWalker, n map[string]any, file, def string, depth int) string {
	if def == "Ref" {
		return `{"kind":"<Kind>","id":"<id>"} | {"local":"resources","id":"<role id>"} | {"external":"<name, outside the workspace>"}`
	}
	if depth > 2 {
		return "…"
	}
	for _, k := range []string{"oneOf", "anyOf"} {
		if alts, ok := n[k].([]any); ok && n["properties"] == nil {
			var parts []string
			for _, a := range alts {
				if m, ok := a.(map[string]any); ok {
					sub, subFile := w.resolve(m, file, "")
					parts = append(parts, shapeText(w, sub, subFile, refName(m), depth+1))
				}
			}
			return strings.Join(parts, " | ")
		}
	}
	if items, ok := n["items"].(map[string]any); ok {
		if ref, _ := items["x-cartograph-ref"].(string); ref != "" && ref != "*" {
			return `["<` + ref + ` id>", …]`
		}
		sub, subFile := w.resolve(items, file, "")
		return "[" + shapeText(w, sub, subFile, refName(items), depth+1) + ", …]"
	}
	if props, ok := n["properties"].(map[string]any); ok {
		names := make([]string, 0, len(props))
		for k := range props {
			names = append(names, k)
		}
		sort.Strings(names)
		req := map[string]bool{}
		for _, r := range asSlice(n["required"]) {
			if s, ok := r.(string); ok {
				req[s] = true
			}
		}
		// Required properties first, so the shortest valid value reads first.
		sort.SliceStable(names, func(i, j int) bool { return req[names[i]] && !req[names[j]] })
		var parts []string
		for _, k := range names {
			p, _ := props[k].(map[string]any)
			if p == nil || strings.HasPrefix(fmt.Sprint(p["description"]), "Deprecated") {
				continue
			}
			v := ""
			if ref, _ := p["x-cartograph-ref"].(string); ref != "" && ref != "*" {
				v = `"<` + ref + ` id>"`
			} else {
				sub, subFile := w.resolve(p, file, "")
				v = shapeText(w, sub, subFile, refName(p), depth+1)
			}
			if !req[k] {
				k += "?"
			}
			parts = append(parts, fmt.Sprintf("%q:%s", k, v))
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	if v, ok := n["enum"].([]any); ok {
		// A long list (currencies) is the field's own values; the shape
		// only says one of them goes here.
		if len(v) > 12 {
			return `"<one of its values>"`
		}
		var vals []string
		for _, x := range v {
			vals = append(vals, fmt.Sprint(x))
		}
		return `"` + strings.Join(vals, "|") + `"`
	}
	if ref, _ := n["x-cartograph-ref"].(string); ref != "" {
		return `"<` + ref + ` id>"`
	}
	switch t, _ := n["type"].(string); t {
	case "integer", "number":
		return "0"
	case "boolean":
		return "true"
	}
	if f := formatOf(n); f != "" {
		return `"` + f + `"`
	}
	return `"text"`
}

func refName(n map[string]any) string {
	ref, _ := n["$ref"].(string)
	if ref == "" {
		return ""
	}
	return ref[strings.LastIndex(ref, "/")+1:]
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}
