// Package spa embeds the built SPA (cartograph/web, built with `npm run build`
// into web/dist and synced here by `make generate`, since Go embed cannot
// reach outside its module) and serves it at "/", falling back to
// index.html for any path that is not a real file, so client-side routes
// resolve on a hard refresh. Never applied to "/api": the caller mounts
// that separately, before this handler.
package spa

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded build with a single-page-app fallback.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // programmer error: dist is always embedded
	}
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
