package engine

import (
	"context"
	"fmt"
	"sort"
)

// How records get into the sources a project's targets are read from
// (TAXONOMY.md D18).
//
// A national assessment printed for hand marking cannot return results in
// weeks, however good its key result is. Nothing in a definition said so
// until now, because nothing asked how answers become data. These checks
// ask, and advise only: whether the capture is fast enough is a judgement,
// but not having decided it is a fact worth saying out loud.

// sourceReading is one target read from a data source.
type sourceReading struct {
	label      string // what is read: a key result's metric or a criterion
	turnaround bool   // a key result measured as a duration
}

func (e *Engine) addCaptureChecks(ctx context.Context, c checkAdder, id string, spec map[string]any) {
	reads := map[string][]sourceReading{}
	objectives, _ := spec["objectives"].([]any)
	for _, o := range objectives {
		om, _ := o.(map[string]any)
		krs, _ := om["keyResults"].([]any)
		for _, k := range krs {
			km, _ := k.(map[string]any)
			src, _ := km["source"].(string)
			if src == "" {
				continue
			}
			metric, _ := km["metric"].(string)
			kind, _ := km["kind"].(string)
			reads[src] = append(reads[src], sourceReading{label: metric, turnaround: kind == "duration"})
		}
	}
	criteria, _ := spec["successCriteria"].([]any)
	for _, sc := range criteria {
		sm, _ := sc.(map[string]any)
		src, _ := sm["source"].(string)
		if src == "" {
			continue
		}
		statement, _ := sm["statement"].(string)
		reads[src] = append(reads[src], sourceReading{label: statement})
	}
	if len(reads) == 0 {
		return
	}

	ids := make([]string, 0, len(reads))
	for id := range reads {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// A source this project, or one of its components, writes into is one
	// whose capture this work is changing: the OMR component of an
	// assessment is exactly the answer to "typed in by hand". Named, not
	// silenced, so the reader sees which part carries it.
	changedBy := map[string]string{}
	for src := range producedInto(spec) {
		changedBy[src] = "this project"
	}
	if parts, err := e.projectComponents(ctx, id, spec); err == nil {
		for _, part := range parts {
			doc, err := e.loadProjectDoc(ctx, part)
			if err != nil {
				continue
			}
			pspec, _ := doc["spec"].(map[string]any)
			for src := range producedInto(pspec) {
				if _, have := changedBy[src]; !have {
					changedBy[src] = nameOf(doc, part)
				}
			}
		}
	}

	flagged := false
	for _, id := range ids {
		name, capture, found := e.dataSourceCapture(ctx, id)
		if !found {
			continue
		}
		var turnaround string
		for _, r := range reads[id] {
			if r.turnaround {
				turnaround = r.label
				break
			}
		}
		if by, ok := changedBy[id]; ok && capture != "notDecided" {
			c.add("data-capture-turnaround", "measures", phaseInitiation, checkOK,
				fmt.Sprintf("How records get into %s is being changed by %s.", name, by))
			continue
		}
		switch {
		case capture == "notDecided":
			flagged = true
			c.add("data-capture-decided", "data", phaseInitiation, checkWarn,
				fmt.Sprintf("How records get into %s isn't decided, and %d target%s %s read from it.",
					name, len(reads[id]), plural(len(reads[id])), isAre(len(reads[id]))))
		case turnaround != "" && (capture == "byHand" || capture == ""):
			flagged = true
			how := "doesn't say how records get in"
			if capture == "byHand" {
				how = "is typed in by hand"
			}
			c.add("data-capture-turnaround", "measures", phaseInitiation, checkWarn,
				fmt.Sprintf("\"%s\" is a turnaround read from %s, which %s. Say how records will get in fast enough.",
					turnaround, name, how))
		}
	}
	if !flagged {
		c.add("data-capture-decided", "data", phaseInitiation, checkOK,
			"The sources its targets are read from say how records get in, or have no deadline to meet.")
	}
}

// producedInto lists the data sources a project writes into.
func producedInto(spec map[string]any) map[string]bool {
	out := map[string]bool{}
	data, _ := spec["data"].(map[string]any)
	produces, _ := data["produces"].([]any)
	for _, p := range produces {
		pm, _ := p.(map[string]any)
		if s, _ := pm["sink"].(string); s != "" {
			out[s] = true
		}
	}
	return out
}

// dataSourceCapture reads a data source's name and capture method.
func (e *Engine) dataSourceCapture(ctx context.Context, id string) (name, capture string, found bool) {
	v, ok, err := e.manifests.GetCurrent(ctx, "DataSource", id)
	if err != nil || !ok {
		return "", "", false
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		return "", "", false
	}
	spec, _ := doc["spec"].(map[string]any)
	capture, _ = spec["capture"].(string)
	return nameOf(doc, id), capture, true
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}
