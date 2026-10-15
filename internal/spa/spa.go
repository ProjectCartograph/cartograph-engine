// Package spa embeds the web interface's build and serves it at "/",
// falling back to index.html for any path that is not a real file, so
// client-side routes resolve on a hard refresh. Never applied to "/api":
// the caller mounts that separately, before this handler.
//
// The build is a cartograph-ui release, not source: `just ui` (and the
// flake) unpack the release UI_VERSION names into dist, checked against
// UI_SHA256, and `just ui <path>` puts a local build there instead. Dir
// serves a build from a directory at run time instead (CARTOGRAPH_UI_DIR),
// so a container image tries an interface build without being rebuilt.
package spa

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded build with a single-page-app fallback.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // programmer error: dist is always embedded
	}
	return serve(sub)
}

// Dir serves the build in dir, as Handler serves the embedded one. A
// directory without an index.html is refused, so a wrong path fails at
// start rather than as a blank page.
func Dir(dir string) (http.Handler, error) {
	sub := os.DirFS(dir)
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, fmt.Errorf("CARTOGRAPH_UI_DIR %s: no index.html: %w", dir, err)
	}
	return serve(sub), nil
}

func serve(sub fs.FS) http.Handler {
	files := &encoded{fsys: sub}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := trimLeadingSlash(r.URL.Path)
		if info, err := fs.Stat(sub, name); err != nil || info.IsDir() {
			// Not a real file: hand the SPA's index.html back so the
			// client-side router can take over.
			name = "index.html"
		}
		files.serve(w, r, name)
	})
}

// encoded serves a build's files compressed when the browser takes gzip,
// with caching the build allows (engine issue 103). The bundler names
// every file under assets/ by its content's hash, so a browser keeps one
// for a year without asking again; everything else, index.html above all,
// is asked for again on each load and answered 304 while it is the same.
// Each file is read, hashed and compressed once and kept, keyed by its
// size and time as well as its name, so a build replaced in a directory
// (CARTOGRAPH_UI_DIR) is read afresh.
type encoded struct {
	fsys  fs.FS
	cache sync.Map // key -> *file
}

type file struct {
	plain, gz []byte
	etag      string
}

// Compressing a file this small or already compressed saves nothing.
const minCompress = 1024

var precompressed = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true, ".woff": true, ".woff2": true, ".gz": true, ".zst": true, ".br": true, ".zip": true}

func (e *encoded) load(name string) (*file, time.Time, error) {
	info, err := fs.Stat(e.fsys, name)
	if err != nil {
		return nil, time.Time{}, err
	}
	key := fmt.Sprintf("%s\x00%d\x00%d", name, info.Size(), info.ModTime().UnixNano())
	if f, ok := e.cache.Load(key); ok {
		return f.(*file), info.ModTime(), nil
	}
	plain, err := fs.ReadFile(e.fsys, name)
	if err != nil {
		return nil, time.Time{}, err
	}
	sum := sha256.Sum256(plain)
	f := &file{plain: plain, etag: `"` + hex.EncodeToString(sum[:12]) + `"`}
	if len(plain) >= minCompress && !precompressed[strings.ToLower(path.Ext(name))] {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		zw.Write(plain)
		zw.Close()
		if buf.Len() < len(plain) {
			f.gz = buf.Bytes()
		}
	}
	e.cache.Store(key, f)
	return f, info.ModTime(), nil
}

func (e *encoded) serve(w http.ResponseWriter, r *http.Request, name string) {
	f, mod, err := e.load(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	body, etag := f.plain, f.etag
	if f.gz != nil {
		h.Add("Vary", "Accept-Encoding")
		if acceptsGzip(r.Header.Get("Accept-Encoding")) {
			body, etag = f.gz, f.etag[:len(f.etag)-1]+`-gz"`
			h.Set("Content-Encoding", "gzip")
		}
	}
	h.Set("ETag", etag)
	// ServeContent answers If-None-Match with 304, ranges and HEAD, and
	// takes the type from the name, not from the compressed bytes.
	http.ServeContent(w, r, path.Base(name), mod, bytes.NewReader(body))
}

// acceptsGzip says whether an Accept-Encoding header takes gzip with a
// weight above zero.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "gzip") && strings.TrimSpace(coding) != "*" {
			continue
		}
		q := strings.ReplaceAll(strings.TrimSpace(params), " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}

func trimLeadingSlash(p string) string {
	if p == "" || p == "/" {
		return "."
	}
	if p[0] == '/' {
		return p[1:]
	}
	return p
}
