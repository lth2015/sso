package platformapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProtocolObservationEndpointsRemainInTheUserRealm(t *testing.T) {
	handler := Handler(&Authenticator{})
	id := strings.Repeat("a", 32)
	for _, path := range []string{"/v1/tenants/" + id + "/applications/" + id + "/clients/" + id + "/protocol-status", "/v1/tenants/" + id + "/applications/" + id + "/service-principals/" + id + "/credentials/" + id + "/protocol-status"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://api.localhost:8083"+path, nil)
		handler.ServeHTTP(w, r)
		if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("WWW-Authenticate"), "platform") {
			t.Fatal("read-only private profile observation escaped User authentication")
		}
	}
}
