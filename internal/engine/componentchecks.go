package engine

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Components of a project (TAXONOMY.md D15).
//
// PMI's subproject and the World Bank's component rest on the same test:
// one sponsor, one budget, one objective, one authority. A project with
// parts that pass it is one project. When a part needs its own sponsor or
// its own budget, it is a separate project, and the pair is a programme.
//
// These checks say that, and advise only. Like programme membership (D1),
// the answer depends on another file: a parent whose sponsor changes today
// must not refuse a component saved yesterday.

// componentParent returns the id a project names as its parent.
func componentParent(spec map[string]any) string {
	alignment, _ := spec["alignment"].(map[string]any)
	parent, _ := alignment["partOf"].(string)
	return parent
}

// addComponentChecks runs the checks for a project that is a component of
// another, and for a project that has components of its own.
func (e *Engine) addComponentChecks(ctx context.Context, c checkAdder, id string, spec map[string]any) error {
	if parent := componentParent(spec); parent != "" {
		if err := e.addPartChecks(ctx, c, id, parent, spec); err != nil {
			return err
		}
	}

	legacy, err := e.componentsOf(ctx, id)
	if err != nil {
		return err
	}
	children := declaredComponents(spec)
	for _, l := range legacy {
		if r := (Ref{Kind: "Project", ID: l}); !containsRef(children, r) {
			children = append(children, r)
		}
	}
	if err := e.addLoopCheck(ctx, c, Ref{Kind: "Project", ID: id}, len(children) > 0); err != nil {
		return err
	}
	if len(children) == 0 {
		return nil
	}
	// A parent's key results are the development objective's indicators:
	// what the components add up to. Without one there is nothing to judge
	// the parts against together.
	if countKeyResults(spec) == 0 {
		c.add("components-add-up", "measures", phaseInitiation, checkWarn,
			fmt.Sprintf("Has %d component%s but no key result for them to add up to.", len(children), plural(len(children))))
	} else {
		c.add("components-add-up", "measures", phaseInitiation, checkOK,
			fmt.Sprintf("%d component%s, measured together by this project's key results.", len(children), plural(len(children))))
	}
	return nil
}

func (e *Engine) addPartChecks(ctx context.Context, c checkAdder, id, parentID string, spec map[string]any) error {
	if parentID == id {
		c.add("components-parent", "goals", phaseInitiation, checkBlock, "A project can't be a component of itself.")
		return nil
	}
	parentDoc, err := e.loadProjectDoc(ctx, parentID)
	if err != nil {
		// A parent that does not resolve is the reference check's to report.
		return nil
	}
	parentSpec, _ := parentDoc["spec"].(map[string]any)
	parentName := nameOf(parentDoc, parentID)

	if grand := componentParent(parentSpec); grand != "" {
		c.add("components-one-level", "goals", phaseInitiation, checkWarn,
			fmt.Sprintf("%s is itself a component. Name the project it belongs to instead.", parentName))
	}

	alignment, _ := spec["alignment"].(map[string]any)
	if programmes, _ := alignment["programmes"].([]any); len(programmes) > 0 {
		c.add("components-programmes", "goals", phaseInitiation, checkWarn,
			fmt.Sprintf("Belongs to the programmes %s is in. Remove them here.", parentName))
	}

	own := roleResources(spec, "sponsor")
	theirs := roleResources(parentSpec, "sponsor")
	ownFunds := fundingSources(spec)
	theirFunds := fundingSources(parentSpec)
	switch {
	case len(own) > 0 && len(theirs) > 0 && !subset(own, theirs):
		c.add("components-one-sponsor", "resources", phaseInitiation, checkWarn,
			fmt.Sprintf("Different sponsor from %s. With its own sponsor this is a separate project, and the two together are a programme.", parentName))
	case len(ownFunds) > 0 && len(theirFunds) > 0 && !subset(ownFunds, theirFunds):
		c.add("components-one-sponsor", "resources", phaseInitiation, checkWarn,
			fmt.Sprintf("Funded from a source %s doesn't use. With its own budget this is a separate project, and the two together are a programme.", parentName))
	default:
		c.add("components-one-sponsor", "resources", phaseInitiation, checkOK,
			fmt.Sprintf("Shares the sponsor and budget of %s.", parentName))
	}
	return nil
}

