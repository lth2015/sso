package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

func testRuntime() RuntimeConfig {
	return RuntimeConfig{
		Origin: "https://bff.nsstest.com", Issuer: "https://sso.nsstest.com/", HydraPublicURL: "http://hydra-public.ory-hydra.svc.cluster.local:4444", PlatformAPIURL: "http://platform-api.nss-sso.svc.cluster.local:8083",
	}
}
func TestRuntimeConfigurationRejectsUnsafeURLs(t *testing.T) {
	for _, raw := range []string{"http://bff.nsstest.com", "https://user:pass@bff.nsstest.com", "https://bff.nsstest.com/path", "https://bff.nsstest.com?x=1", "//bff.nsstest.com"} {
		cfg := testRuntime()
		cfg.Origin = raw
		if cfg.Validate() == nil {
			t.Fatalf("unsafe public origin accepted: %q", raw)
		}
	}
}
func TestProductionConstructorUsesInternalHydraAndPublicIssuer(t *testing.T) {
	cfg := testRuntime()
	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"issuer": cfg.Issuer, "authorization_endpoint": "https://sso.nsstest.com/oauth2/auth", "token_endpoint": "https://sso.nsstest.com/oauth2/token", "jwks_uri": "https://sso.nsstest.com/.well-known/jwks.json", "id_token_signing_alg_values_supported": []string{"RS256"}})
	}))
	defer discovery.Close()
	cfg.HydraPublicURL = discovery.URL
	app, err := New(context.Background(), store.Store{}, "sso-bff", strings.Repeat("s", 64), make([]byte, 32), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !app.isConsole() || app.OAuth.RedirectURL != "https://bff.nsstest.com/oidc/callback" || app.OAuth.Endpoint.AuthURL != "https://sso.nsstest.com/oauth2/auth" {
		t.Fatal("production client routing mismatch")
	}
	if app.platformURL() != cfg.PlatformAPIURL {
		t.Fatal("incorrect platform upstream")
	}
	r := httptest.NewRequest("GET", "https://attacker.example/.well-known/jwks.json", nil)
	if _, err := app.HTTP.Transport.RoundTrip(r); err == nil {
		t.Fatal("transport accepted an untrusted origin")
	}
}
func TestProductionCookiesAndContextBinding(t *testing.T) {
	app := &App{Client: "sso-bff", Console: true, Origin: "https://bff.nsstest.com"}
	context := "0123456789abcdef0123456789abcdef"
	r := httptest.NewRequest("GET", "https://bff.nsstest.com/session", nil)
	r.Header.Set("X-Console-Context", context)
	name, ok := app.sessionCookieName(r)
	if !ok || name != "__Host-sso-bff-context-"+context {
		t.Fatal("invalid production context cookie")
	}
	for _, cookieName := range []string{browserCookie, name} {
		w := httptest.NewRecorder()
		app.setCookie(w, cookieName, "value", 3600)
		c := w.Result().Cookies()[0]
		if !strings.HasPrefix(c.Name, "__Host-") || !c.Secure || !c.HttpOnly || c.Domain != "" || c.Path != "/" || c.SameSite != http.SameSiteLaxMode {
			t.Fatalf("unsafe cookie: %+v", c)
		}
	}
	w := httptest.NewRecorder()
	app.clearSessionCookie(w, r)
	if c := w.Result().Cookies()[0]; c.Name != name || c.MaxAge != -1 || !c.Secure {
		t.Fatal("cookie deletion mismatch")
	}
}
func TestProductionHostAndCSRFAreNotBypassedBySiblingDomain(t *testing.T) {
	app := &App{Client: "sso-bff", Console: true, Origin: "https://bff.nsstest.com"}
	handler := app.Handler()
	for _, tc := range []struct{ host, origin string }{{"attacker.nsstest.com", "https://bff.nsstest.com"}, {"bff.nsstest.com", "https://attacker.nsstest.com"}} {
		r := httptest.NewRequest("POST", "https://"+tc.host+"/session/logout", strings.NewReader("{}"))
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", "same-site")
		r.Header.Set("X-Forwarded-Host", "bff.nsstest.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("host or sibling-origin bypass: %d", w.Code)
		}
	}
}
func TestBridgeDoesNotMutateRequest(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer target.Close()
	bridge := Bridge{Base: http.DefaultTransport, Issuer: "https://sso.nsstest.com/", Upstream: target.URL}
	r := httptest.NewRequest("GET", "https://sso.nsstest.com/.well-known/jwks.json", nil)
	before := *r.URL
	resp, err := bridge.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if *r.URL != before {
		t.Fatal("transport mutated source request")
	}
}
