package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Settings is the one singleton configuration manifest (kind Settings, id
// "default"), read from an optional settings.yaml at an import directory's
// root. Defaults apply when none was imported.
type Settings struct {
	GoalLevels       []string `json:"goalLevels"`
	ProjectLevelName string   `json:"projectLevelName"`
	Operator         string   `json:"operator"`
	Chromium         string   `json:"chromium"`
	// Examples a vault supplies per field, in its own words (TAXONOMY.md
	// D20). Empty when the vault supplies none.
	Examples map[string][]string `json:"examples,omitempty"`
	// Why everything below exists: the vision and mission (D24).
	Purpose *Purpose `json:"purpose,omitempty"`
}

// Purpose is the vault's vision and mission, stated once above every goal.
type Purpose struct {
	// The organisation it is the purpose of, by name (D37).
	Organisation string `json:"organisation,omitempty" yaml:"organisation"`
	Vision       string `json:"vision,omitempty" yaml:"vision"`
	Mission      string `json:"mission,omitempty" yaml:"mission"`
	Source       string `json:"source,omitempty" yaml:"source"`
}

// defaultSettings is what applies when no Settings manifest exists.
func defaultSettings() Settings {
	return Settings{
		GoalLevels:       []string{"Goal", "Objective", "Outcome"},
		ProjectLevelName: "Project",
		Operator:         "local",
		// Left empty on purpose: the renderer resolves the browser from
		// the CHROMIUM environment variable or the PATH when a vault does
		// not name one, which travels between machines.
		Chromium: "",
	}
}

// GetSettings returns the current Settings, defaulted where the imported
// document (if any) leaves a field empty. Its Purpose is the Purpose
// manifest's where there is one, else what Settings held before 2.7.0
// (ADR 0020), so every reader of the vision and mission reads one place.
func (e *Engine) GetSettings(ctx context.Context) (Settings, error) {
	out, err := e.settingsOnly(ctx)
	if err != nil {
		return Settings{}, err
	}
	p, found, err := e.statedPurpose(ctx)
	if err != nil {
		return Settings{}, err
	}
	if found {
		out.Purpose = p
	}
	return out, nil
}

// statedPurpose reads the Purpose manifest, its draft where one is being
// checked with the work on ctx (a change set's, a proposal's), so a goal
// drafted beside the purpose is judged against it.
func (e *Engine) statedPurpose(ctx context.Context) (*Purpose, bool, error) {
	doc, found, err := e.currentDoc(ctx, "Purpose", "default")
	if err != nil || !found {
		return nil, false, err
	}
	spec, _ := doc["spec"].(map[string]any)
	text := func(k string) string {
		s, _ := spec[k].(string)
		return s
	}
	return &Purpose{Organisation: text("organisation"), Vision: text("vision"), Mission: text("mission"), Source: text("source")}, true, nil
}

func (e *Engine) settingsOnly(ctx context.Context) (Settings, error) {
	out := defaultSettings()
	v, found, err := e.manifests.GetCurrent(ctx, "Settings", "default")
	if err != nil {
		return Settings{}, err
	}
	if !found {
		return out, nil
	}
	var doc struct {
		Spec struct {
			GoalLevels       []string            `yaml:"goalLevels"`
			ProjectLevelName string              `yaml:"projectLevelName"`
			Operator         string              `yaml:"operator"`
			Chromium         string              `yaml:"chromium"`
			Examples         map[string][]string `yaml:"examples"`
			Purpose          *Purpose            `yaml:"purpose"`
		} `yaml:"spec"`
	}
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		return Settings{}, fmt.Errorf("parse settings: %w", err)
	}
	if len(doc.Spec.GoalLevels) > 0 {
		out.GoalLevels = doc.Spec.GoalLevels
	}
	if doc.Spec.ProjectLevelName != "" {
		out.ProjectLevelName = doc.Spec.ProjectLevelName
	}
	if doc.Spec.Operator != "" {
		out.Operator = doc.Spec.Operator
	}
	if doc.Spec.Chromium != "" {
		out.Chromium = doc.Spec.Chromium
	}
	out.Examples = doc.Spec.Examples
	out.Purpose = doc.Spec.Purpose
	return out, nil
}

