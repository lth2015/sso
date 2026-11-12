package mysql

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func accessHistory(t *testing.T, s Store, b Bootstrap, app ID) ApplicationAccessHistoryPage {
	t.Helper()
	v, e := s.ForTenant(b.Tenant, b.Membership).APIApplicationAccessHistory(context.Background(), app, 100, nil)
	if e != nil {
		t.Fatal("application history read failed")
	}
	return v
}
func TestApplicationAccessHistoryCaptureNoopAndHistoricalMember(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	app := userAccessFixture(t, s, b)
	member := rbacMember(t, s, b)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, b.Membership)
	if v := accessHistory(t, s, b, app); v.Format != ApplicationAccessHistoryFormat || v.Items == nil || len(v.Items) != 0 || v.Next != nil {
		t.Fatal("empty history invented a capture")
	}
	if _, e := repo.SetApplicationUserAccessMode(ctx, app, UserAccessSelected, 1); e != nil {
		t.Fatal("mode fixture failed")
	}
	if _, e := repo.SetApplicationUserAccessMember(ctx, app, member, true, 2); e != nil {
		t.Fatal("grant fixture failed")
	}
	if _, e := repo.SetApplicationUserAccessMember(ctx, app, member, false, 3); e != nil {
		t.Fatal("revoke fixture failed")
	}
	before := accessHistory(t, s, b, app)
	if before.Tenant != b.Tenant.String() || before.Application != app.String() || len(before.Items) != 3 {
		t.Fatal("actual changes missing or cross-linked")
	}
	for i, v := range before.Items {
		if v.Actor != b.Membership.String() || v.Mode != UserAccessSelected || v.Version != []string{"2", "3", "4"}[i] || v.At.IsZero() {
			t.Fatal("historical projection lost context")
		}
		if i == 0 && (v.Member != nil || v.Action != "application.user_access.mode_changed") {
			t.Fatal("mode history has a member")
		}
		if i > 0 && (v.Member == nil || *v.Member != member.String()) {
			t.Fatal("member history missing target")
		}
	}
	audits := count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant)
	outbox := count(t, s, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=?`, b.Tenant)
	if _, e := repo.SetApplicationUserAccessMember(ctx, app, member, false, 4); e != nil {
		t.Fatal("no-op failed")
	}
	if _, e := repo.SetApplicationUserAccessMode(ctx, app, UserAccessSelected, 4); e != nil {
		t.Fatal("mode no-op failed")
	}
	if _, e := s.db.Exec(`UPDATE memberships SET status='suspended' WHERE tenant_id=? AND id=?`, b.Tenant, member); e != nil {
		t.Fatal("historical target fixture failed")
	}
	if _, e := repo.SetApplicationEnabled(ctx, app, false, 1); e != nil {
		t.Fatal("disable fixture failed")
	}
	// Disable contributes its own original audit/outbox; reads and no-ops do not.
	if v := accessHistory(t, s, b, app); !reflect.DeepEqual(v, before) {
		t.Fatal("live member/application state rewrote history")
	}
	if count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant) != audits+1 || count(t, s, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=?`, b.Tenant) != outbox+1 {
		t.Fatal("read or no-op emitted an event")
	}
}

