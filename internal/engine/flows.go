package engine

import (
	"io/fs"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/contract"
)

// FlowJSON returns a kind's flow document exactly as written in the
// contract, or found=false for a kind that is a plain sheet. Served raw
// for the same reason schemas are: the order of steps and fields is the
// document's meaning.
func (e *Engine) FlowJSON(kind string) ([]byte, bool, error) {
	b, err := fs.ReadFile(contract.Flows, "flows/"+strings.ToLower(kind)+".flow.json")
	if err != nil {
		if _, isNotExist := err.(*fs.PathError); isNotExist {
			return nil, false, nil
		}
		return nil, false, err
	}
	return b, true, nil
}

// FlowKinds lists the kinds that have a flow, in registry order.
func (e *Engine) FlowKinds() []string {
	out := []string{}
	for _, info := range e.Kinds() {
		if _, found, _ := e.FlowJSON(info.Kind); found {
			out = append(out, info.Kind)
		}
	}
	sort.Strings(out)
	return out
}
