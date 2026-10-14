package spa

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A build served from a directory answers like the embedded one: its
// files by path, and index.html for a client-side route.
func TestDirServesABuild(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>local build</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/projects/p1/initiation"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if !strings.Contains(rec.Body.String(), "local build") {
			t.Errorf("GET %s: %d %q, want the local index.html", path, rec.Code, rec.Body.String())
		}
	}
}

// A directory with no build in it is refused at start, not served blank.
func TestDirRefusesADirectoryWithoutABuild(t *testing.T) {
	if _, err := Dir(t.TempDir()); err == nil || !strings.Contains(err.Error(), "CARTOGRAPH_UI_DIR") {
		t.Fatalf("Dir(empty) = %v, want an error naming CARTOGRAPH_UI_DIR", err)
	}
}
