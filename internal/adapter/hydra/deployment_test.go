package hydra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfiguredDecisionRedirect(t *testing.T) {
	for _, redirect := range []string{"https://sso.nsstest.com/oauth2/auth?x=1", "http://sso.nsstest.com/oauth2/auth", "https://attacker.example/oauth2/auth", "https://sso.nsstest.com/admin", "https://user@sso.nsstest.com/oauth2/auth"} {
		t.Run(redirect, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]string{"redirect_to": redirect})
			}))
			defer server.Close()
			c := New()
			c.Base = server.URL
			c.IssuerURL = "https://sso.nsstest.com/"
			_, err := c.Decide(context.Background(), "login", "challenge", "accept", map[string]string{})
			if (err == nil) != (redirect == "https://sso.nsstest.com/oauth2/auth?x=1") {
				t.Fatalf("unexpected redirect decision: %v", err)
			}
		})
	}
}
