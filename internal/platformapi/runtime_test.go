package platformapi

import (
	"context"
	"encoding/json"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProductionVerifierUsesConfiguredIssuerAndPrivateServices(t *testing.T) {
	issuer := "https://sso.nsstest.com/"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "oauth2/auth", "token_endpoint": issuer + "oauth2/token", "jwks_uri": issuer + ".well-known/jwks.json", "id_token_signing_alg_values_supported": []string{"RS256"}})
	}))
	defer server.Close()
	cfg := RuntimeConfig{issuer, server.URL, "http://hydra-admin.ory-hydra.svc.cluster.local:4445"}
	a, err := NewAuthenticatorWithConfig(context.Background(), store.Store{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if a.Issuer != issuer || a.Admin.Base != cfg.AdminURL || a.Admin.IssuerURL != issuer {
		t.Fatal("production endpoints were not retained")
	}
	request, _ := http.NewRequest("GET", "https://attacker.example/.well-known/jwks.json", nil)
	if _, err := a.HTTP.Do(request); err == nil {
		t.Fatal("untrusted issuer destination accepted")
	}
}
func TestProductionVerifierRejectsLocalIssuer(t *testing.T) {
	cfg := RuntimeConfig{"http://auth.localhost:4464/", "http://hydra:4444", "http://hydra:4445"}
	if cfg.Validate() == nil {
		t.Fatal("insecure production issuer accepted")
	}
	cfg.Issuer = "https://sso.nsstest.com/"
	cfg.AdminURL = "http://user:secret@hydra:4445"
	if cfg.Validate() == nil {
		t.Fatal("embedded admin credentials accepted")
	}
}