// DeleteGoal deletes a goal (goals are easily mutable), allowed only
// when nothing currently references it (a child goal's own parent, a
// Project/Programme/Operation/KPI's own alignment, or anything else the
// generic reference index tracks). Refused with a *ValidationError listing
// every referencing manifest when the goal is not empty of references;
// ErrNotFound when the goal does not exist. Calls Exclude to remove from
// vault.yaml while keeping the file.
func (e *Engine) DeleteGoal(ctx context.Context, id, actor, reason string) error {
	if err := refuseAgent(ctx); err != nil {
		return err
	}
	v, found, err := e.manifests.GetCurrent(ctx, "Goal", id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: Goal/%s", ErrNotFound, id)
	}
	if err := e.guardDoc(ctx, "Goal", id, nil); err != nil {
		return err
	}
	refs, err := e.manifests.ListReferencing(ctx, "Goal", id)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		problems := make([]Problem, len(refs))
		for i, r := range refs {
			problems[i] = Problem{Message: fmt.Sprintf("%s/%s (%s) still references this goal", r.Kind, r.ID, r.Name)}
		}
		return &ValidationError{Problems: problems}
	}

	// Extract goal name from the YAML for the exclusion record
	var doc struct {
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		doc.Metadata.Name = id
	}

	// Exclude the goal from vault.yaml
	return e.manifests.Exclude(ctx, "Goal", id, doc.Metadata.Name, reason, actor)
}

// GoalAligned is how many manifests of each kind reference a goal, by
// referencing kind. Anything that references a goal counts as aligned to
// it; kinds outside this set are not counted here.
type GoalAligned struct {
	Projects   int `json:"projects"`
	Programmes int `json:"programmes"`
	Operations int `json:"operations"`
	KPIs       int `json:"kpis"`
}

// GoalNode is one record in the tree, with its objectives nested as
// Children (for a goal) or its outcomes nested as Children (for an
// objective), or no children (for an outcome).
type GoalNode struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Level      string      `json:"level"`
	Parent     string      `json:"parent,omitempty"`
	KeyResults int         `json:"keyResults"`
	Aligned    GoalAligned `json:"aligned"`
	Children   []*GoalNode `json:"children"`
	// What the Strategy view reads (D24): the statement, why it matters,
	// what else it leads to, and the gaps it would close.
	Objective     string     `json:"objective,omitempty"`
	Why           string     `json:"why,omitempty"`
	ContributesTo []GoalLink `json:"contributesTo,omitempty"`
	Gaps          []GoalGap  `json:"gaps,omitempty"`
	// Which SMART letters it meets (D25), for a mark on every card.
	Smart Smart `json:"smart"`
	// The role accountable for it, and the period it covers (its own or
	// its parent's), so every card carries its context (D26).
	Owner   string   `json:"owner,omitempty"`
	Horizon *Horizon `json:"horizon,omitempty"`
}

// GoalLink is an outcome leading to a higher objective, and why.
type GoalLink struct {
	Goal    string `json:"goal" yaml:"goal"`
	Because string `json:"because,omitempty" yaml:"because"`
}

// GoalGap is a gap that names this goal as the outcome that closes it.
type GoalGap struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Current string `json:"current,omitempty"`
	Desired string `json:"desired,omitempty"`
}

// GoalTree is the whole tree: every level's display name, and the root
// goals, each carrying its objectives as Children, and each objective
// carrying its outcomes as Children.
type GoalTree struct {
	Levels []string    `json:"levels"`
	Nodes  []*GoalNode `json:"nodes"`
	// Unplaced are objectives and outcomes with no parent yet (TAXONOMY.md
	// D35), each with what sits under it.
	Unplaced []*GoalNode `json:"unplaced"`
}

type goalDoc struct {
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Level         string     `yaml:"level"`
		Parent        string     `yaml:"parent"`
		Objective     string     `yaml:"objective"`
		WhyItMatters  string     `yaml:"whyItMatters"`
		ContributesTo []GoalLink `yaml:"contributesTo"`
		KeyResults    []any      `yaml:"keyResults"`
		Evidence      string     `yaml:"evidence"`
	} `yaml:"spec"`
}

