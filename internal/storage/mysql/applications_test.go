package mysql

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func applicationFixture(t *testing.T, s Store, tenant ID) ID {
	t.Helper()
	id, e := NewID()
	if e != nil {
		t.Fatal("application fixture ID failed")
	}
	if _, e = s.db.Exec(`INSERT INTO applications(id,code,name) VALUES(?,?,'Fixture application')`, id, "fixture-"+id.String()); e != nil {
		t.Fatal("application fixture creation failed")
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM tenant_applications WHERE application_id=?`, `DELETE FROM oauth_clients WHERE application_id=?`, `DELETE FROM applications WHERE id=?`} {
			if _, e := s.db.Exec(q, id); e != nil {
				t.Error("application fixture cleanup failed")
			}
		}
	})
	if _, e = s.db.Exec(`INSERT INTO tenant_applications(tenant_id,application_id) VALUES(?,?)`, tenant, id); e != nil {
		t.Fatal("application association fixture failed")
	}
	for _, suffix := range []string{"a", "b"} {
		if _, e = s.db.Exec(`INSERT INTO oauth_clients(client_id,application_id,redirect_uri,scopes,audience) VALUES(?,?,'http://127.0.0.1:9878/callback','openid platform:read','https://api.sso.local')`, "fixture-"+id.String()+"-"+suffix, id); e != nil {
			t.Fatal("client fixture failed")
		}
	}
	return id
}
func TestApplicationMetadataAndTenantOwnerBoundaries(t *testing.T) {
	s := testStore(t)
	a := fixture(t, s)
	b := fixture(t, s)
	app := applicationFixture(t, s, a.Tenant)
	ctx := context.Background()
	repo := s.ForTenant(a.Tenant, a.Membership)
	items, e := repo.APIApplications(ctx, 2, ID{})
	if e != nil || len(items) != 1 || items[0].ID != app.String() || items[0].ClientCount != 2 || !items[0].Enabled || !items[0].Available {
		t.Fatal("application metadata mismatch")
	}
	if _, e = s.ForTenant(b.Tenant, b.Membership).APIApplication(ctx, app); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-space association disclosed")
	}
	if _, e = s.ForTenant(a.Tenant, b.Membership).APIApplication(ctx, app); !errors.Is(e, ErrDenied) {
		t.Fatal("foreign actor accepted")
	}
	clients, e := repo.APIApplicationClients(ctx, app, 1, "")
	if e != nil || len(clients) != 1 {
		t.Fatal("client page failed")
	}
	next, e := repo.APIApplicationClients(ctx, app, 2, clients[0].Client)
	if e != nil || len(next) != 1 || next[0].Client <= clients[0].Client || len(next[0].Scopes) != 2 {
		t.Fatal("client cursor not scoped/ordered")
	}
	if _, e = repo.APIApplicationClients(ctx, app, 102, ""); !errors.Is(e, ErrInvalid) {
		t.Fatal("client query unbounded")
	}
}
func TestApplicationStateVersionsAuditAndGlobalSuspension(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	app := applicationFixture(t, s, b.Tenant)
	repo := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	before := count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant)
	value, e := repo.SetApplicationEnabled(ctx, app, false, 1)
	if e != nil || value.Enabled || value.Version != 2 {
		t.Fatal("application disable failed")
	}
	value, e = repo.SetApplicationEnabled(ctx, app, false, 2)
	if e != nil || value.Version != 2 {
		t.Fatal("same-state operation changed version")
	}
	if count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant) != before+1 {
		t.Fatal("no-op repeated audit")
	}
	if _, e = repo.SetApplicationEnabled(ctx, app, true, 1); !errors.Is(e, ErrConflict) {
		t.Fatal("stale app version accepted")
	}
	value, e = repo.SetApplicationEnabled(ctx, app, true, 2)
	if e != nil || !value.Enabled || value.Version != 3 {
		t.Fatal("application recovery failed")
	}
	if _, e = s.db.Exec(`UPDATE applications SET active=FALSE WHERE id=?`, app); e != nil {
		t.Fatal("global suspension fixture failed")
	}
	value, e = repo.SetApplicationEnabled(ctx, app, false, 3)
	if e != nil || value.Version != 4 {
		t.Fatal("global suspension blocked local disable")
	}
	if _, e = repo.SetApplicationEnabled(ctx, app, true, 4); !errors.Is(e, ErrApplicationUnavailable) {
		t.Fatal("tenant bypassed global suspension")
	}
	if count(t, s, `SELECT authz_version FROM tenants WHERE id=?`, b.Tenant) != 1 {
		t.Fatal("unrelated application grants invalidated by tenant version")
	}
	if count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant) != before+3 {
		t.Fatal("application audit not atomic with changes")
	}
	if count(t, s, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND type IN ('application.enabled','application.disabled')`, b.Tenant) != 3 {
		t.Fatal("application outbox missing")
	}
}
func TestApplicationConcurrentDisableHasOneWinner(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	app := applicationFixture(t, s, b.Tenant)
	repo := s.ForTenant(b.Tenant, b.Membership)
	start := make(chan struct{})
	result := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := repo.SetApplicationEnabled(context.Background(), app, false, 1)
			result <- e
		}()
	}
	close(start)
	wg.Wait()
	close(result)
	success, conflict := 0, 0
	for e := range result {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflict++
		} else {
			t.Fatal("concurrent application operation failed")
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent stale version not rejected")
	}
}
