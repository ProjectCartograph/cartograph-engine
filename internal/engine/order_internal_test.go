package engine

import (
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
)

// The contract is a directed acyclic graph (TAXONOMY.md D28): every
// reference a schema declares to a named kind points to a kind before its
// holder in the order of work, or to the holder's own kind, a tree the
// loop check holds to. A reference that pointed downstream would mean
// writing the later thing first and coming back to finish the earlier
// one, which is the loop the order exists to remove.
func TestEveryReferenceInTheContractPointsUpstream(t *testing.T) {
	ss, err := loadSchemas()
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range kinds.All {
		if spec.Name == "Settings" {
			continue
		}
		if rank(spec.Name, "") >= len(registers)+len(stages)+len(afterStages) {
			t.Errorf("%s has no place in the order of work", spec.Name)
		}
		for _, r := range collectRefRules(ss.raw, spec.SchemaFile) {
			if r.kind == "*" {
				// Read from the instance; validation holds it to the order.
				continue
			}
			if !names(spec.Name, r.kind) {
				path := []string{}
				for _, s := range r.path {
					if s.wildcard {
						path = append(path, "-")
					} else {
						path = append(path, s.prop)
					}
				}
				t.Errorf("%s /%s names %s, which comes after it in the order of work", spec.Name, strings.Join(path, "/"), r.kind)
			}
		}
	}
}

// Each stage waits only on stages before it, so the order can be walked
// once from the top.
func TestEveryStageWaitsOnlyOnStagesBeforeIt(t *testing.T) {
	at := map[string]int{}
	for i, s := range stages {
		for _, a := range s.After {
			j, ok := at[a]
			if !ok {
				t.Errorf("stage %s waits on %s, which is not before it", s.Key, a)
				continue
			}
			if j >= i {
				t.Errorf("stage %s waits on %s, at %d", s.Key, a, j)
			}
		}
		at[s.Key] = i
		if _, ok := kinds.ByName(s.Kind); !ok {
			t.Errorf("stage %s names kind %s, which is not registered", s.Key, s.Kind)
		}
	}
	for i := 1; i < len(goalLevels); i++ {
		if rank("Goal", goalLevels[i-1]) >= rank("Goal", goalLevels[i]) {
			t.Errorf("goal level %s does not come before %s", goalLevels[i-1], goalLevels[i])
		}
	}
}
