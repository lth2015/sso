package demo

import (
	"embed"
	"net/http"
)

//go:embed web/*
var assets embed.FS

func (a *App) assets() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := ""
		switch r.URL.Path {
		case "/":
			name = "index.html"
		case "/app.js":
			name = "app.js"
		case "/app.css":
			name = "app.css"
		default:
			fail(w, 404)
			return
		}
		body, err := assets.ReadFile("web/" + name)
		if err != nil {
			fail(w, 503)
			return
		}
		kind := "text/html; charset=utf-8"
		if name == "app.js" {
			kind = "text/javascript; charset=utf-8"
		}
		if name == "app.css" {
			kind = "text/css; charset=utf-8"
		}
		w.Header().Set("Content-Type", kind)
		_, _ = w.Write(body)
	})
}
