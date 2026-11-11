package mysql

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
)

func testStore(t *testing.T) Store {
	t.Helper()
	if os.Getenv("SSO_M1_TEST") != "true" {
		t.Skip("requires real isolated MySQL: scripts/m1 test")
	}
	cfg := driver.NewConfig()
	cfg.User = "business_app"
	cfg.Passwd = os.Getenv("M1_DB_PASSWORD")
	cfg.Net = "tcp"
	cfg.Addr = "mysql:3306"
	cfg.DBName = "sso_business"
	cfg.ParseTime = true
	cfg.Timeout = 2 * time.Second
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { db.Close() })
	if err = db.Ping(); err != nil {
		t.Fatal("business DB unavailable")
	}
	return Store{db}
}
func newUser(t *testing.T, s Store) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO users(id) VALUES(?)`, id); err != nil {
		t.Fatal(err)
	}
	return id
}
func fixture(t *testing.T, s Store) Bootstrap {
	t.Helper()
	u := newUser(t, s)
	b, err := s.BootstrapTenant(context.Background(), u, "test-"+u.String(), "Root")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		users := []ID{}
		rows, err := s.db.Query(`SELECT user_id FROM memberships WHERE tenant_id=?`, b.Tenant)
		if err != nil {
			t.Error(err)
		} else {
			for rows.Next() {
				var id ID
				if err := rows.Scan(&id); err != nil {
					t.Error(err)
				} else {
					users = append(users, id)
				}
			}
			if err := rows.Err(); err != nil {
				t.Error(err)
			}
			rows.Close()
		}
		cleanupManagedApplications(t, s, b.Tenant)
		// Remove only this test's freshly generated tenant and related identities.
		for _, table := range []string{"role_binding_changes", "role_policies", "outbox_events", "audit_events", "invitations", "org_memberships", "role_bindings", "roles", "org_closure", "tenant_roots"} {
			if _, err := s.db.Exec("DELETE FROM "+table+" WHERE tenant_id=?", b.Tenant); err != nil {
				t.Error(err)
			}
		}
		// Self-referencing org FK requires deletion from leaves towards the root.
		for range 17 {
			_, err := s.db.Exec(`DELETE leaf FROM org_nodes leaf LEFT JOIN org_nodes child ON child.tenant_id=leaf.tenant_id AND child.parent_id=leaf.id WHERE leaf.tenant_id=? AND child.id IS NULL`, b.Tenant)
			if err != nil {
				t.Error(err)
				break
			}
		}
		_, _ = s.db.Exec(`DELETE FROM memberships WHERE tenant_id=?`, b.Tenant)
		_, _ = s.db.Exec(`DELETE FROM tenants WHERE id=?`, b.Tenant)
		for _, id := range users {
			if _, err := s.db.Exec(`DELETE FROM login_aliases WHERE user_id=?`, id); err != nil {
				t.Error(err)
			}
			if _, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id); err != nil {
				t.Error(err)
			}
		}
	})
	return b
}
func count(t *testing.T, s Store, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func add(t *testing.T, s Store, b Bootstrap, parent ID, name string, version int64) ID {
	t.Helper()
	id, err := s.ForTenant(b.Tenant, b.Membership).AddOrganization(context.Background(), parent, name, version)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBootstrapAtomicAndConstraints(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	for _, table := range []string{"memberships", "org_nodes", "tenant_roots", "org_closure", "role_bindings", "audit_events", "outbox_events"} {
		if n := count(t, s, "SELECT COUNT(*) FROM "+table+" WHERE tenant_id=?", b.Tenant); n != 1 {
			t.Fatalf("%s count=%d", table, n)
		}
	}
	id, _ := NewID()
	if _, err := s.db.Exec(`INSERT INTO org_nodes(id,tenant_id,name) VALUES(?,?,'Other root')`, id, b.Tenant); err == nil {
		t.Fatal("duplicate root allowed")
	}
	// A duplicate slug fails after beginning the bootstrap, with no partial tenant.
	var slug string
	var user ID
	if err := s.db.QueryRow(`SELECT t.slug,m.user_id FROM tenants t JOIN memberships m ON m.tenant_id=t.id WHERE t.id=?`, b.Tenant).Scan(&slug, &user); err != nil {
		t.Fatal(err)
	}
	before := count(t, s, `SELECT COUNT(*) FROM tenants`)
	if _, err := s.BootstrapTenant(context.Background(), user, slug, "Root"); err == nil {
		t.Fatal("duplicate slug accepted")
	}
	if after := count(t, s, `SELECT COUNT(*) FROM tenants`); after != before {
		t.Fatal("partial bootstrap persisted")
	}
}
func TestTenantIsolationAndForeignKeys(t *testing.T) {
	s := testStore(t)
	a := fixture(t, s)
	b := fixture(t, s)
	if _, err := s.ForTenant(a.Tenant, b.Membership).ListOrganizations(context.Background()); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-tenant actor: %v", err)
	}
	if _, err := s.ForTenant(a.Tenant, a.Membership).AddOrganization(context.Background(), b.Root, "Illegal", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant parent: %v", err)
	}
	id, _ := NewID()
	if _, err := s.db.Exec(`INSERT INTO org_nodes(id,tenant_id,parent_id,name) VALUES(?,?,?,'Cross tenant')`, id, a.Tenant, b.Root); err == nil {
		t.Fatal("cross-tenant FK accepted")
	}
	var role ID
	if err := s.db.QueryRow(`SELECT id FROM roles WHERE tenant_id=?`, a.Tenant).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO role_bindings VALUES(?,?,?)`, a.Tenant, b.Membership, role); err == nil {
		t.Fatal("cross-tenant binding accepted")
	}
}
func TestMoveDryRunClosureCycleAndVersion(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	repo := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	a := add(t, s, b, b.Root, "A", 1)
	child := add(t, s, b, a, "Child", 2)
	other := add(t, s, b, b.Root, "Other", 3)
	before := count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant)
	plan, err := repo.MoveOrganization(ctx, a, other, 4, true)
	if err != nil || plan.Nodes != 2 || plan.Depth != 3 {
		t.Fatalf("preview %+v: %v", plan, err)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM org_closure WHERE tenant_id=? AND ancestor_id=? AND descendant_id=?`, b.Tenant, other, child); n != 0 {
		t.Fatal("dry run modified closure")
	}
	if after := count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant); after != before {
		t.Fatal("dry run wrote audit")
	}
	if _, err = repo.MoveOrganization(ctx, a, child, 4, false); !errors.Is(err, ErrCycle) {
		t.Fatalf("cycle allowed: %v", err)
	}
	if _, err = repo.MoveOrganization(ctx, b.Root, a, 4, false); !errors.Is(err, ErrRoot) {
		t.Fatalf("root move allowed: %v", err)
	}
	if _, err = repo.MoveOrganization(ctx, a, other, 3, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale version allowed: %v", err)
	}
	if _, err = repo.MoveOrganization(ctx, a, other, 4, false); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT depth FROM org_closure WHERE tenant_id=? AND ancestor_id=? AND descendant_id=?`, b.Tenant, b.Root, child); n != 3 {
		t.Fatalf("wrong root path depth=%d", n)
	}
	if n := count(t, s, `SELECT depth FROM org_closure WHERE tenant_id=? AND ancestor_id=? AND descendant_id=?`, b.Tenant, other, child); n != 2 {
		t.Fatalf("new ancestor path depth=%d", n)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM org_closure WHERE tenant_id=? AND ancestor_id=? AND descendant_id=? AND depth=1`, b.Tenant, a, child); n != 1 {
		t.Fatal("subtree path lost")
	}
	if n := count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant); n != before+1 {
		t.Fatal("move audit missing")
	}
}
func TestConcurrentMovesSerialize(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	a := add(t, s, b, b.Root, "A", 1)
	c := add(t, s, b, b.Root, "B", 2)
	repo := s.ForTenant(b.Tenant, b.Membership)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, pair := range [][2]ID{{a, c}, {c, a}} {
		wg.Add(1)
		go func(pair [2]ID) {
			defer wg.Done()
			<-start
			_, err := repo.MoveOrganization(context.Background(), pair[0], pair[1], 3, false)
			results <- err
		}(pair)
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}
func TestLastOwnerAndConcurrentSuspension(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, b.Membership)
	if err := repo.SuspendMember(ctx, b.Membership, 1); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("last owner: %v", err)
	}
	user := newUser(t, s)
	member, _ := NewID()
	if _, err := s.db.Exec(`INSERT INTO memberships(id,tenant_id,user_id) VALUES(?,?,?)`, member, b.Tenant, user); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO role_bindings SELECT tenant_id,?,id FROM roles WHERE tenant_id=? AND code='owner'`, member, b.Tenant); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, target := range []ID{b.Membership, member} {
		go func(target ID) { <-start; results <- repo.SuspendMember(ctx, target, 1) }(target)
	}
	close(start)
	success := 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrDenied) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("concurrent removals success=%d", success)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM memberships WHERE tenant_id=? AND status='active'`, b.Tenant); n != 1 {
		t.Fatal("active owner invariant lost")
	}
}
func TestAuditFailureRollsBackMutation(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	repo := s.ForTenant(b.Tenant, b.Membership)
	// Corrupt only the test capability's actor after the safety check by a trigger
	// is not permitted to the app user. Instead test event transaction atomicity
	// using a deliberately invalid target actor in the shared record primitive.
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE tenants SET org_version=org_version+1 WHERE id=?`, b.Tenant); err != nil {
		t.Fatal(err)
	}
	missing, _ := NewID()
	if err = record(context.Background(), tx, b.Tenant, missing, "test.invalid", b.Tenant, 2); err == nil {
		t.Fatal("audit actor FK not enforced")
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT org_version FROM tenants WHERE id=?`, b.Tenant); n != 1 {
		t.Fatal("mutation survived audit rollback")
	}
	if _, err = repo.ListOrganizations(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestDepthLimitAndInactiveActor(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	parent := b.Root
	for depth := int64(1); depth <= 16; depth++ {
		parent = add(t, s, b, parent, "Deep", depth)
	}
	if _, err := s.ForTenant(b.Tenant, b.Membership).AddOrganization(context.Background(), parent, "Too deep", 17); !errors.Is(err, ErrInvalid) {
		t.Fatalf("depth limit: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE memberships SET status='suspended' WHERE tenant_id=? AND id=?`, b.Tenant, b.Membership); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ForTenant(b.Tenant, b.Membership).ListOrganizations(context.Background()); !errors.Is(err, ErrDenied) {
		t.Fatalf("inactive actor: %v", err)
	}
}