// GoalTree computes the goal tree from every current Goal manifest plus the
// reference index (which counts as aligned to a goal). It reads goals,
// gaps, indicators and references once each, however many goals there
// are (docs/adr/0012).
func (e *Engine) GoalTree(ctx context.Context) (GoalTree, error) {
	goalVersions, err := currentOfKind(ctx, e.manifests, "Goal")
	if err != nil {
		return GoalTree{}, err
	}
	ids := make([]string, 0, len(goalVersions))
	goals := make(map[string]map[string]any, len(goalVersions))
	texts := make(map[string][]byte, len(goalVersions))
	for _, v := range goalVersions {
		var raw map[string]any
		if err := e.codec.DecodeInto(v.YAML, &raw); err != nil {
			return GoalTree{}, fmt.Errorf("parse Goal/%s: %w", v.ID, err)
		}
		ids = append(ids, v.ID)
		goals[v.ID] = raw
		texts[v.ID] = v.YAML
	}

	// Gaps name the outcomes that would close them; read back here so a
	// goal never has to point below itself (D9, D24).
	gapsByGoal := map[string][]GoalGap{}
	if gapVersions, err := currentOfKind(ctx, e.manifests, "Gap"); err == nil {
		for _, gv := range gapVersions {
			var gdoc struct {
				Metadata struct {
					Name string `yaml:"name"`
				} `yaml:"metadata"`
				Spec struct {
					Current  string   `yaml:"current"`
					Desired  string   `yaml:"desired"`
					Outcomes []string `yaml:"outcomes"`
				} `yaml:"spec"`
			}
			if e.codec.DecodeInto(gv.YAML, &gdoc) != nil {
				continue
			}
			for _, o := range gdoc.Spec.Outcomes {
				gapsByGoal[o] = append(gapsByGoal[o], GoalGap{ID: gv.ID, Name: gdoc.Metadata.Name,
					Current: gdoc.Spec.Current, Desired: gdoc.Spec.Desired})
			}
		}
	}

	referencing, err := referencingKind(ctx, e.manifests, "Goal", ids)
	if err != nil {
		return GoalTree{}, err
	}
	kpiSpecs := map[string]map[string]any{}
	if kpiVersions, err := currentOfKind(ctx, e.manifests, "KPI"); err == nil {
		for _, kv := range kpiVersions {
			var kdoc map[string]any
			if e.codec.DecodeInto(kv.YAML, &kdoc) != nil {
				continue
			}
			if sp, ok := kdoc["spec"].(map[string]any); ok {
				kpiSpecs[kv.ID] = sp
			}
		}
	}
	read := e.loadedGoals(ctx, goals)

	nodesByID := make(map[string]*GoalNode, len(ids))
	for _, id := range ids {
		var doc goalDoc
		if err := e.codec.DecodeInto(texts[id], &doc); err != nil {
			return GoalTree{}, fmt.Errorf("parse Goal/%s: %w", id, err)
		}
		aligned := GoalAligned{}
		var kpis []map[string]any
		for _, r := range referencing[id] {
			switch r.Kind {
			case "Project":
				aligned.Projects++
			case "Programme":
				aligned.Programmes++
			case "Operation":
				aligned.Operations++
			case "KPI":
				aligned.KPIs++
				if sp, ok := kpiSpecs[r.ID]; ok {
					kpis = append(kpis, sp)
				}
			}
		}
		rawSpec, _ := goals[id]["spec"].(map[string]any)
		if rawSpec == nil {
			rawSpec = map[string]any{}
		}
		horizon := e.effectiveHorizon(read, rawSpec)
		smart, _ := e.goalSmart(read, rawSpec, kpis, horizon)
		owner, _ := rawSpec["owner"].(string)
		nodesByID[id] = &GoalNode{
			Smart:      smart,
			Owner:      owner,
			Horizon:    horizon,
			ID:         id,
			Name:       doc.Metadata.Name,
			Level:      doc.Spec.Level,
			Parent:     doc.Spec.Parent,
			KeyResults: len(doc.Spec.KeyResults),
			Aligned:    aligned,
			Children:   []*GoalNode{},

			Objective:     doc.Spec.Objective,
			Why:           doc.Spec.WhyItMatters,
			ContributesTo: doc.Spec.ContributesTo,
			Gaps:          gapsByGoal[id],
		}
	}

	var roots, unplaced []*GoalNode
	for _, id := range ids {
		n, ok := nodesByID[id]
		if !ok {
			continue
		}
		if n.Parent != "" {
			if p, ok := nodesByID[n.Parent]; ok {
				p.Children = append(p.Children, n)
				continue
			}
		}
		// Only a goal is a root: an objective or outcome without a parent
		// is unplaced, not a goal (TAXONOMY.md D35).
		if n.Level != "goal" && n.Parent == "" {
			unplaced = append(unplaced, n)
			continue
		}
		roots = append(roots, n)
	}

	sortByName := func(ns []*GoalNode) {
		sort.Slice(ns, func(i, j int) bool { return ns[i].Name < ns[j].Name })
	}
	sortByName(roots)
	for _, n := range roots {
		sortByName(n.Children)
	}
	sortByName(unplaced)

	settings, err := e.GetSettings(ctx)
	if err != nil {
		return GoalTree{}, err
	}

	if roots == nil {
		roots = []*GoalNode{}
	}
	if unplaced == nil {
		unplaced = []*GoalNode{}
	}
	return GoalTree{Levels: settings.GoalLevels, Nodes: roots, Unplaced: unplaced}, nil
}

