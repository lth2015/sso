package platformapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditExportQueryCannotSelectOtherSourcesOrUnboundedRows(t *testing.T) {
	for _, tt := range []struct {
		query, kind string
		limit       int
		ok          bool
	}{
		{"", "management", 50, true}, {"?kind=machine&limit=100", "machine", 100, true}, {"?kind=management&limit=1&after=" + strings.Repeat("a", 32), "management", 1, true},
		{"?kind=", "", 0, false}, {"?kind=users", "", 0, false}, {"?kind=machine&kind=management", "", 0, false}, {"?kind=machine&limit=101", "", 0, false}, {"?kind=machine&limit=0", "", 0, false}, {"?kind=machine&after=bad", "", 0, false}, {"?kind=machine&tenant_id=untrusted", "", 0, false}, {"?kind=machine&limit=1&limit=2", "", 0, false}, {"?kind=machine&%zz", "", 0, false},
	} {
		r := httptest.NewRequest("GET", "http://api.localhost:8083/"+tt.query, nil)
		w := httptest.NewRecorder()
		kind, n, _, ok := auditExportPagination(w, r)
		if ok != tt.ok || (ok && (kind != tt.kind || n != tt.limit)) {
			t.Fatal("audit export query boundary failed")
		}
	}
}
