package demo

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticationLinksUseConfiguredOrigin(t *testing.T) {
	app := &App{Client: "sso-bff", Console: true, Origin: "https://bff.nsstest.com", AuthOrigin: "https://sso.nsstest.com"}
	handler := app.Handler()
	for route, target := range map[string]string{"/auth/account": "/account", "/auth/security": "/sso/security", "/auth/authenticator": "/sso/security/authenticator", "/auth/logout": "/sso/logout"} {
		r := httptest.NewRequest("GET", "https://bff.nsstest.com"+route, nil)
		r.Header.Set("X-Forwarded-Host", "attacker.example")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "https://sso.nsstest.com"+target {
			t.Fatalf("incorrect authentication link: %s", route)
		}
	}
	r := httptest.NewRequest("GET", "https://bff.nsstest.com/auth/account?redirect=https://attacker.example", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 || w.Header().Get("Location") != "" {
		t.Fatal("accepted caller-provided redirect")
	}
}
