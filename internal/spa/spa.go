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
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
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
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(sub, trimLeadingSlash(r.URL.Path)); err != nil {
			// Not a real file: hand the SPA's index.html back so the
			// client-side router can take over.
			r2 := new(http.Request)
			*r2 = *r
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
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
