//go:build integration

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/proxy"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/printer"
)

const accessYAML = `roles:
  contributor: [quality-team]
  administrator: [cartograph-administrators]
teams:
  - group: quality-team
    name: Quality Team
  - group: intake
    name: Intake
    parent: Quality Team
`

func writeFile(t *testing.T, name, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDirectory(t *testing.T) {
	d, err := loadDirectory(writeFile(t, "access.yaml", accessYAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Roles["contributor"]) != 1 || len(d.Teams) != 2 || d.Teams[1].Parent != "Quality Team" {
		t.Fatalf("loaded: %+v", d)
	}
	for name, text := range map[string]string{
		"an unknown role":      "roles:\n  owner: [x]\n",
		"a team with no group": "teams:\n  - name: Nobody's\n",
		"two teams, one name":  "teams:\n  - group: a\n    name: Same\n  - group: b\n    name: Same\n",
	} {
		if _, err := loadDirectory(writeFile(t, "access.yaml", text)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if d, err := loadDirectory(""); err != nil || len(d.Teams) != 0 {
		t.Fatalf("no file: %+v, %v", d, err)
	}
}

// Access by role and team through exactly the stack serve runs: the
// proxy's headers, the access policy, the mapped teams created by
// `cartograph access apply`, and a contributor refused another team's
// work while their own goes through.
func TestAccessThroughTheServeStack(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("cp", "-r", "../../examples/minimal/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("copy the example: %v %s", err, out)
	}
	file := writeFile(t, "access.yaml", accessYAML)
	if err := runAccessApply([]string{file, "-store", dir}); err != nil {
		t.Fatal(err)
	}
	d, err := loadDirectory(file)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := compose(context.Background(), storeOptions{Target: dir, Codec: "yaml", Access: &d})
	if err != nil {
		t.Fatal(err)
	}
	defer comp.Close()
	var ready atomic.Bool
	ready.Store(true)
	mux, _ := routes(comp.Engine, comp.Fanout, proxy.New("X-Forwarded-User"), comp.Authz, printer.None{}, comp.Reports, agentsConfig{On: true}, &ready)
	srv := httptest.NewServer(requestLog(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, mux))
	defer srv.Close()

	do := func(method, path, body, groups string) int {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-User", "sub-lee")
		req.Header.Set("X-Forwarded-Email", "lee@example.org")
		req.Header.Set("X-Forwarded-Groups", groups)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	project := func(team string) string {
		return `{"yaml":"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: intake-checks\n  name: Intake checks\nspec:\n  team: ` + team + `\n"}`
	}
	if got := do("GET", "/api/v1/session", "", "all-staff"); got != http.StatusOK {
		t.Fatalf("an unlisted session: %d", got)
	}
	if got := do("GET", "/api/v1/manifests/Team/quality-team", "", "all-staff"); got != http.StatusForbidden {
		t.Fatalf("an unlisted read: %d", got)
	}
	// Intake sits under the quality team, which the example already had.
	if got := do("PUT", "/api/v1/manifests/Project/intake-checks/working", project("intake"), "quality-team"); got/100 != 2 {
		t.Fatalf("a project for a team beneath theirs: %d", got)
	}
	if got := do("PUT", "/api/v1/manifests/Project/intake-checks/working", project("operations-team"), "quality-team"); got != http.StatusForbidden {
		t.Fatalf("giving it to another team: %d", got)
	}
	if got := do("GET", "/api/v1/access/people", "", "quality-team"); got != http.StatusForbidden {
		t.Fatalf("a contributor reading the access list: %d", got)
	}
}
