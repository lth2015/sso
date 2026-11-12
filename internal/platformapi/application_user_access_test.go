package platformapi

import (
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http/httptest"
	"testing"
)

func TestUserApplicationAccessPreconditionsAndListShape(t *testing.T) {
	tenant, _ := store.ParseID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	app, _ := store.ParseID("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	policy := store.ApplicationUserAccessPolicy{Tenant: tenant.String(), Application: app.String(), Version: "7"}
	tag := userAccessETag(policy)
	for _, v := range []struct {
		tag    string
		status int
	}{
		{tag, 200}, {"", 428}, {`"app-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-7"`, 400},
		{`"ua-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-9223372036854775808"`, 400},
		{`"ua-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-07"`, 400},
		{`"ua-cccccccccccccccccccccccccccccccc-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-7"`, 412},
	} {
		r := httptest.NewRequest("PUT", "/", nil)
		if v.tag != "" {
			r.Header.Set("If-Match", v.tag)
		}
		w := httptest.NewRecorder()
		revision, ok := userAccessPrecondition(w, r, tenant, app)
		if ok != (v.status == 200) || w.Code != v.status || (ok && revision != 7) {
			t.Fatal("User admission resource precondition escaped namespace/version bounds")
		}
	}
	path := "/v1/tenants/" + tenant.String() + "/applications/" + app.String() + "/user-access"
	if !isList(path) || isList(path+"/members/"+tenant.String()) || isList("/admin/user-access") {
		t.Fatal("User admission query surface is not fixed")
	}
	for _, length := range []int64{-1, 1, 4097} {
		r := httptest.NewRequest("GET", path, nil)
		r.ContentLength = length
		w := httptest.NewRecorder()
		if userAccessRead(w, r) || w.Code != 400 {
			t.Fatal("User access GET admitted a body")
		}
	}
}
