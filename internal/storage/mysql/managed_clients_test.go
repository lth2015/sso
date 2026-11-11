package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lth2015/sso/internal/adapter/hydra"
)

func cleanupManagedApplications(t *testing.T, s Store, tenant ID) {
	t.Helper()
	rows, e := s.db.Query(`SELECT application_id FROM managed_applications WHERE tenant_id=?`, tenant)
	if e != nil {
		t.Error("managed fixture lookup failed")
		return
	}
	apps := []ID{}
	for rows.Next() {
		var id ID
		if e = rows.Scan(&id); e != nil {
			t.Error(e)
			break
		}
		apps = append(apps, id)
	}
	rows.Close()
	for _, table := range []string{"machine_outbox_events", "machine_events", "machine_credentials", "service_principals"} {
		if _, e = s.db.Exec("DELETE FROM "+table+" WHERE tenant_id=?", tenant); e != nil {
			t.Error("machine fixture cleanup failed")
		}
	}
	for _, app := range apps {
		if _, e = s.db.Exec(`DELETE FROM managed_oauth_clients WHERE tenant_id=? AND application_id=?`, tenant, app); e != nil {
			t.Error("managed client fixture cleanup failed")
		}
		if _, e = s.db.Exec(`DELETE FROM oauth_clients WHERE application_id=?`, app); e != nil {
			t.Error("managed registry fixture cleanup failed")
		}
		if _, e = s.db.Exec(`DELETE FROM managed_applications WHERE tenant_id=? AND application_id=?`, tenant, app); e != nil {
			t.Error("managed ownership cleanup failed")
		}
		if _, e = s.db.Exec(`DELETE FROM tenant_applications WHERE tenant_id=? AND application_id=?`, tenant, app); e != nil {
			t.Error("managed association cleanup failed")
		}
		if _, e = s.db.Exec(`DELETE FROM applications WHERE id=?`, app); e != nil {
			t.Error("managed application cleanup failed")
		}
	}
}
func TestManagedRedirectProfileBounds(t *testing.T) {
	for _, test := range []struct {
		value string
		ok    bool
	}{
		{"https://app.example.invalid/oidc/callback", true}, {"https://app.example.invalid:8443/callback?context=one", true}, {"http://127.0.0.1:9876/callback", true}, {"http://[::1]:9876/callback", true},
		{"http://app.example.invalid/callback", false}, {"http://localhost:9876/callback", false}, {"http://127.0.0.1:80/callback", false}, {"https://user:secret@app.example.invalid/callback", false}, {"https://*.example.invalid/callback", false}, {"https://app.example.invalid/callback#", false}, {"https://app.example.invalid/callback#token", false}, {"https://app.example.invalid", false}, {"https://app.example.invalid:99999/callback", false}, {"https://app.example.invalid/\\callback", false}, {"https://app.example.invalid/path\"quote", false}, {"https://app.example.invalid/path%22quote", true},
		{"javascript:alert(1)", false},
	} {
		if ValidManagedRedirect(test.value) != test.ok {
			t.Fatal("redirect profile validation mismatch")
		}
	}
}
func TestManagedClientPendingIsolationRevokeAndNoop(t *testing.T) {
	s := testStore(t)
	a := fixture(t, s)
	b := fixture(t, s)
	ctx := context.Background()
	repo := s.ForTenant(a.Tenant, a.Membership)
	app, e := repo.APICreateApplication(ctx, "  New application  ")
	if e != nil || !app.Managed || app.ClientCount != 0 {
		t.Fatal("owned application creation failed")
	}
	appID, _ := ParseID(app.ID)
	if count(t, s, `SELECT authz_version FROM tenants WHERE id=?`, a.Tenant) != 1 {
		t.Fatal("app creation revoked unrelated grants")
	}
	client, e := repo.APICreateManagedClient(ctx, appID, "Native desktop", "http://127.0.0.1:9876/callback")
	if e != nil || client.State != "pending" || client.Active || client.Version != 1 {
		t.Fatal("registration not initially closed")
	}
	id, _ := ParseID(client.ID)
	if _, e = s.OAuthClient(ctx, client.ID); !errors.Is(e, ErrDenied) {
		t.Fatal("pending client login accepted")
	}
	if _, e = s.ForTenant(b.Tenant, b.Membership).APIManagedClient(ctx, appID, id); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign client metadata disclosed")
	}
	if _, e = repo.APICreateManagedClient(ctx, b.Root, "Foreign", "https://app.example.invalid/callback"); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign application accepted")
	}
	legacy := applicationFixture(t, s, a.Tenant)
	if _, e = repo.APICreateManagedClient(ctx, legacy, "Shared", "https://app.example.invalid/callback"); !errors.Is(e, ErrApplicationUnmanaged) {
		t.Fatal("shared registry takeover")
	}
	if _, e = repo.SetApplicationEnabled(ctx, appID, false, 1); e != nil {
		t.Fatal("owned app disable failed")
	}
	if _, e = repo.APICreateManagedClient(ctx, appID, "Disabled", "https://app.example.invalid/callback"); !errors.Is(e, ErrApplicationUnavailable) {
		t.Fatal("disabled app allowed registration")
	}
	if _, _, e = repo.ReconcileManagedClient(ctx, appID, id, 1, hydra.New()); !errors.Is(e, ErrApplicationUnavailable) {
		t.Fatal("disabled app allowed activation")
	}
	if _, e = repo.RevokeManagedClient(ctx, appID, id, 2); !errors.Is(e, ErrConflict) {
		t.Fatal("stale client version accepted")
	}
	client, e = repo.RevokeManagedClient(ctx, appID, id, 1)
	if e != nil || client.State != "revoked" || client.Active || client.Version != 2 {
		t.Fatal("pending revocation failed")
	}
	if _, e = repo.RevokeManagedClient(ctx, appID, id, 2); e != nil {
		t.Fatal("same-state revoke failed")
	}
	if count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=? AND action='client.revoked'`, a.Tenant) != 1 {
		t.Fatal("no-op revocation repeated audit")
	}
}

// The HTTP engine below injects response loss/conflict into a real MySQL
// authority transaction. Full protocol behavior is separately tested with Hydra.
func TestManagedClientReconcileFailureConflictAndRevocation(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	repo := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	app, e := repo.APICreateApplication(ctx, "Reconciliation")
	if e != nil {
		t.Fatal(e)
	}
	appID, _ := ParseID(app.ID)
	client, e := repo.APICreateManagedClient(ctx, appID, "Native", "https://app.example.invalid/callback")
	if e != nil {
		t.Fatal(e)
	}
	id, _ := ParseID(client.ID)
	var current *hydra.PublicClientProfile
	failAfterCreate := true
	var mutex sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "GET":
			if current == nil {
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(current)
		case "POST":
			var p hydra.PublicClientProfile
			if json.NewDecoder(r.Body).Decode(&p) != nil {
				w.WriteHeader(400)
				return
			}
			current = &p
			if failAfterCreate {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(p)
		case "DELETE":
			current = nil
			w.WriteHeader(204)
		}
	}))
	defer server.Close()
	admin := hydra.New()
	admin.Base = server.URL
	if _, _, e = repo.ReconcileManagedClient(ctx, appID, id, 1, admin); e == nil {
		t.Fatal("lost engine reply treated as activation")
	}
	closed, e := repo.APIManagedClient(ctx, appID, id)
	if e != nil || closed.State != "pending" || closed.Active || closed.Version != 1 {
		t.Fatal("failure did not roll back local activation")
	}
	mutex.Lock()
	failAfterCreate = false
	mutex.Unlock()
	client, observed, e := repo.ReconcileManagedClient(ctx, appID, id, 1, admin)
	if e != nil || observed != "present" || !client.Active || client.Version != 2 {
		t.Fatal("matching remote orphan not recoverable")
	}
	client, _, e = repo.ReconcileManagedClient(ctx, appID, id, 2, admin)
	if e != nil || client.Version != 2 {
		t.Fatal("repeat reconcile changed authority")
	}
	mutex.Lock()
	current.Metadata["tenant_id"] = "foreign"
	mutex.Unlock()
	if _, _, e = repo.ReconcileManagedClient(ctx, appID, id, 2, admin); !errors.Is(e, hydra.ErrClientProfileConflict) {
		t.Fatal("foreign remote object adopted")
	}
	mutex.Lock()
	current.Metadata["tenant_id"] = b.Tenant.String()
	mutex.Unlock()
	if _, e = repo.SetApplicationEnabled(ctx, appID, false, 1); e != nil {
		t.Fatal("owned app disable failed")
	}
	client, e = repo.RevokeManagedClient(ctx, appID, id, 2)
	if e != nil || client.Active || client.Version != 3 {
		t.Fatal("active revocation failed")
	}
	if _, e = s.OAuthClient(ctx, id.String()); !errors.Is(e, ErrDenied) {
		t.Fatal("revocation waited on remote cleanup")
	}
	mutex.Lock()
	current.Metadata["tenant_id"] = "foreign"
	mutex.Unlock()
	if _, _, e = repo.ReconcileManagedClient(ctx, appID, id, 3, admin); !errors.Is(e, hydra.ErrClientProfileConflict) {
		t.Fatal("revoked cleanup deleted a foreign profile")
	}
	mutex.Lock()
	foreignStillPresent := current != nil
	if current != nil {
		current.Metadata["tenant_id"] = b.Tenant.String()
	}
	mutex.Unlock()
	if !foreignStillPresent {
		t.Fatal("foreign protocol object was removed")
	}
	client, observed, e = repo.ReconcileManagedClient(ctx, appID, id, 3, admin)
	if e != nil || observed != "absent" || client.State != "revoked" || client.Active {
		t.Fatal("revoked reconciliation resurrected authority")
	}
	if _, _, e = repo.ReconcileManagedClient(ctx, appID, id, 3, admin); e != nil {
		t.Fatal("absent engine cleanup not idempotent")
	}
	mutex.Lock()
	present := current != nil
	mutex.Unlock()
	if present {
		t.Fatal("owned engine client retained")
	}
}
func TestManagedClientConcurrentReconcileCannotResurrectRevocation(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	repo := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	app, e := repo.APICreateApplication(ctx, "Race")
	if e != nil {
		t.Fatal(e)
	}
	appID, _ := ParseID(app.ID)
	client, e := repo.APICreateManagedClient(ctx, appID, "Native", "https://app.example.invalid/callback")
	if e != nil {
		t.Fatal(e)
	}
	id, _ := ParseID(client.ID)
	start := make(chan struct{})
	release := make(chan struct{})
	var current *hydra.PublicClientProfile
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			if current == nil {
				w.WriteHeader(404)
			} else {
				json.NewEncoder(w).Encode(current)
			}
			return
		}
		var p hydra.PublicClientProfile
		_ = json.NewDecoder(r.Body).Decode(&p)
		close(start)
		<-release
		current = &p
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(p)
	}))
	defer server.Close()
	admin := hydra.New()
	admin.Base = server.URL
	done := make(chan error, 1)
	go func() { _, _, err := repo.ReconcileManagedClient(ctx, appID, id, 1, admin); done <- err }()
	<-start
	revoked := make(chan error, 1)
	go func() { _, err := repo.RevokeManagedClient(ctx, appID, id, 1); revoked <- err }()
	close(release)
	if e = <-done; e != nil {
		t.Fatal("reconciliation race failed")
	}
	if e = <-revoked; !errors.Is(e, ErrConflict) {
		t.Fatal("revocation ignored newly published version")
	}
	if _, e = repo.RevokeManagedClient(ctx, appID, id, 2); e != nil {
		t.Fatal(e)
	}
	if _, e = s.OAuthClient(ctx, id.String()); !errors.Is(e, ErrDenied) {
		t.Fatal("completed revoke not authoritative")
	}
}

func TestManagedClientCancelledProtocolCallRemainsPending(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	repo := s.ForTenant(b.Tenant, b.Membership)
	app, e := repo.APICreateApplication(context.Background(), "Cancellation")
	if e != nil {
		t.Fatal(e)
	}
	appID, _ := ParseID(app.ID)
	client, e := repo.APICreateManagedClient(context.Background(), appID, "Native", "https://app.example.invalid/callback")
	if e != nil {
		t.Fatal(e)
	}
	id, _ := ParseID(client.ID)
	reached := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(reached); <-r.Context().Done() }))
	defer server.Close()
	admin := hydra.New()
	admin.Base = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, _, e = repo.ReconcileManagedClient(ctx, appID, id, 1, admin); e == nil {
		t.Fatal("cancelled RPC activated client")
	}
	select {
	case <-reached:
	default:
		t.Fatal("protocol cancellation fixture not reached")
	}
	value, e := repo.APIManagedClient(context.Background(), appID, id)
	if e != nil || value.Active || value.State != "pending" || value.Version != 1 {
		t.Fatal("cancelled reconcile escaped rollback")
	}
	if count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=? AND action='client.activated'`, b.Tenant) != 0 {
		t.Fatal("cancelled activation recorded as committed")
	}
}
