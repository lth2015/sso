package mysql

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOrganizationRenameNoopRootAndTenantBounds(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	other := fixture(t, s)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, b.Membership)
	org := add(t, s, b, b.Root, "Before", 1)
	if _, e := repo.RenameOrganization(ctx, org, "  新名称  ", 2); e != nil {
		t.Fatal(e)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM org_nodes WHERE tenant_id=? AND id=? AND name='新名称'`, b.Tenant, org); n != 1 {
		t.Fatal("trimmed rename missing")
	}
	if v, e := repo.RenameOrganization(ctx, org, "新名称", 3); e != nil || v != 3 {
		t.Fatal("rename noop changed version")
	}
	if count(t, s, `SELECT authz_version FROM tenants WHERE id=?`, b.Tenant) != 1 {
		t.Fatal("display rename changed grants")
	}
	if count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=? AND action='org.renamed'`, b.Tenant) != 1 {
		t.Fatal("rename audit not exactly once")
	}
	for _, name := range []string{"  ", strings.Repeat("中", 43), string([]byte{0xff})} {
		if _, e := repo.RenameOrganization(ctx, org, name, 3); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid name accepted")
		}
	}
	if _, e := repo.RenameOrganization(ctx, org, "Stale", 2); !errors.Is(e, ErrConflict) {
		t.Fatal("stale rename accepted")
	}
	if _, e := repo.RenameOrganization(ctx, b.Root, "Space rename", 3); !errors.Is(e, ErrProtectedOrganization) {
		t.Fatal("delegated space rename allowed")
	}
	if _, e := repo.RenameOrganization(ctx, other.Root, "Foreign", 3); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-tenant rename accepted")
	}
	if count(t, s, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND type='org.renamed'`, b.Tenant) != 1 {
		t.Fatal("rename outbox mismatch")
	}
}
func TestOrganizationEmptyLeafDeletionAndInactiveMemberRemoval(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, b.Membership)
	parent := add(t, s, b, b.Root, "Parent", 1)
	child := add(t, s, b, parent, "Child", 2)
	if _, e := repo.DeleteOrganization(ctx, b.Root, 3); !errors.Is(e, ErrProtectedOrganization) {
		t.Fatal("root deleted")
	}
	if _, e := repo.DeleteOrganization(ctx, parent, 3); !errors.Is(e, ErrOrganizationNotEmpty) {
		t.Fatal("parent cascade allowed")
	}
	if e := repo.AssignOrganization(ctx, b.Membership, child, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := repo.DeleteOrganization(ctx, child, 3); !errors.Is(e, ErrOrganizationNotEmpty) {
		t.Fatal("assigned node deleted")
	}
	if e := repo.UnassignOrganization(ctx, b.Membership, child, 1); !errors.Is(e, ErrConflict) {
		t.Fatal("stale membership removal accepted")
	}
	if e := repo.UnassignOrganization(ctx, b.Membership, child, 2); e != nil {
		t.Fatal(e)
	}
	if e := repo.UnassignOrganization(ctx, b.Membership, child, 3); e != nil {
		t.Fatal("association noop failed")
	}
	if count(t, s, `SELECT authz_version FROM tenants WHERE id=?`, b.Tenant) != 3 {
		t.Fatal("association noop changed authz")
	}
	if count(t, s, `SELECT COUNT(*) FROM role_bindings WHERE tenant_id=? AND membership_id=?`, b.Tenant, b.Membership) != 1 {
		t.Fatal("removing organization removed owner")
	}
	user := newUser(t, s)
	member, _ := NewID()
	if _, e := s.db.Exec(`INSERT INTO memberships(id,tenant_id,user_id,status) VALUES(?,?,?,'suspended')`, member, b.Tenant, user); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec(`INSERT INTO org_memberships VALUES(?,?,?)`, b.Tenant, child, member); e != nil {
		t.Fatal(e)
	}
	members, _, e := repo.APIOrganizationMembers(ctx, child, 2, ID{})
	if e != nil || len(members) != 1 || members[0].Status != "suspended" {
		t.Fatal("inactive references hidden")
	}
	if _, e := repo.DeleteOrganization(ctx, child, 3); !errors.Is(e, ErrOrganizationNotEmpty) {
		t.Fatal("inactive reference ignored")
	}
	if e := repo.UnassignOrganization(ctx, member, child, 3); e != nil {
		t.Fatal("inactive association not removable")
	}
	if v, e := repo.DeleteOrganization(ctx, child, 3); e != nil || v != 4 {
		t.Fatal("empty leaf deletion failed")
	}
	if count(t, s, `SELECT COUNT(*) FROM org_closure WHERE tenant_id=? AND (ancestor_id=? OR descendant_id=?)`, b.Tenant, child, child) != 0 {
		t.Fatal("deleted leaf closure retained")
	}
	if count(t, s, `SELECT COUNT(*) FROM org_closure WHERE tenant_id=?`, b.Tenant) != 3 {
		t.Fatal("unrelated closure changed")
	}
	if _, e := repo.DeleteOrganization(ctx, child, 4); !errors.Is(e, ErrNotFound) {
		t.Fatal("deleted node still visible")
	}
	if _, e := repo.DeleteOrganization(ctx, parent, 4); e != nil {
		t.Fatal("now-empty parent not deletable")
	}
	if count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=? AND action='org.deleted'`, b.Tenant) != 2 || count(t, s, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND type='org.deleted'`, b.Tenant) != 2 {
		t.Fatal("deletion audit/outbox mismatch")
	}
}
func TestOrganizationMembersBothPermissionsPaginationAndIsolation(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	other := fixture(t, s)
	ctx := context.Background()
	owner := s.ForTenant(b.Tenant, b.Membership)
	org := add(t, s, b, b.Root, "Team", 1)
	if e := owner.AssignOrganization(ctx, b.Membership, org, 1); e != nil {
		t.Fatal(e)
	}
	user := newUser(t, s)
	member, _ := NewID()
	role, _ := NewID()
	for _, st := range []struct {
		q string
		a []any
	}{
		{`INSERT INTO memberships(id,tenant_id,user_id) VALUES(?,?,?)`, []any{member, b.Tenant, user}},
		{`INSERT INTO org_memberships VALUES(?,?,?)`, []any{b.Tenant, org, member}},
		{`INSERT INTO roles(id,tenant_id,code) VALUES(?,?,'org_operator')`, []any{role, b.Tenant}},
		{`INSERT INTO role_bindings VALUES(?,?,?)`, []any{b.Tenant, member, role}},
		{`INSERT INTO role_policies VALUES(?,?,'organizations.read','allow')`, []any{b.Tenant, role}},
	} {
		if _, e := s.db.Exec(st.q, st.a...); e != nil {
			t.Fatal(e)
		}
	}
	delegated := s.ForTenant(b.Tenant, member)
	if _, _, e := delegated.APIOrganizationMembers(ctx, org, 2, ID{}); !errors.Is(e, ErrDenied) {
		t.Fatal("organization read disclosed member data")
	}
	if _, e := s.db.Exec(`INSERT INTO role_policies VALUES(?,?,'members.read','allow')`, b.Tenant, role); e != nil {
		t.Fatal(e)
	}
	page, version, e := delegated.APIOrganizationMembers(ctx, org, 1, ID{})
	if e != nil || len(page) != 1 || version != 2 {
		t.Fatal("direct member first page failed")
	}
	after, e := ParseID(page[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	next, _, e := delegated.APIOrganizationMembers(ctx, org, 2, after)
	if e != nil || len(next) != 1 || next[0].ID <= page[0].ID {
		t.Fatal("direct member cursor wrong")
	}
	if _, e := delegated.RenameOrganization(ctx, org, "Denied", 2); !errors.Is(e, ErrDenied) {
		t.Fatal("read delegated write")
	}
	if _, _, e := owner.APIOrganizationMembers(ctx, other.Root, 2, ID{}); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign organization disclosed")
	}
	if e := owner.UnassignOrganization(ctx, other.Membership, org, 2); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign member accepted")
	}
	if _, e := s.db.Exec(`INSERT INTO role_policies VALUES(?,?,'members.read','deny')`, b.Tenant, role); e != nil {
		t.Fatal(e)
	}
	if _, _, e := delegated.APIOrganizationMembers(ctx, org, 2, ID{}); !errors.Is(e, ErrDenied) {
		t.Fatal("deny did not override allow")
	}
}
func TestOrganizationDeleteAndConcurrentCreateSerialize(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	repo := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	org := add(t, s, b, b.Root, "Race", 1)
	start := make(chan struct{})
	result := make(chan error, 2)
	go func() { <-start; _, e := repo.DeleteOrganization(ctx, org, 2); result <- e }()
	go func() { <-start; _, e := repo.AddOrganization(ctx, org, "New child", 2); result <- e }()
	close(start)
	success, conflict := 0, 0
	for range 2 {
		e := <-result
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflict++
		} else {
			t.Fatal("concurrent directory operation failed")
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent mutation not serialized")
	}
	if count(t, s, `SELECT org_version FROM tenants WHERE id=?`, b.Tenant) != 3 {
		t.Fatal("race changed version twice")
	}
}
