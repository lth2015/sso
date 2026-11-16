package demo

import (
	"net/http"
	"strings"

	"github.com/lth2015/sso/internal/adapter/hydra"
)

// Fixed navigation routes keep frontend bundles independent of deployment domains.
// No user-provided redirect target, query string, or forwarded Host is trusted.
func (a *App) mountAuthLinks(mux *http.ServeMux) {
	origin := a.AuthOrigin
	if origin == "" {
		origin = hydra.AuthOrigin
	}
	origin = strings.TrimSuffix(origin, "/")
	for route, target := range map[string]string{
		"/auth/account":       "/account",
		"/auth/security":      "/sso/security",
		"/auth/authenticator": "/sso/security/authenticator",
		"/auth/logout":        "/sso/logout",
	} {
		mux.HandleFunc("GET "+route, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, origin+target, http.StatusSeeOther)
		})
	}
}