func TestApplicationAccessHistoryCursorNamespaceAndBounds(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	app := userAccessFixture(t, s, b)
	other := userAccessFixture(t, s, b)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, b.Membership)
	for i := int64(1); i <= 4; i++ {
		mode := UserAccessSelected
		if i%2 == 0 {
			mode = UserAccessAll
		}
		if _, e := repo.SetApplicationUserAccessMode(ctx, app, mode, i); e != nil {
			t.Fatal("pagination fixture failed")
		}
	}
	first, e := repo.APIApplicationAccessHistory(ctx, app, 2, nil)
	if e != nil || len(first.Items) != 2 || first.Next == nil {
		t.Fatal("first page unbounded")
	}
	cursor, _ := ParseID(*first.Next)
	next, e := repo.APIApplicationAccessHistory(ctx, app, 2, &cursor)
	if e != nil || len(next.Items) != 2 || next.Next != nil || next.Items[0].ID <= *first.Next {
		t.Fatal("cursor failed to advance")
	}
	if _, e = repo.APIApplicationAccessHistory(ctx, other, 2, &cursor); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign app cursor accepted")
	}
	zero := ID{}
	if _, e = repo.APIApplicationAccessHistory(ctx, app, 2, &zero); !errors.Is(e, ErrNotFound) {
		t.Fatal("explicit zero cursor treated as omission")
	}
	for _, limit := range []int{0, 101} {
		if _, e = repo.APIApplicationAccessHistory(ctx, app, limit, nil); !errors.Is(e, ErrInvalid) {
			t.Fatal("unbounded history allowed")
		}
	}
}

func TestApplicationAccessHistoryOwnerAndApplicationBoundaries(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	other := fixture(t, s)
	app := userAccessFixture(t, s, b)
	member := rbacMember(t, s, b)
	ctx := context.Background()
	role := rbacRole(t, s, b, "access_history_auditor", []RolePolicy{{"audit.read", "allow"}})
	rbacBind(t, s, b, member, role, false)
	if _, e := s.ForTenant(b.Tenant, member).APIApplicationAccessHistory(ctx, app, 20, nil); !errors.Is(e, ErrDenied) {
		t.Fatal("audit delegate read membership history")
	}
	if _, e := s.ForTenant(other.Tenant, other.Membership).APIApplicationAccessHistory(ctx, app, 20, nil); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign app history exposed")
	}
	shared := applicationFixture(t, s, b.Tenant)
	if _, e := s.ForTenant(b.Tenant, b.Membership).APIApplicationAccessHistory(ctx, shared, 20, nil); !errors.Is(e, ErrApplicationUnmanaged) {
		t.Fatal("shared application history exposed")
	}
}

func TestApplicationAccessHistoryCorruptRowsNeverReturnPartialData(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	app := userAccessFixture(t, s, b)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, b.Membership)
	if _, e := repo.SetApplicationUserAccessMode(ctx, app, UserAccessSelected, 1); e != nil {
		t.Fatal("history fixture failed")
	}
	if _, e := repo.SetApplicationUserAccessMode(ctx, app, UserAccessAll, 2); e != nil {
		t.Fatal("history fixture failed")
	}
	v := accessHistory(t, s, b, app)
	id, _ := ParseID(v.Items[1].ID)
	for _, query := range []string{
		`UPDATE application_user_access_changes SET membership_id=? WHERE event_id=?`,
		`UPDATE audit_events SET target_id=? WHERE event_id=?`,
	} {
		if _, e := s.db.Exec(query, b.Membership, id); e != nil {
			t.Fatal("corrupt-row fixture failed")
		}
		page, e := repo.APIApplicationAccessHistory(ctx, app, 100, nil)
		if !errors.Is(e, ErrApplicationAccessHistoryUnavailable) || page.Items != nil || page.Tenant != "" {
			t.Fatal("corrupt page returned partial history")
		}
		if _, e := s.db.Exec(`UPDATE application_user_access_changes SET membership_id=NULL WHERE event_id=?`, id); e != nil {
			t.Fatal("fixture restoration failed")
		}
		if _, e := s.db.Exec(`UPDATE audit_events SET target_id=? WHERE event_id=?`, app, id); e != nil {
			t.Fatal("fixture restoration failed")
		}
	}
	if _, e := s.db.Exec(`UPDATE audit_events SET action='private.unreviewed' WHERE event_id=?`, id); e != nil {
		t.Fatal("unknown-action fixture failed")
	}
	if page, e := repo.APIApplicationAccessHistory(ctx, app, 100, nil); !errors.Is(e, ErrApplicationAccessHistoryUnavailable) || page.Items != nil {
		t.Fatal("unknown action exposed")
	}
}