// GoalCheckFix names which section of the goal editor addresses a check.
type GoalCheckFix struct {
	Section string `json:"section"`
}

// GoalCheck is one assessability check on a Goal. Checks never block a
// save; they only report.
type GoalCheck struct {
	ID      string        `json:"id"`
	State   string        `json:"state"` // ok, warn
	Message string        `json:"message"`
	Fix     *GoalCheckFix `json:"fix,omitempty"`
}

const (
	goalCheckOK   = "ok"
	goalCheckWarn = "warn"
)

// GoalChecks reports whether a goal can be assessed: a title, a qualitative
// objective, a goal parent for an objective or an objective parent for
// an outcome, one to three key results each with a baseline (or an
// admitted unknown) and a target, scoring evidence, and at least one KPI
// referencing it. No check mentions a team or a cycle: the
// goal is the root of the dependency tree and never references a lower
// component, so nothing here reads spec.team or spec.reviewCycle, both
// removed from the schema.
func (e *Engine) GoalChecks(ctx context.Context, id string) ([]GoalCheck, error) {
	v, found, err := e.manifests.GetCurrent(ctx, "Goal", id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: Goal/%s", ErrNotFound, id)
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		return nil, fmt.Errorf("parse Goal/%s: %w", id, err)
	}
	return e.goalChecksOf(ctx, id, doc)
}

