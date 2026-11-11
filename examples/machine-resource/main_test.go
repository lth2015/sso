package main

import (
	"context"
	"errors"
	"github.com/lth2015/sso/pkg/machineidentity"
	"net/http/httptest"
	"strings"
	"testing"
)

type verifierFunc func(context.Context, string, string, string) (machineidentity.Identity, error)

func (v verifierFunc) Verify(c context.Context, t, a, b string) (machineidentity.Identity, error) {
	return v(c, t, a, b)
}
func TestInboundAuthenticationAndFailurePrivacy(t *testing.T) {
	for _, test := range []struct {
		name, method, path string
		headers            []string
		err                error
		status             int
		called             bool
	}{
		{"contract-json", "GET", "/openapi.json", nil, nil, 200, false},
		{"contract-yaml", "GET", "/openapi.yaml", nil, nil, 200, false},
		{"contract-post", "POST", "/openapi.json", nil, nil, 405, false},
		{"missing", "GET", "/whoami", nil, nil, 401, false},
		{"cookie-only", "GET", "/whoami", nil, nil, 401, false},
		{"duplicate", "GET", "/whoami", []string{"Bearer first", "Bearer second"}, nil, 401, false},
		{"basic", "GET", "/whoami", []string{"Basic fixture"}, nil, 401, false},
		{"hint-query", "GET", "/whoami?tenant=other", []string{"Bearer fixture"}, nil, 400, false},
		{"post", "POST", "/whoami", []string{"Bearer fixture"}, nil, 405, false},
		{"unknown", "GET", "/other", []string{"Bearer fixture"}, nil, 404, false},
		{"valid", "GET", "/whoami", []string{"Bearer fixture"}, nil, 200, true},
		{"revoked", "GET", "/whoami", []string{"Bearer fixture"}, machineidentity.ErrUnauthenticated, 401, true},
		{"forbidden", "GET", "/whoami", []string{"Bearer fixture"}, machineidentity.ErrForbidden, 403, true},
		{"offline", "GET", "/whoami", []string{"Bearer fixture"}, machineidentity.ErrUnavailable, 503, true},
		{"raw-error", "GET", "/whoami", []string{"Bearer fixture"}, errors.New("private-inbound-token-must-not-escape"), 503, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			verifier := verifierFunc(func(ctx context.Context, tenant, app, bearer string) (machineidentity.Identity, error) {
				called = true
				if tenant != "expected-tenant" || app != "expected-application" || bearer != "fixture" {
					t.Fatal("inbound data selected the authority context")
				}
				return machineidentity.Identity{Kind: "service_principal"}, test.err
			})
			request := httptest.NewRequest(test.method, "http://127.0.0.1:8092"+test.path, nil)
			request.Header["Authorization"] = test.headers
			request.Header.Set("Cookie", "user-session=does-not-authorize")
			request.Header.Set("X-Tenant-ID", "untrusted-hint")
			response := httptest.NewRecorder()
			handler(verifier, "expected-tenant", "expected-application").ServeHTTP(response, request)
			if response.Code != test.status || called != test.called {
				t.Fatal("reference resource authentication boundary failed")
			}
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "private-inbound") || strings.Contains(response.Body.String(), "user-session") {
				t.Fatal("reference resource leaked or cached authentication data")
			}
		})
	}
}
