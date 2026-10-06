package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Every check added as checkBlock is in blockingChecks, and nothing else
// is, so the fields Guide marks required are exactly those a handoff
// waits on.
func TestBlockingChecksAreTheChecksThatBlock(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	// add("id", ..., checkBlock or addFix("id", ..., checkBlock, possibly
	// with the state on the next line.
	re := regexp.MustCompile(`add(?:Fix)?\("([a-z-]+)",[^\n]*\n?[^\n]*?checkBlock`)
	found := map[string]bool{}
	for _, f := range files {
		if filepath.Ext(f) != ".go" || len(f) > 8 && f[len(f)-8:] == "_test.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			found[m[1]] = true
		}
	}
	for id := range found {
		if !blockingChecks[id] {
			t.Errorf("%s is added as checkBlock but is not in blockingChecks", id)
		}
	}
	for id := range blockingChecks {
		if !found[id] {
			t.Errorf("%s is in blockingChecks but never added as checkBlock", id)
		}
	}
}
