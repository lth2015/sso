package platformapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

func TestInvitationPreconditionBindsResourceTenantAndRevision(t *testing.T) {
	tenant, _ := store.ParseID(strings.Repeat("a", 32))
	id, _ := store.ParseID(strings.Repeat("b", 32))
	valid := invitationETag(tenant, id.String(), 7)
	for _, tt := range []struct {
		values []string
		status int
	}{
		{nil, 428}, {[]string{valid}, 200}, {[]string{valid, valid}, 400},
		{[]string{"*"}, 400}, {[]string{"W/" + valid}, 400},
		{[]string{strings.Replace(valid, "-7\"", "-07\"", 1)}, 400},
		{[]string{strings.Replace(valid, "-7\"", "-9223372036854775808\"", 1)}, 400},
		{[]string{strings.Replace(valid, tenant.String(), strings.Repeat("c", 32), 1)}, 412},
		{[]string{strings.Replace(valid, id.String(), strings.Repeat("c", 32), 1)}, 412},
		{[]string{strings.Replace(valid, "invite-", "cred-", 1)}, 400},
	} {
		r := httptest.NewRequest("POST", "http://api.localhost:8083/", nil)
		r.Header["If-Match"] = tt.values
		w := httptest.NewRecorder()
		revision, ok := invitationPrecondition(w, r, tenant, id)
		if w.Code != tt.status || ok != (tt.status == 200) || (ok && revision != 7) {
			t.Fatal("invitation precondition failed closed or crossed its resource")
		}
	}
}
