package demo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBFFChecksAllServerBoundAuthorityFields(t *testing.T) {
	expected := accessClaims{Subject: "user", Client: "sso-bff"}
	expected.Ext.Tenant = "tenant"
	expected.Ext.Grant = "grant"
	expected.Ext.Member = "member"
	expected.Ext.Session = "session"
	correct := map[string]string{"user": "user", "client": "sso-bff", "tenant": "tenant", "grant": "grant", "member": "member", "session": "session"}
	for _, field := range []string{"", "user", "client", "tenant", "grant", "member", "session"} {
		t.Run(field, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/internal/bff/authorization" || r.Header.Get("Authorization") != "Bearer test-access" || r.Host != "api.localhost:8083" {
					t.Fatal("unexpected authority request")
				}
				result := map[string]string{}
				for k, v := range correct {
					result[k] = v
				}
				if field != "" {
					result[field] = "other"
				}
				json.NewEncoder(w).Encode(result)
			}))
			defer server.Close()
			app := &App{Console: true, Client: "sso-bff", PlatformURL: server.URL}
			err := app.checkAuthority(context.Background(), "test-access", expected)
			if field == "" && err != nil {
				t.Fatal(err)
			}
			if field != "" && !errors.Is(err, store.ErrDenied) {
				t.Fatal("authority context mismatch accepted")
			}
		})
	}
}
func TestRevocationCredentialNeverUsesAnExpiredBearerToken(t *testing.T) {
	secret := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access" {
			t.Fatal("unexpected credential authentication")
		}
		json.NewEncoder(w).Encode(map[string]any{"credential": secret, "expires_at": time.Now().Unix() + 3600})
	}))
	defer server.Close()
	app := &App{Console: true, Client: "sso-bff", PlatformURL: server.URL}
	value, expiry, err := app.issueRevocation(context.Background(), "test-access")
	if err != nil || value != secret || expiry <= time.Now().Unix() {
		t.Fatal("capability response rejected")
	}
	if consoleRouteAllowed("POST", "/internal/bff/revoke") || consoleRouteAllowed("POST", "/internal/bff/revocation-credentials") {
		t.Fatal("private capability API exposed by browser proxy")
	}
}
func TestAuthorityOutageIsNotTreatedAsValidAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	app := &App{PlatformURL: server.URL, Client: "sso-bff", Console: true}
	if err := app.checkAuthority(context.Background(), "token", accessClaims{}); !errors.Is(err, errAuthorityUnavailable) {
		t.Fatal("outage accepted or misclassified")
	}
	// Browser input does not choose an authority endpoint.
	if strings.Contains(app.platformURL(), "internal/bff") {
		t.Fatal("unexpected upstream mutation")
	}
}
