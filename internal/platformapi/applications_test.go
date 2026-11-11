package platformapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lth2015/sso/internal/adapter/hydra"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

func TestApplicationPreconditionBindsResource(t *testing.T) {
	tenant, _ := store.ParseID(strings.Repeat("a", 32))
	app, _ := store.ParseID(strings.Repeat("b", 32))
	valid := `"app-` + tenant.String() + `-` + app.String() + `-2"`
	for _, test := range []struct {
		header  []string
		status  int
		version int64
	}{{nil, 428, 0}, {[]string{valid}, 200, 2}, {[]string{valid, valid}, 400, 0}, {[]string{`"app-` + strings.Repeat("c", 32) + `-` + app.String() + `-2"`}, 412, 0}, {[]string{`"app-` + tenant.String() + `-` + strings.Repeat("c", 32) + `-2"`}, 412, 0}, {[]string{`"authz-2"`}, 400, 0}, {[]string{strings.Replace(valid, "-2\"", "-02\"", 1)}, 400, 0}, {[]string{strings.Replace(valid, "-2\"", "-99999999999999999999\"", 1)}, 400, 0}} {
		request := httptest.NewRequest("POST", "http://api.localhost:8083/", nil)
		request.Header["If-Match"] = test.header
		response := httptest.NewRecorder()
		version, ok := applicationPrecondition(response, request, tenant, app)
		if response.Code != test.status || ok != (test.status == 200) || version != test.version {
			t.Fatal("application precondition did not bind exact target/version")
		}
	}
}
func TestApplicationClientCursorBounds(t *testing.T) {
	for _, test := range []struct {
		query string
		ok    bool
	}{{"?limit=1&after=local-demo-a", true}, {"?limit=100&after=client%3F%26%3D", true}, {"?after=" + strings.Repeat("a", 129), false}, {"?after=client%0A", false}, {"?after=%00", false}, {"?limit=101", false}, {"?after=a&after=b", false}, {"?unknown=1", false}} {
		request := httptest.NewRequest("GET", "http://api.localhost:8083/"+test.query, nil)
		response := httptest.NewRecorder()
		_, _, ok := clientPagination(response, request)
		if ok != test.ok {
			t.Fatal("client cursor boundary mismatch")
		}
	}
}
func verifyApplicationPrimaryChecks(t *testing.T, db *sql.DB, repo store.Store, a store.Authorization) {
	t.Helper()
	ctx := context.Background()
	a.Recent = false
	items, err := repo.ForAuthorization(a).APIApplications(ctx, 101, store.ID{})
	if err != nil || len(items) == 0 {
		t.Fatal("application OAuth fixture unavailable")
	}
	app, err := store.ParseID(items[0].ID)
	if err != nil {
		t.Fatal("application fixture ID unavailable")
	}
	var authTime int64
	if err = db.QueryRow(`SELECT auth_time FROM sso_sessions WHERE id=? AND user_id=?`, a.Session, a.User).Scan(&authTime); err != nil {
		t.Fatal("application MFA fixture unavailable")
	}
	if _, err = db.Exec(`UPDATE sso_sessions SET auth_time=UNIX_TIMESTAMP()-301 WHERE id=? AND user_id=?`, a.Session, a.User); err != nil {
		t.Fatal("application MFA fixture aging failed")
	}
	defer func() {
		if _, e := db.Exec(`UPDATE sso_sessions SET auth_time=? WHERE id=? AND user_id=?`, authTime, a.Session, a.User); e != nil {
			t.Error("application MFA fixture restoration failed")
		}
	}()
	// The new organization writers must enforce MFA in their own transaction,
	// even when a trusted internal caller did not request Recent checks.
	var root store.ID
	if err = db.QueryRow(`SELECT org_id FROM tenant_roots WHERE tenant_id=?`, a.Tenant).Scan(&root); err != nil {
		t.Fatal("organization root fixture unavailable")
	}
	if _, err = repo.ForAuthorization(a).RenameOrganization(ctx, root, "Forbidden rename", 1); !errors.Is(err, store.ErrStepUp) {
		t.Fatal("rename trusted caller Recent flag")
	}
	if _, err = repo.ForAuthorization(a).DeleteOrganization(ctx, root, 1); !errors.Is(err, store.ErrStepUp) {
		t.Fatal("deletion trusted caller Recent flag")
	}
	if err = repo.ForAuthorization(a).UnassignOrganization(ctx, a.Member, root, 1); !errors.Is(err, store.ErrStepUp) {
		t.Fatal("unassign trusted caller Recent flag")
	}
	if _, err = repo.ForAuthorization(a).APICreateApplication(ctx, "Aged MFA"); !errors.Is(err, store.ErrStepUp) {
		t.Fatal("application creation trusted caller Recent flag")
	}
	for _, owned := range items {
		if !owned.Managed {
			continue
		}
		ownedID, _ := store.ParseID(owned.ID)
		if _, err = repo.ForAuthorization(a).APICreateManagedClient(ctx, ownedID, "Aged MFA", "http://127.0.0.1:9876/callback"); !errors.Is(err, store.ErrStepUp) {
			t.Fatal("client registration trusted caller Recent flag")
		}
		clients, e := repo.ForAuthorization(a).APIApplicationClients(ctx, ownedID, 2, "")
		if e != nil || len(clients) == 0 {
			t.Fatal("managed MFA client fixture unavailable")
		}
		client, _ := store.ParseID(clients[0].Client)
		if _, err = repo.ForAuthorization(a).RevokeManagedClient(ctx, ownedID, client, clients[0].Version); !errors.Is(err, store.ErrStepUp) {
			t.Fatal("client revoke trusted caller Recent flag")
		}
		if _, _, err = repo.ForAuthorization(a).ReconcileManagedClient(ctx, ownedID, client, clients[0].Version, hydra.New()); !errors.Is(err, store.ErrStepUp) {
			t.Fatal("client reconcile trusted caller Recent flag")
		}
	}
	if _, err = repo.ForAuthorization(a).APIApplication(ctx, app); err != nil {
		t.Fatal("application read incorrectly required recent MFA")
	}
	if _, err = repo.ForAuthorization(a).SetApplicationEnabled(ctx, app, items[0].Enabled, items[0].Version); !errors.Is(err, store.ErrStepUp) {
		t.Fatal("application write trusted caller Recent flag")
	}
}
