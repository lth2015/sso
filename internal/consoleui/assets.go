// Package consoleui embeds the built React application. Source-only Go tests
// compile with a placeholder; actual startup requires the generated index/assets.
package consoleui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static
var files embed.FS

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := "static/index.html"
		if r.URL.Path != "/" {
			if !strings.HasPrefix(r.URL.Path, "/assets/") || path.Clean(r.URL.Path) != r.URL.Path || r.URL.RawPath != "" {
				http.NotFound(w, r)
				return
			}
			name = "static" + r.URL.Path
		}
		data, err := files.ReadFile(name)
		if err != nil {
			if r.URL.Path == "/" {
				http.Error(w, "temporarily_unavailable", 503)
			} else {
				http.NotFound(w, r)
			}
			return
		}
		kind := "text/html; charset=utf-8"
		if strings.HasSuffix(name, ".js") {
			kind = "text/javascript; charset=utf-8"
		} else if strings.HasSuffix(name, ".css") {
			kind = "text/css; charset=utf-8"
		} else if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", kind)
		w.Write(data)
	})
}
func Ready() bool { _, err := fs.Stat(files, "static/index.html"); return err == nil }