func TestInvitationVerifiedIdentityAndOneTimeAcceptance(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	repo := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	user := newUser(t, s)
	email := "join-" + user.String() + "+tag@example.invalid"
	if _, err := s.db.Exec(`INSERT INTO login_aliases VALUES(?,?,NULL)`, email, user); err != nil {
		t.Fatal(err)
	}
	invite, token, err := repo.CreateInvitation(ctx, "  "+email+"  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptInvitation(ctx, user, token); !errors.Is(err, ErrDenied) {
		t.Fatalf("unverified alias: %v", err)
	}
	if _, err = s.db.Exec(`UPDATE login_aliases SET verified_at=UTC_TIMESTAMP(6) WHERE user_id=?`, user); err != nil {
		t.Fatal(err)
	}
	member, err := s.AcceptInvitation(ctx, user, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptInvitation(ctx, user, token); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reused invitation: %v", err)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM role_bindings WHERE tenant_id=? AND membership_id=?`, b.Tenant, member); n != 0 {
		t.Fatal("invitation granted an implicit role")
	}
	if n := count(t, s, `SELECT COUNT(*) FROM invitations WHERE id=? AND consumed_at IS NOT NULL AND LENGTH(token_digest)=32`, invite); n != 1 {
		t.Fatal("invitation not consumed/digest invalid")
	}
	if err = s.ForTenant(b.Tenant, member).AssignOrganization(ctx, member, b.Root, 2); !errors.Is(err, ErrDenied) {
		t.Fatalf("ordinary member performed owner action: %v", err)
	}
	if err = repo.AssignOrganization(ctx, member, b.Root, 2); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM role_bindings WHERE tenant_id=? AND membership_id=?`, b.Tenant, member); n != 0 {
		t.Fatal("organization association granted role")
	}
}
func TestInvitationConcurrentAcceptance(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	ctx := context.Background()
	u := newUser(t, s)
	email := "parallel-" + u.String() + "@example.invalid"
	if _, err := s.db.Exec(`INSERT INTO login_aliases VALUES(?,?,UTC_TIMESTAMP(6))`, email, u); err != nil {
		t.Fatal(err)
	}
	_, token, err := s.ForTenant(b.Tenant, b.Membership).CreateInvitation(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() { <-start; _, err := s.AcceptInvitation(ctx, u, token); results <- err }()
	}
	close(start)
	success := 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("acceptances=%d", success)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM memberships WHERE tenant_id=? AND user_id=?`, b.Tenant, u); n != 1 {
		t.Fatal("duplicate membership")
	}
}
func TestExpiredInvitationAndCrossTenantOrganization(t *testing.T) {
	s := testStore(t)
	a := fixture(t, s)
	b := fixture(t, s)
	ctx := context.Background()
	u := newUser(t, s)
	// This identity never joins the tenant, so clean it separately.
	t.Cleanup(func() { _, _ = s.db.Exec(`DELETE FROM users WHERE id=?`, u) })
	invite, token, err := s.ForTenant(a.Tenant, a.Membership).CreateInvitation(ctx, "expired@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE invitations SET expires_at=1 WHERE id=?`, invite); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptInvitation(ctx, u, token); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired invitation: %v", err)
	}
	if err = s.ForTenant(a.Tenant, a.Membership).AssignOrganization(ctx, a.Membership, b.Root, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant association: %v", err)
	}
	if _, err = s.db.Exec(`INSERT INTO org_memberships VALUES(?,?,?)`, a.Tenant, b.Root, a.Membership); err == nil {
		t.Fatal("cross-tenant org membership FK accepted")
	}
}
