package platformapi

import (
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMachinePreconditionsCannotCrossResourceOrKind(t *testing.T) {
	tenant, _ := store.ParseID(strings.Repeat("a", 32))
	credential, _ := store.ParseID(strings.Repeat("b", 32))
	valid := machineETag("cred", tenant, credential.String(), 3)
	for _, tt := range []struct {
		header []string
		kind   string
		status int
	}{
		{nil, "cred", 428}, {[]string{valid}, "cred", 200}, {[]string{valid, valid}, "cred", 400},
		{[]string{valid}, "sp", 400}, {[]string{strings.Replace(valid, "-3\"", "-03\"", 1)}, "cred", 400},
		{[]string{strings.Replace(valid, tenant.String(), strings.Repeat("c", 32), 1)}, "cred", 412},
		{[]string{strings.Replace(valid, credential.String(), strings.Repeat("c", 32), 1)}, "cred", 412},
	} {
		r := httptest.NewRequest("POST", "http://api.localhost:8083/", nil)
		r.Header["If-Match"] = tt.header
		w := httptest.NewRecorder()
		version, ok := machinePrecondition(w, r, tt.kind, tenant, credential)
		if w.Code != tt.status || ok != (tt.status == 200) || (ok && version != 3) {
			t.Fatal("machine precondition escaped resource, tenant or kind")
		}
	}
}
func TestMachineResourceAnonymousBoundaryAndContract(t *testing.T) {
	handler := Handler(&Authenticator{})
	for _, tt := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/machine-openapi.json", 200}, {"GET", "/machine-openapi.yaml", 200},
		{"POST", "/machine-openapi.json", 400},
		{"GET", "/v1/machine/applications/" + strings.Repeat("a", 32) + "/identity", 401},
		{"GET", "/v1/machine/applications/" + strings.Repeat("a", 32) + "/identity?tenant=ignored", 400},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tt.method, "http://api.localhost:8083"+tt.path, nil)
		handler.ServeHTTP(w, r)
		if w.Code != tt.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("machine realm or contract boundary failed")
		}
		if tt.status == 401 && !strings.Contains(w.Header().Get("WWW-Authenticate"), "machine-resource") {
			t.Fatal("machine route selected User realm")
		}
	}
}
