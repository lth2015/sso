package identity

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func deploymentLogin(t *testing.T) *Login {
	t.Helper()
	l, err := NewLoginWithConfig(nil, make([]byte, 32), make([]byte, 32), LoginConfig{
		Issuer: "https://sso.nsstest.com/", Origin: "https://sso.nsstest.com", AdminURL: "http://hydra-admin.ory-hydra.svc.cluster.local:4445", ConsoleOrigin: "https://console.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}
func TestDeploymentSecureCookies(t *testing.T) {
	l := deploymentLogin(t)
	for _, age := range []int{3600, -1} {
		w := httptest.NewRecorder()
		l.setCookie(w, SessionCookie, "value", age)
		c := w.Result().Cookies()[0]
		if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Domain != "" {
			t.Fatalf("unsafe cookie: %+v", c)
		}
	}
}
func TestDeploymentHostAndOrigin(t *testing.T) {
	l := deploymentLogin(t)
	h := l.Handler()
	for _, tc := range []struct{ host, origin string }{
		{"attacker.example", "https://sso.nsstest.com"},
		{"sso.nsstest.com", "https://attacker.example"},
	} {
		r := httptest.NewRequest("POST", "https://"+tc.host+"/v1/flows/x/password", strings.NewReader("{}"))
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", "application/json")
		// Forwarded headers must not override configured public origin checks.
		r.Header.Set("X-Forwarded-Host", "sso.nsstest.com")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("unexpected status: %d", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "https://sso.nsstest.com/flow", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("configured host rejected: %d", w.Code)
	}
}
func TestDeploymentEndpointValidation(t *testing.T) {
	l := deploymentLogin(t)
	for _, raw := range []string{"https://user:pass@example.com", "https://example.com/path", "https://example.com?x=1", "//example.com", "https://example.com#x"} {
		cfg := l.Config
		cfg.Issuer = raw
		if cfg.Validate() == nil {
			t.Fatalf("accepted unsafe issuer %q", raw)
		}
	}
}
