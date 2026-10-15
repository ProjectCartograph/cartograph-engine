package spa

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
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

// build writes an index.html and a hashed script big enough to compress.
func build(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	script := strings.Repeat("console.log('a line of the interface');\n", 200)
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": "<p>local build</p>", "assets/main-abc123.js": script} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, script
}

func get(t *testing.T, h http.Handler, path string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A browser that takes gzip gets the hashed script compressed, kept for a
// year; one that does not, or refuses gzip, gets it as it is (engine
// issue 103).
func TestAssetsAreCompressedAndKept(t *testing.T) {
	dir, script := build(t)
	h, err := Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/assets/main-abc123.js", map[string]string{"Accept-Encoding": "gzip, deflate, br, zstd"})
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Body.Len() >= len(script) {
		t.Fatalf("encoding %q, %d bytes of %d", rec.Header().Get("Content-Encoding"), rec.Body.Len(), len(script))
	}
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if plain, _ := io.ReadAll(zr); string(plain) != script {
		t.Fatal("the compressed script does not read back as the script")
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control %q", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
		t.Errorf("Content-Type %q", got)
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Errorf("Vary %q", rec.Header().Get("Vary"))
	}
	for _, ae := range []string{"", "identity", "gzip;q=0, identity"} {
		rec := get(t, h, "/assets/main-abc123.js", map[string]string{"Accept-Encoding": ae})
		if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != script {
			t.Errorf("Accept-Encoding %q: encoding %q, %d bytes", ae, rec.Header().Get("Content-Encoding"), rec.Body.Len())
		}
	}
}

// index.html, for "/" and for a client-side route, is asked for again on
// every load and answered 304 while it is the same.
func TestIndexIsRevalidated(t *testing.T) {
	dir, _ := build(t)
	h, err := Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/projects/p1"} {
		rec := get(t, h, path, nil)
		if rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("ETag") == "" {
			t.Fatalf("GET %s: Cache-Control %q ETag %q", path, rec.Header().Get("Cache-Control"), rec.Header().Get("ETag"))
		}
		if again := get(t, h, path, map[string]string{"If-None-Match": rec.Header().Get("ETag")}); again.Code != 304 {
			t.Errorf("GET %s again: %d, want 304", path, again.Code)
		}
	}
}

// A build replaced in its directory is served as it now is, not as it was
// first read.
func TestAReplacedBuildIsReadAfresh(t *testing.T) {
	dir, _ := build(t)
	h, err := Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	get(t, h, "/", nil)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>the new build, longer</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := get(t, h, "/", nil); !strings.Contains(rec.Body.String(), "the new build") {
		t.Fatalf("GET / after the build changed: %q", rec.Body.String())
	}
}

// The embedded build compresses too.
func TestTheEmbeddedBuildIsCompressed(t *testing.T) {
	rec := get(t, Handler(), "/", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Code != 200 {
		t.Fatalf("GET /: %d", rec.Code)
	}
	if rec.Body.Len() >= minCompress && rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("index.html of %d bytes not compressed", rec.Body.Len())
	}
}
