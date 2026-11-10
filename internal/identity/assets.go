package identity

import (
	"embed"
	"net/http"
)

//go:embed web/*
var web embed.FS

func Assets() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		files := map[string]string{"/account": "index.html", "/account/": "index.html", "/account/account.css": "account.css", "/account/account.js": "account.js"}
		file, ok := files[r.URL.Path]
		if !ok {
			writeError(w, 404, "not_found")
			return
		}
		body, err := web.ReadFile("web/" + file)
		if err != nil {
			writeError(w, 503, "temporarily_unavailable")
			return
		}
		kind := "text/html; charset=utf-8"
		if file == "account.css" {
			kind = "text/css; charset=utf-8"
		}
		if file == "account.js" {
			kind = "text/javascript; charset=utf-8"
		}
		w.Header().Set("Content-Type", kind)
		_, _ = w.Write(body)
	})
}
