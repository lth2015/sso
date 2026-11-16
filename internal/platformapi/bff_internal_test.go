package platformapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBFFPrivateAPIRejectsUnauthenticatedAndAmbiguousRequests(t *testing.T) {
	a := &Authenticator{}
	for _, tc := range []struct {
		method, path, auth, body string
		status                   int
	}{
		{"GET", bffAuthorityPath, "", "", 401},
		{"POST", bffCredentialPath, "", "{}", 401},
		{"POST", bffRevokePath, "Bearer expired-access", "{}", 401},
		{"POST", bffRevokePath, "BFF-Revoke bad", "{}", 401},
		{"DELETE", bffRevokePath, "", "", 405},
		{"POST", bffRevokePath, "BFF-Revoke " + strings.Repeat("A", 43), `{"reason":"authorization.logout","reason":"tenant.delete"}`, 400},
		{"POST", bffRevokePath, "BFF-Revoke " + strings.Repeat("A", 43), `{"reason":"authorization.logout","tenant":"other"}`, 400},
	} {
		r := httptest.NewRequest(tc.method, "http://api.localhost:8083"+tc.path, strings.NewReader(tc.body))
		r.Header.Set("Authorization", tc.auth)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.bffInternal(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: got %d, want %d", tc.method, tc.path, w.Code, tc.status)
		}
	}
}
