package mysql

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func rbacMember(t *testing.T, s Store, b Bootstrap) ID {
	t.Helper()
	u := newUser(t, s)
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO memberships(id,tenant_id,user_id) VALUES(?,?,?)`, id, b.Tenant, u); err != nil {
		t.Fatal(err)
	}
	return id
}
func rbacVersion(t *testing.T, s Store, b Bootstrap) int64 {
	t.Helper()
	var n int64
	if err := s.db.QueryRow(`SELECT authz_version FROM tenants WHERE id=?`, b.Tenant).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func rbacRole(t *testing.T, s Store, b Bootstrap, code string, policies []RolePolicy) ID {
	t.Helper()
	id, err := s.ForTenant(b.Tenant, b.Membership).APICreateRole(context.Background(), code, policies, rbacVersion(t, s, b))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func rbacBind(t *testing.T, s Store, b Bootstrap, member, role ID, remove bool) {
	t.Helper()
	if err := s.ForTenant(b.Tenant, b.Membership).APIBindRole(context.Background(), member, role, rbacVersion(t, s, b), remove); err != nil {
		t.Fatal(err)
	}
}
func TestRBACDenyOverridesAllowAndRevocation(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	guest := rbacMember(t, s, b)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, guest)
	if _, _, err := repo.APIMembers(ctx, 20, ID{}); !errors.Is(err, ErrDenied) {
		t.Fatal("unbound member could read directory")
	}
	allow := rbacRole(t, s, b, "directory", []RolePolicy{{"members.read", "allow"}})
	deny := rbacRole(t, s, b, "blocked", []RolePolicy{{"members.read", "deny"}})
	rbacBind(t, s, b, guest, allow, false)
	if rows, _, err := repo.APIMembers(ctx, 20, ID{}); err != nil || len(rows) != 2 {
		t.Fatal("delegated read failed")
	}
	if err := repo.SuspendMember(ctx, b.Membership, rbacVersion(t, s, b)); !errors.Is(err, ErrDenied) {
		t.Fatal("read permission allowed mutation")
	}
	rbacBind(t, s, b, guest, deny, false)
	if _, _, err := repo.APIMembers(ctx, 20, ID{}); !errors.Is(err, ErrDenied) {
		t.Fatal("allow bypassed explicit deny")
	}
	rbacBind(t, s, b, guest, deny, true)
	if _, _, err := repo.APIMembers(ctx, 20, ID{}); err != nil {
		t.Fatal("removing deny did not restore allow")
	}
	rbacBind(t, s, b, guest, allow, true)
	if _, _, err := repo.APIMembers(ctx, 20, ID{}); !errors.Is(err, ErrDenied) {
		t.Fatal("removed grant remained effective")
	}
}
func TestRBACInvitationRequiresCurrentDelegatedPermission(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	creator := rbacMember(t, s, b)
	user := newUser(t, s)
	email := user.String() + "@example.invalid"
	if _, err := s.db.Exec(`INSERT INTO login_aliases VALUES(?,?,UTC_TIMESTAMP(6))`, email, user); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, creator)
	role := rbacRole(t, s, b, "invite_operator", []RolePolicy{{"invitations.manage", "allow"}})
	rbacBind(t, s, b, creator, role, false)
	_, token, err := repo.CreateInvitation(ctx, email)
	if err != nil {
		t.Fatal("delegated invitation failed")
	}
	rbacBind(t, s, b, creator, role, true)
	if _, err = s.AcceptInvitation(ctx, user, token); !errors.Is(err, ErrDenied) {
		t.Fatal("invitation accepted after creator authority was removed")
	}
	rbacBind(t, s, b, creator, role, false)
	member, err := s.AcceptInvitation(ctx, user, token)
	if err != nil {
		t.Fatal("denied acceptance consumed the invitation")
	}
	if _, _, err = s.ForTenant(b.Tenant, member).APIMembers(ctx, 20, ID{}); !errors.Is(err, ErrDenied) {
		t.Fatal("invitation implicitly granted directory access")
	}
}
func TestRBACDelegationCannotManageRolesOrOwners(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	guest := rbacMember(t, s, b)
	target := rbacMember(t, s, b)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, guest)
	role := rbacRole(t, s, b, "member_operator", []RolePolicy{{"members.read", "allow"}, {"members.write", "allow"}})
	rbacBind(t, s, b, guest, role, false)
	if _, _, err := repo.APIRoles(ctx, 20, ID{}); !errors.Is(err, ErrDenied) {
		t.Fatal("delegated operator could inspect administrative roles")
	}
	if _, err := repo.APICreateRole(ctx, "escalate", []RolePolicy{{"audit.read", "allow"}}, rbacVersion(t, s, b)); !errors.Is(err, ErrDenied) {
		t.Fatal("delegated operator created a role")
	}
	if err := repo.SuspendMember(ctx, b.Membership, rbacVersion(t, s, b)); !errors.Is(err, ErrDenied) {
		t.Fatal("delegated operator suspended reserved owner")
	}
	if err := repo.SuspendMember(ctx, target, rbacVersion(t, s, b)); err != nil {
		t.Fatal("ordinary member could not be suspended")
	}
	if err := repo.APIRestoreMember(ctx, target, rbacVersion(t, s, b)); err != nil {
		t.Fatal("ordinary member could not be restored")
	}
}
func TestRBACReservedRoleCrossTenantAndValidationAtomicity(t *testing.T) {
	s := testStore(t)
	a, b := fixture(t, s), fixture(t, s)
	ctx := context.Background()
	repo := s.ForTenant(a.Tenant, a.Membership)
	var owner ID
	if err := s.db.QueryRow(`SELECT id FROM roles WHERE tenant_id=? AND code='owner'`, a.Tenant).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	v := rbacVersion(t, s, a)
	if err := repo.APIReplaceRole(ctx, owner, []RolePolicy{}, v); !errors.Is(err, ErrReservedRole) {
		t.Fatal("reserved owner was edited")
	}
	if err := repo.APIDeleteRole(ctx, owner, v); !errors.Is(err, ErrReservedRole) {
		t.Fatal("reserved owner was deleted")
	}
	for _, policies := range [][]RolePolicy{{{"roles.manage", "allow"}}, {{"audit.read", "allow"}, {"audit.read", "allow"}}, {{"*", "allow"}}} {
		if _, err := repo.APICreateRole(ctx, "invalid", policies, v); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid administrative rule accepted")
		}
	}
	if rbacVersion(t, s, a) != v {
		t.Fatal("rejected mutation changed authority version")
	}
	foreign := rbacRole(t, s, b, "foreign", []RolePolicy{{"audit.read", "allow"}})
	if err := repo.APIBindRole(ctx, a.Membership, foreign, v, false); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-tenant role bound")
	}
	if err := repo.APIReplaceRole(ctx, foreign, []RolePolicy{}, v); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-tenant role changed")
	}
}
func TestRBACOwnerDenyRecoveryAndHistoricalAudit(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, b.Membership)
	role := rbacRole(t, s, b, "deny_audit", []RolePolicy{{"audit.read", "deny"}})
	rbacBind(t, s, b, b.Membership, role, false)
	if _, err := repo.APIAudit(ctx, 20, ID{}); !errors.Is(err, ErrDenied) {
		t.Fatal("owner bypassed operational deny")
	}
	if _, _, err := repo.APIRoles(ctx, 20, ID{}); err != nil {
		t.Fatal("reserved recovery administration lost")
	}
	if err := repo.APIDeleteRole(ctx, role, rbacVersion(t, s, b)); !errors.Is(err, ErrRoleInUse) {
		t.Fatal("bound role deleted")
	}
	rbacBind(t, s, b, b.Membership, role, true)
	if err := repo.APIDeleteRole(ctx, role, rbacVersion(t, s, b)); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.APIAudit(ctx, 100, ID{})
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, r := range rows {
		if (r.Action == "role.bound" || r.Action == "role.unbound") && r.RoleCode != nil && *r.RoleCode == "deny_audit" && r.Membership != nil && *r.Membership == b.Membership.String() {
			found++
		}
	}
	if found != 2 {
		t.Fatal("binding audit metadata did not survive role deletion")
	}
}
func TestRBACConcurrentOwnerRemovalPreservesLastOwner(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	other := rbacMember(t, s, b)
	ctx := context.Background()
	var owner ID
	if err := s.db.QueryRow(`SELECT id FROM roles WHERE tenant_id=? AND code='owner'`, b.Tenant).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	rbacBind(t, s, b, other, owner, false)
	version := rbacVersion(t, s, b)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, actor := range []ID{b.Membership, other} {
		wg.Add(1)
		go func(actor ID) {
			defer wg.Done()
			<-start
			results <- s.ForTenant(b.Tenant, actor).APIBindRole(ctx, actor, owner, version, true)
		}(actor)
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
		t.Fatal("concurrent owner changes were not serialized")
	}
	var remaining ID
	if err := s.db.QueryRow(`SELECT membership_id FROM role_bindings WHERE tenant_id=? AND role_id=?`, b.Tenant, owner).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if err := s.ForTenant(b.Tenant, remaining).APIBindRole(ctx, remaining, owner, rbacVersion(t, s, b), true); !errors.Is(err, ErrLastOwner) {
		t.Fatal("last owner removed")
	}
}
