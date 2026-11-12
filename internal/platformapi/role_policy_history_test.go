package platformapi

import (
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRolePolicyHistoryRequestBounds(t *testing.T) {
	base := "/v1/tenants/" + strings.Repeat("a", 32) + "/roles/" + strings.Repeat("b", 32) + "/policy-history"
	if !isList(base) || isList("/admin/clients/policy-history") {
		t.Fatal("history pagination URI boundary invalid")
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=", "?after=", "?after=a", "?after=" + strings.Repeat("b", 32) + "&after=" + strings.Repeat("b", 32), "?kind=management", "?unknown=private-value"} {
		mux := http.NewServeMux()
		mountRolePolicyHistory(mux, store.Store{})
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET", base+query, nil)
		mux.ServeHTTP(response, request)
		if response.Code != 400 || strings.Contains(response.Body.String(), "private-value") {
			t.Fatal("history malformed query accepted or echoed")
		}
	}
	for _, length := range []int64{1, -1} {
		mux := http.NewServeMux()
		mountRolePolicyHistory(mux, store.Store{})
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET", base, strings.NewReader("private-body"))
		request.ContentLength = length
		mux.ServeHTTP(response, request)
		if response.Code != 400 || strings.Contains(response.Body.String(), "private-body") {
			t.Fatal("history GET body accepted or echoed")
		}
	}
}
