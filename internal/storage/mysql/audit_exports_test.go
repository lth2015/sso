package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAuditExportReviewedKindsPaginationAndCursorIsolation(t *testing.T) {
	s, b, repo, app, principal, admin, _ := machineFixture(t)
	ctx := context.Background()
	credential, e := repo.CreateMachineCredential(ctx, app, principal, "Export fixture", 30, admin)
	if e != nil {
		t.Fatal("export credential fixture failed")
	}
	if _, e = s.CheckMachineToken(ctx, machineCheck(credential.Credential.AccessKey, app)); e != nil {
		t.Fatal("machine audit fixture failed")
	}
	_, invitationSecret, e := repo.CreateInvitation(ctx, "hidden-export-target@example.invalid")
	if e != nil {
		t.Fatal("private invitation fixture failed")
	}
	first, e := repo.APIExportManagementAudit(ctx, 1, ID{})
	if e != nil || first.Format != AuditBatchFormat || first.Kind != "management" || first.Tenant != b.Tenant.String() || len(first.Items) != 1 || first.NextCursor == nil || first.Version < 1 || first.GeneratedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatal("management batch envelope or bounds failed")
	}
	seen := map[string]bool{}
	cursor := ID{}
	for range 100 {
		batch, e := repo.APIExportManagementAudit(ctx, 1, cursor)
		if e != nil || len(batch.Items) > 1 {
			t.Fatal("management export page failed")
		}
		for _, item := range batch.Items {
			if seen[item.ID] {
				t.Fatal("management keyset repeated an event")
			}
			seen[item.ID] = true
		}
		raw, _ := json.Marshal(batch)
		if strings.Contains(string(raw), invitationSecret) || strings.Contains(string(raw), credential.Secret) || strings.Contains(string(raw), "hidden-export-target") {
			t.Fatal("private data entered audit export")
		}
		if batch.NextCursor == nil {
			break
		}
		cursor, _ = ParseID(*batch.NextCursor)
	}
	if len(seen) != count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant) {
		t.Fatal("bounded pages did not cover this tenant management events")
	}
	machine, e := repo.APIExportMachineAudit(ctx, 1, ID{})
	if e != nil || machine.Kind != "machine" || len(machine.Items) != 1 || machine.Items[0].Principal != principal.String() || machine.NextCursor != nil {
		t.Fatal("machine export namespace failed")
	}
	raw, _ := json.Marshal(machine)
	if strings.Contains(string(raw), "actor_membership_id") || strings.Contains(string(raw), credential.Secret) {
		t.Fatal("machine export forged a human actor or exposed SK")
	}
	managementID, _ := ParseID(first.Items[0].ID)
	if _, e = repo.APIExportMachineAudit(ctx, 1, managementID); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-kind export cursor accepted")
	}
	machineID, _ := ParseID(machine.Items[0].ID)
	if _, e = repo.APIExportManagementAudit(ctx, 1, machineID); !errors.Is(e, ErrNotFound) {
		t.Fatal("machine cursor accepted for management")
	}
	foreign := fixture(t, s)
	other, e := s.ForTenant(foreign.Tenant, foreign.Membership).APIExportManagementAudit(ctx, 1, ID{})
	if e != nil || len(other.Items) != 1 {
		t.Fatal("foreign audit fixture failed")
	}
	otherID, _ := ParseID(other.Items[0].ID)
	if _, e = repo.APIExportManagementAudit(ctx, 1, otherID); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign tenant export cursor accepted")
	}
	unknown, _ := NewID()
	if _, e = repo.APIExportManagementAudit(ctx, 1, unknown); !errors.Is(e, ErrNotFound) {
		t.Fatal("unknown export cursor accepted")
	}
}
func TestAuditExportDelegatedPermissionAndLiveRevocation(t *testing.T) {
	s, b, _, app, principal, _, _ := machineFixture(t)
	ctx := context.Background()
	member := rbacMember(t, s, b)
	delegate := s.ForTenant(b.Tenant, member)
	if _, e := delegate.APIExportManagementAudit(ctx, 50, ID{}); !errors.Is(e, ErrDenied) {
		t.Fatal("ordinary member exported audit")
	}
	role := rbacRole(t, s, b, "export_auditor", []RolePolicy{{Permission: "audit.read", Effect: "allow"}})
	rbacBind(t, s, b, member, role, false)
	if _, e := delegate.APIExportManagementAudit(ctx, 50, ID{}); e != nil {
		t.Fatal("delegated management export denied")
	}
	if batch, e := delegate.APIExportMachineAudit(ctx, 50, ID{}); e != nil || len(batch.Items) != 0 || batch.Items == nil {
		t.Fatal("empty machine audit export failed")
	}
	if _, e := delegate.APIMachineCredentials(ctx, app, principal, 50, ID{}); !errors.Is(e, ErrDenied) {
		t.Fatal("export permission granted credential management")
	}
	denied := rbacRole(t, s, b, "export_denied", []RolePolicy{{Permission: "audit.read", Effect: "deny"}})
	rbacBind(t, s, b, member, denied, false)
	if _, e := delegate.APIExportManagementAudit(ctx, 50, ID{}); !errors.Is(e, ErrDenied) {
		t.Fatal("export ignored a live explicit deny")
	}
	if _, e := delegate.APIExportMachineAudit(ctx, 50, ID{}); !errors.Is(e, ErrDenied) {
		t.Fatal("machine export ignored live permission revocation")
	}
}
func TestAuditExportDoesNotChangeAuthorityOrAuditAndRejectsUnboundedLimit(t *testing.T) {
	s, b, repo, _, _, _, _ := machineFixture(t)
	ctx := context.Background()
	version := rbacVersion(t, s, b)
	before := count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant)
	outbox := count(t, s, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=?`, b.Tenant)
	for _, limit := range []int{0, 101, -1} {
		if _, e := repo.APIExportManagementAudit(ctx, limit, ID{}); !errors.Is(e, ErrInvalid) {
			t.Fatal("management export limit escaped")
		}
		if _, e := repo.APIExportMachineAudit(ctx, limit, ID{}); !errors.Is(e, ErrInvalid) {
			t.Fatal("machine export limit escaped")
		}
	}
	if _, e := repo.APIExportManagementAudit(ctx, 100, ID{}); e != nil {
		t.Fatal("bounded read failed")
	}
	if _, e := repo.APIExportMachineAudit(ctx, 100, ID{}); e != nil {
		t.Fatal("bounded machine read failed")
	}
	if rbacVersion(t, s, b) != version || count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant) != before || count(t, s, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=?`, b.Tenant) != outbox {
		t.Fatal("audit GET export changed authority or created mutation events")
	}
}