// goalChecksOf computes the checks for a goal's document, saved or not: a
// draft, or what an agent proposes.
func (e *Engine) goalChecksOf(ctx context.Context, id string, doc map[string]any) ([]GoalCheck, error) {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		spec = map[string]any{}
	}
	level, _ := spec["level"].(string)

	// The five SMART letters first (D25), then the two checks SMART does
	// not cover: focus (OKR's one to three key results) and the links up.
	kpis, err := e.alignedKPISpecs(ctx, id)
	if err != nil {
		return nil, err
	}
	read := e.storedGoals(ctx)
	horizon := e.effectiveHorizon(read, spec)
	_, checks := e.goalSmart(read, spec, kpis, horizon)
	checks = append(checks, e.contextChecks(ctx, spec, horizon)...)

	// The strategy is read top-down (D24): a goal is met through the
	// objectives under it, an objective through the outcomes under it. One
	// with nothing under it says what is wanted and not how anyone will
	// see it happen.
	var under GoalCheck
	below := ""
	switch level {
	case "goal":
		under, below = GoalCheck{ID: "has-objectives"}, "objective"
	case "objective":
		under, below = GoalCheck{ID: "has-outcomes"}, "outcome"
	}
	if below != "" {
		children, err := e.goalsUnder(ctx, id, below)
		if err != nil {
			return nil, err
		}
		if len(children) > 0 {
			under.State, under.Message = goalCheckOK, fmt.Sprintf("%d %s%s under it.", len(children), below, plural(len(children)))
		} else {
			under.State, under.Message = goalCheckWarn, fmt.Sprintf("No %s under it yet: say what will show it is met.", below)
		}
		checks = append(checks, under)
	}
	// And each outcome under an objective closes a gap, so the objective
	// answers a shortfall someone measured, not only an intention.
	if level == "objective" {
		ids, err := e.goalIDsUnder(ctx, id, "outcome")
		if err != nil {
			return nil, err
		}
		var unanswered []string
		for _, child := range ids {
			closing, err := e.gapsClosing(ctx, child.id)
			if err != nil {
				return nil, err
			}
			if len(closing) == 0 {
				unanswered = append(unanswered, child.name)
			}
		}
		switch {
		case len(ids) == 0:
		case len(unanswered) == 0:
			checks = append(checks, GoalCheck{ID: "outcomes-close-gaps", State: goalCheckOK, Message: "Every outcome under it closes a gap."})
		default:
			checks = append(checks, GoalCheck{ID: "outcomes-close-gaps", State: goalCheckWarn,
				Message: "Closes no gap yet: " + englishList(unanswered) + ". Name the gap each closes, or define it."})
		}
	}

	// An outcome closes a gap (D24): which gap, and so what shortfall it
	// answers. The link is the gap's, so this reads the gaps that name it.
	if level == "outcome" {
		closing, err := e.gapsClosing(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(closing) > 0 {
			checks = append(checks, GoalCheck{ID: "closes-gap", State: goalCheckOK,
				Message: "Closes " + englishList(closing) + "."})
		} else {
			checks = append(checks, GoalCheck{ID: "closes-gap", State: goalCheckWarn,
				Message: "No gap names this outcome yet. Write the gap it closes next; the gap names it."})
		}
	}

	if krs, _ := spec["keyResults"].([]any); len(krs) > 3 {
		checks = append(checks, GoalCheck{
			ID: "key-results-count", State: goalCheckWarn,
			Message: fmt.Sprintf("%d key results. One to three keep focus.", len(krs)),
			Fix:     &GoalCheckFix{Section: "keyResults"},
		})
	}

	// What else this outcome leads to, and why (D24). Only an outcome
	// contributes; each link points up at an objective or a goal, never
	// at the parent it is already filed under, and says why.
	if links, _ := spec["contributesTo"].([]any); len(links) > 0 {
		parentID, _ := spec["parent"].(string)
		var bad, unexplained int
		for _, l := range links {
			m, _ := l.(map[string]any)
			target, _ := m["goal"].(string)
			because, _ := m["because"].(string)
			ok := false
			if target != "" && target != parentID && target != id {
				if tv, tfound, err := e.manifests.GetCurrent(ctx, "Goal", target); err == nil && tfound {
					var tdoc goalDoc
					if e.codec.DecodeInto(tv.YAML, &tdoc) == nil && (tdoc.Spec.Level == "objective" || tdoc.Spec.Level == "goal") {
						ok = true
					}
				}
			}
			if !ok {
				bad++
			}
			if strings.TrimSpace(because) == "" {
				unexplained++
			}
		}
		switch {
		case level != "outcome":
			checks = append(checks, GoalCheck{ID: "contributes-to", State: goalCheckWarn,
				Message: "Only an outcome leads to other objectives.", Fix: &GoalCheckFix{Section: "contributesTo"}})
		case bad > 0:
			checks = append(checks, GoalCheck{ID: "contributes-to", State: goalCheckWarn,
				Message: "Each link must point at a higher objective other than the one this is filed under.",
				Fix:     &GoalCheckFix{Section: "contributesTo"}})
		case unexplained > 0:
			checks = append(checks, GoalCheck{ID: "contributes-to", State: goalCheckWarn,
				Message: map[bool]string{true: "One link has no reason yet.", false: fmt.Sprintf("%d links have no reason yet.", unexplained)}[unexplained == 1],
				Fix:     &GoalCheckFix{Section: "contributesTo"}})
		default:
			checks = append(checks, GoalCheck{ID: "contributes-to", State: goalCheckOK,
				Message: fmt.Sprintf("Also leads to %d other objective%s, with reasons.", len(links), plural(len(links))),
				Fix:     &GoalCheckFix{Section: "contributesTo"}})
		}
	}

	// What a decision model makes of the statement, where there is one.
	for _, j := range e.judgedChecks(ctx, "Goal", doc) {
		state := goalCheckOK
		if j.State != programmeCheckOK {
			state = goalCheckWarn
		}
		checks = append(checks, GoalCheck{ID: j.ID, State: state, Message: j.Message})
	}
	// An objective or outcome left unplaced (TAXONOMY.md D35) says so
	// plainly; any other placeholder is listed as everywhere else.
	if parent, _ := spec["parent"].(string); level != "goal" && parent == "" {
		above := map[string]string{"objective": "a goal", "outcome": "an objective"}[level]
		checks = append(checks, GoalCheck{ID: "placed", State: goalCheckWarn, Message: fmt.Sprintf("Not placed yet: place it under %s.", above), Fix: &GoalCheckFix{Section: "aim"}})
	}
	if pc, ok := pendingCheck(withoutPending(doc, "/spec/parent")); ok {
		checks = append(checks, GoalCheck{ID: pc.ID, State: goalCheckWarn, Message: pc.Message})
	}
	return checks, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// manifestName extracts metadata.name from a manifest's YAML text, for
// composing a check's message from a referenced manifest's display name.
func (e *Engine) manifestName(y []byte) string {
	var doc struct {
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	if err := e.codec.DecodeInto(y, &doc); err != nil {
		return ""
	}
	return doc.Metadata.Name
}

// contextChecks are the two lines SMART does not cover: who owns the aim,
// and whether its horizon sits inside its parent's (D26).
func (e *Engine) contextChecks(ctx context.Context, spec map[string]any, horizon *Horizon) []GoalCheck {
	var out []GoalCheck
	if owner, _ := spec["owner"].(string); owner != "" {
		name := owner
		if v, found, err := e.manifests.GetCurrent(ctx, "Resource", owner); err == nil && found {
			if n := e.manifestName(v.YAML); n != "" {
				name = n
			}
		}
		out = append(out, GoalCheck{ID: "owner", State: goalCheckOK, Message: "Owner: " + name + ".", Fix: &GoalCheckFix{Section: "owner"}})
	} else {
		out = append(out, GoalCheck{ID: "owner", State: goalCheckWarn, Message: "No owner yet.", Fix: &GoalCheckFix{Section: "owner"}})
	}

	level, _ := spec["level"].(string)
	own := horizonOf(spec)
	switch {
	case horizon == nil && level == "goal":
		out = append(out, GoalCheck{ID: "horizon", State: goalCheckWarn, Message: "No horizon yet.", Fix: &GoalCheckFix{Section: "horizon"}})
	case horizon == nil:
		out = append(out, GoalCheck{ID: "horizon", State: goalCheckWarn, Message: "No horizon here or above it yet.", Fix: &GoalCheckFix{Section: "horizon"}})
	case own != nil && own.Start > own.End:
		out = append(out, GoalCheck{ID: "horizon", State: goalCheckWarn, Message: "The horizon ends before it starts.", Fix: &GoalCheckFix{Section: "horizon"}})
	case own != nil:
		parentSpec := map[string]any{}
		if parent, _ := spec["parent"].(string); parent != "" {
			parentSpec["parent"] = parent
		}
		up := e.effectiveHorizon(e.storedGoals(ctx), parentSpec)
		if up != nil && (own.Start < up.Start || own.End > up.End) {
			out = append(out, GoalCheck{ID: "horizon", State: goalCheckWarn,
				Message: "The horizon runs outside the one above it, " + span(up) + ".", Fix: &GoalCheckFix{Section: "horizon"}})
		} else {
			out = append(out, GoalCheck{ID: "horizon", State: goalCheckOK, Message: "Horizon: " + span(own) + ".", Fix: &GoalCheckFix{Section: "horizon"}})
		}
	default:
		out = append(out, GoalCheck{ID: "horizon", State: goalCheckOK, Message: "Horizon: " + span(horizon) + ", from the aim above it.", Fix: &GoalCheckFix{Section: "horizon"}})
	}
	return out
}