// componentsOf lists the projects that name id as their parent.
func (e *Engine) componentsOf(ctx context.Context, id string) ([]string, error) {
	summaries, err := e.List(ctx, "Project", Filter{}, false)
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	// Components drafted with it count as they stand, saved or not.
	for key, doc := range proposedDocs(ctx) {
		pid, ok := strings.CutPrefix(key, "Project/")
		if !ok || pid == id {
			continue
		}
		seen[pid] = true
		spec, _ := doc["spec"].(map[string]any)
		if componentParent(spec) == id {
			out = append(out, pid)
		}
	}
	for _, s := range summaries {
		if s.ID == id || seen[s.ID] {
			continue
		}
		doc, err := e.loadProjectDoc(ctx, s.ID)
		if err != nil {
			continue
		}
		spec, _ := doc["spec"].(map[string]any)
		if componentParent(spec) == id {
			out = append(out, s.ID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func nameOf(doc map[string]any, fallback string) string {
	md, _ := doc["metadata"].(map[string]any)
	if n, _ := md["name"].(string); n != "" {
		return n
	}
	return fallback
}

func countKeyResults(spec map[string]any) int {
	n := 0
	objectives, _ := spec["objectives"].([]any)
	for _, o := range objectives {
		om, _ := o.(map[string]any)
		krs, _ := om["keyResults"].([]any)
		n += len(krs)
	}
	return n
}

// roleResources lists the catalogue entries bound to a role.
func roleResources(spec map[string]any, role string) []string {
	var out []string
	for _, r := range resourcesWithRole(spec, role) {
		if res, _ := r["resource"].(string); res != "" {
			out = append(out, res)
		}
	}
	return out
}

func fundingSources(spec map[string]any) []string {
	var out []string
	funding, _ := spec["funding"].([]any)
	for _, f := range funding {
		fm, _ := f.(map[string]any)
		if s, _ := fm["source"].(string); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func subset(xs, of []string) bool {
	set := map[string]bool{}
	for _, s := range of {
		set[s] = true
	}
	for _, x := range xs {
		if !set[x] {
			return false
		}
	}
	return true
}

// declaredComponents lists the projects and programmes spec names as its
// components (TAXONOMY.md D46).
func declaredComponents(spec map[string]any) []Ref {
	var out []Ref
	list, _ := spec["components"].([]any)
	for _, c := range list {
		cm, _ := c.(map[string]any)
		k, _ := cm["kind"].(string)
		id, _ := cm["id"].(string)
		if r := (Ref{Kind: k, ID: id}); k != "" && id != "" && !containsRef(out, r) {
			out = append(out, r)
		}
	}
	return out
}

func containsRef(list []Ref, r Ref) bool {
	for _, x := range list {
		if x == r {
			return true
		}
	}
	return false
}

// loopOf is the loop of components self lies on, by name, or nothing.
func (e *Engine) loopOf(ctx context.Context, self Ref) ([]string, error) {
	g, err := e.Components(ctx)
	if err != nil {
		return nil, err
	}
	names := map[Ref]string{}
	for _, n := range g.Nodes {
		names[n.Ref] = n.Name
	}
	for _, loop := range g.Loops {
		if !containsRef(loop, self) {
			continue
		}
		// Told from self round to self, so it reads as this work's loop.
		at := 0
		for i, r := range loop[:len(loop)-1] {
			if r == self {
				at = i
			}
		}
		ring := loop[:len(loop)-1]
		var out []string
		for i := range len(ring) + 1 {
			out = append(out, names[ring[(at+i)%len(ring)]])
		}
		return out, nil
	}
	return nil, nil
}

// addLoopCheck says whether a project's components come back round to
// it. A loop has no order the work can be done in, so it stops a handoff.
func (e *Engine) addLoopCheck(ctx context.Context, c checkAdder, self Ref, hasComponents bool) error {
	loop, err := e.loopOf(ctx, self)
	if err != nil {
		return err
	}
	switch {
	case len(loop) > 0:
		c.add("components-loop", "goals", phaseInitiation, checkBlock,
			fmt.Sprintf("Depends on itself: %s. Remove one of these components to break the loop.", strings.Join(loop, " depends on ")))
	case hasComponents:
		c.add("components-loop", "goals", phaseInitiation, checkOK, "None of its components depends back on it.")
	}
	return nil
}

// projectComponents is every project id is made of: the ones it lists
// (D46) and the ones an older definition declared part of it (D15).
func (e *Engine) projectComponents(ctx context.Context, id string, spec map[string]any) ([]string, error) {
	out, err := e.componentsOf(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, r := range declaredComponents(spec) {
		if r.Kind == "Project" && !slices.Contains(out, r.ID) {
			out = append(out, r.ID)
		}
	}
	sort.Strings(out)
	return out, nil
}
