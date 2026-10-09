package activity

import (
	"encoding/json"
	"io/fs"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/contract"
)

// Flow is what the analysis needs of a kind's flow: its steps in order,
// which step asks each field, and its opportunities.
type Flow struct {
	Kind  string
	Steps []string
	// StepOf is the step that asks each field, by its pointer.
	StepOf map[string]string
	// Fields is every field the flow asks, in order, with its step and
	// whether it is a list a person adds items to.
	Fields []FlowField
}

// FlowField is one field a flow asks.
type FlowField struct {
	Path    string
	Step    string
	Control string
}

// Opportunities is how many places a Define or Change task on this kind
// can go wrong: each field the flow asks, and the decision to save it as
// a version. The same in every interface and every run, so defects per
// million compare across kinds.
func (f Flow) Opportunities() int { return len(f.Fields) + 1 }

// StepFor is the step that asks the field a problem names, the longest
// field pointer that prefixes path; empty when no step asks it.
func (f Flow) StepFor(path string) string {
	best, step := -1, ""
	for p, s := range f.StepOf {
		if (path == p || strings.HasPrefix(path, p+"/")) && len(p) > best {
			best, step = len(p), s
		}
	}
	return step
}

// Flows reads every kind's flow from the contract, by kind.
func Flows() (map[string]Flow, error) {
	out := map[string]Flow{}
	err := fs.WalkDir(contract.Flows, "flows", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".flow.json") {
			return err
		}
		b, err := fs.ReadFile(contract.Flows, path)
		if err != nil {
			return err
		}
		var doc struct {
			Spec struct {
				For   string `json:"for"`
				Steps []struct {
					Key    string `json:"key"`
					Fields []struct {
						Path    string `json:"path"`
						Control string `json:"control"`
					} `json:"fields"`
				} `json:"steps"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			return err
		}
		f := Flow{Kind: doc.Spec.For, StepOf: map[string]string{}}
		for _, s := range doc.Spec.Steps {
			f.Steps = append(f.Steps, s.Key)
			for _, fl := range s.Fields {
				if fl.Control == "readonly" {
					continue
				}
				f.StepOf[fl.Path] = s.Key
				f.Fields = append(f.Fields, FlowField{Path: fl.Path, Step: s.Key, Control: fl.Control})
			}
		}
		if f.Kind != "" {
			out[f.Kind] = f
		}
		return nil
	})
	return out, err
}

// FlowKinds lists the kinds of flows, in order.
func FlowKinds(flows map[string]Flow) []string {
	out := make([]string, 0, len(flows))
	for k := range flows {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
