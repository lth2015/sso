package mysql

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"encoding/json"
	"errors"
	driver "github.com/go-sql-driver/mysql"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func policyHistory(t *testing.T, s Store, b Bootstrap, role ID) RolePolicyHistoryPage {
	t.Helper()
	page, e := s.ForTenant(b.Tenant, b.Membership).APIRolePolicyHistory(context.Background(), role, 100, nil)
	if e != nil {
		t.Fatal("role history read failed")
	}
	return page
}
func TestRolePolicyHistoryCaptureNoopDeleteAndCodeReuse(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	repo := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	before := []RolePolicy{{"members.read", "allow"}, {"audit.read", "allow"}}
	role := rbacRole(t, s, b, "tracked_policy", before)
	first := policyHistory(t, s, b, role)
	canonical, _ := canonicalRolePolicies(before)
	if first.Format != RolePolicyHistoryFormat || first.Tenant != b.Tenant.String() || first.Role != role.String() || len(first.Items) != 1 || first.NextCursor != nil || len(first.Items[0].Before) != 0 || !reflect.DeepEqual(first.Items[0].After, canonical) || first.Items[0].Actor != b.Membership.String() {
		t.Fatal("creation snapshot namespace or rules invalid")
	}
	version := rbacVersion(t, s, b)
	events := count(t, s, "SELECT COUNT(*) FROM audit_events WHERE tenant_id=?", b.Tenant)
	if e := repo.APIReplaceRole(ctx, role, []RolePolicy{before[1], before[0]}, version); e != nil {
		t.Fatal("canonical no-op rejected")
	}
	if rbacVersion(t, s, b) != version || len(policyHistory(t, s, b, role).Items) != 1 || count(t, s, "SELECT COUNT(*) FROM audit_events WHERE tenant_id=?", b.Tenant) != events {
		t.Fatal("no-op created a snapshot or event")
	}
	after := []RolePolicy{{"members.read", "deny"}}
	if e := repo.APIReplaceRole(ctx, role, after, version); e != nil {
		t.Fatal("policy replacement failed")
	}
	changed := policyHistory(t, s, b, role)
	var update RolePolicyHistoryEntry
	for _, v := range changed.Items {
		if v.Action == "role.updated" {
			update = v
		}
	}
	if len(changed.Items) != 2 || !reflect.DeepEqual(update.Before, canonical) || !reflect.DeepEqual(update.After, after) || update.Version != version+1 {
		t.Fatal("replacement snapshot lost original policies")
	}
	if e := repo.APIDeleteRole(ctx, role, rbacVersion(t, s, b)); e != nil {
		t.Fatal("tracked role deletion failed")
	}
	deleted := policyHistory(t, s, b, role)
	var removal RolePolicyHistoryEntry
	for _, v := range deleted.Items {
		if v.Action == "role.deleted" {
			removal = v
		}
	}
	if len(deleted.Items) != 3 || len(removal.After) != 0 || !reflect.DeepEqual(removal.Before, after) || removal.Code != "tracked_policy" {
		t.Fatal("deletion rewrote historical policies")
	}
	reused := rbacRole(t, s, b, "tracked_policy", nil)
	if reused == role || len(policyHistory(t, s, b, reused).Items) != 1 || len(policyHistory(t, s, b, role).Items) != 3 {
		t.Fatal("code reuse merged different role histories")
	}
	if count(t, s, `SELECT COUNT(*) FROM role_policy_changes h JOIN audit_events a ON a.event_id=h.event_id JOIN outbox_events o ON o.event_id=a.event_id WHERE h.tenant_id=? AND a.tenant_id=h.tenant_id AND a.target_id=h.role_id AND o.aggregate_id=h.role_id AND o.aggregate_version=h.authz_version AND o.type=a.action`, b.Tenant) != 4 {
		t.Fatal("snapshot/audit/outbox/version graph diverged")
	}
}
func TestRolePolicyHistoryOwnerAndCursorIsolation(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, b.Membership)
	role := rbacRole(t, s, b, "history_cursor", []RolePolicy{{"audit.read", "allow"}})
	if e := repo.APIReplaceRole(ctx, role, nil, rbacVersion(t, s, b)); e != nil {
		t.Fatal("cursor update fixture failed")
	}
	first, e := repo.APIRolePolicyHistory(ctx, role, 1, nil)
	if e != nil || len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatal("history bounded page invalid")
	}
	cursor, _ := ParseID(*first.NextCursor)
	second, e := repo.APIRolePolicyHistory(ctx, role, 1, &cursor)
	if e != nil || len(second.Items) != 1 || second.NextCursor != nil || second.Items[0].ID <= first.Items[0].ID {
		t.Fatal("history cursor did not advance")
	}
	otherRole := rbacRole(t, s, b, "other_history", nil)
	otherEvent, _ := ParseID(policyHistory(t, s, b, otherRole).Items[0].ID)
	foreign := fixture(t, s)
	foreignRole := rbacRole(t, s, foreign, "foreign_history", nil)
	foreignEvent, _ := ParseID(policyHistory(t, s, foreign, foreignRole).Items[0].ID)
	zero := ID{}
	unknown, _ := NewID()
	for _, bad := range []ID{zero, unknown, otherEvent, foreignEvent} {
		if _, e := repo.APIRolePolicyHistory(ctx, role, 1, &bad); !errors.Is(e, ErrNotFound) {
			t.Fatal("foreign/unknown role cursor accepted")
		}
	}
	if _, e := repo.APIRolePolicyHistory(ctx, foreignRole, 1, nil); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign role history disclosed")
	}
	if _, e := repo.APIRolePolicyHistory(ctx, unknown, 1, nil); !errors.Is(e, ErrNotFound) {
		t.Fatal("unknown role appeared valid")
	}
	member := rbacMember(t, s, b)
	auditRole := rbacRole(t, s, b, "history_auditor", []RolePolicy{{"audit.read", "allow"}})
	rbacBind(t, s, b, member, auditRole, false)
	if _, e := s.ForTenant(b.Tenant, member).APIRolePolicyHistory(ctx, role, 1, nil); !errors.Is(e, ErrDenied) {
		t.Fatal("audit delegation exposed owner policy rules")
	}
	// Read metadata neither mutates events nor needs directory access.
	deny := rbacRole(t, s, b, "history_owner_deny", []RolePolicy{{"members.read", "deny"}, {"audit.read", "deny"}})
	rbacBind(t, s, b, b.Membership, deny, false)
	events := count(t, s, "SELECT COUNT(*) FROM audit_events WHERE tenant_id=?", b.Tenant)
	version := rbacVersion(t, s, b)
	if _, e := repo.APIRolePolicyHistory(ctx, role, 1, nil); e != nil {
		t.Fatal("directory denial removed reserved owner history read")
	}
	if count(t, s, "SELECT COUNT(*) FROM audit_events WHERE tenant_id=?", b.Tenant) != events || rbacVersion(t, s, b) != version {
		t.Fatal("history read mutated state")
	}
	for _, limit := range []int{0, 101} {
		if _, e := repo.APIRolePolicyHistory(ctx, role, limit, nil); !errors.Is(e, ErrInvalid) {
			t.Fatal("unbounded history page allowed")
		}
	}
}

var errSnapshotStorageFixture = errors.New("injected snapshot storage failure")

type historyFailureConnector struct{ sqldriver.Connector }

func (c historyFailureConnector) Connect(ctx context.Context) (sqldriver.Conn, error) {
	base, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &historyFailureConn{base}, nil
}

type historyFailureConn struct{ sqldriver.Conn }

func (c *historyFailureConn) BeginTx(ctx context.Context, opts sqldriver.TxOptions) (sqldriver.Tx, error) {
	return c.Conn.(sqldriver.ConnBeginTx).BeginTx(ctx, opts)
}
func (c *historyFailureConn) ExecContext(ctx context.Context, query string, args []sqldriver.NamedValue) (sqldriver.Result, error) {
	if strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO role_policy_changes") {
		return nil, errSnapshotStorageFixture
	}
	return c.Conn.(sqldriver.ExecerContext).ExecContext(ctx, query, args)
}
func (c *historyFailureConn) QueryContext(ctx context.Context, query string, args []sqldriver.NamedValue) (sqldriver.Rows, error) {
	return c.Conn.(sqldriver.QueryerContext).QueryContext(ctx, query, args)
}
func TestRolePolicyHistorySnapshotFailureRollsBackAllWrites(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	role := rbacRole(t, s, b, "atomic_history", []RolePolicy{{"audit.read", "allow"}})
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
	connector, e := driver.NewConnector(cfg)
	if e != nil {
		t.Fatal("snapshot failure fixture connector unavailable")
	}
	db := sql.OpenDB(historyFailureConnector{connector})
	defer db.Close()
	db.SetMaxOpenConns(1)
	failing := Store{db}.ForTenant(b.Tenant, b.Membership)
	version := rbacVersion(t, s, b)
	events := count(t, s, "SELECT COUNT(*) FROM audit_events WHERE tenant_id=?", b.Tenant)
	outbox := count(t, s, "SELECT COUNT(*) FROM outbox_events WHERE tenant_id=?", b.Tenant)
	if e = failing.APIReplaceRole(context.Background(), role, []RolePolicy{{"audit.read", "deny"}}, version); !errors.Is(e, errSnapshotStorageFixture) {
		t.Fatal("snapshot storage fault did not abort mutation")
	}
	page := policyHistory(t, s, b, role)
	if rbacVersion(t, s, b) != version || len(page.Items) != 1 || count(t, s, "SELECT COUNT(*) FROM audit_events WHERE tenant_id=?", b.Tenant) != events || count(t, s, "SELECT COUNT(*) FROM outbox_events WHERE tenant_id=?", b.Tenant) != outbox {
		t.Fatal("snapshot failure partially committed audit/version")
	}
	var effect string
	if e = s.db.QueryRow(`SELECT effect FROM role_policies WHERE tenant_id=? AND role_id=? AND permission='audit.read'`, b.Tenant, role).Scan(&effect); e != nil || effect != "allow" {
		t.Fatal("failed snapshot left changed role policies")
	}
}
func TestRolePolicyHistoryConcurrentReplacementOneSnapshot(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	role := rbacRole(t, s, b, "concurrent_history", nil)
	version := rbacVersion(t, s, b)
	repo := s.ForTenant(b.Tenant, b.Membership)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, effect := range []string{"allow", "deny"} {
		wg.Add(1)
		go func(effect string) {
			defer wg.Done()
			results <- repo.APIReplaceRole(context.Background(), role, []RolePolicy{{"audit.read", effect}}, version)
		}(effect)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal("unexpected concurrent history result")
		}
	}
	if success != 1 || conflicts != 1 || rbacVersion(t, s, b) != version+1 || len(policyHistory(t, s, b, role).Items) != 2 {
		t.Fatal("parallel role writes captured more than one winner")
	}
	page := policyHistory(t, s, b, role)
	var changed RolePolicyHistoryEntry
	for _, v := range page.Items {
		if v.Action == "role.updated" {
			changed = v
		}
	}
	var actual string
	if e := s.db.QueryRow(`SELECT effect FROM role_policies WHERE tenant_id=? AND role_id=? AND permission='audit.read'`, b.Tenant, role).Scan(&actual); e != nil || len(changed.Before) != 0 || len(changed.After) != 1 || changed.After[0].Effect != actual {
		t.Fatal("concurrent snapshot mismatched committed role")
	}
}
func TestRolePolicyHistoryCorruptRowsReturnNoPartialData(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	role := rbacRole(t, s, b, "corrupt_history", nil)
	repo := s.ForTenant(b.Tenant, b.Membership)
	if e := repo.APIReplaceRole(context.Background(), role, []RolePolicy{{"audit.read", "allow"}}, rbacVersion(t, s, b)); e != nil {
		t.Fatal("corruption fixture update failed")
	}
	page := policyHistory(t, s, b, role)
	event, _ := ParseID(page.Items[len(page.Items)-1].ID)
	if _, e := s.db.Exec(`UPDATE role_policy_changes SET after_policies=? WHERE event_id=? AND tenant_id=?`, `[{"permission":"audit.read","effect":"allow","secret_key":"private-snapshot-marker"}]`, event, b.Tenant); e != nil {
		t.Fatal("corruption fixture injection failed")
	}
	result, e := repo.APIRolePolicyHistory(context.Background(), role, 100, nil)
	if !errors.Is(e, ErrPolicyHistoryUnavailable) || result.Format != "" || result.Items != nil || strings.Contains(e.Error(), "private-snapshot-marker") {
		t.Fatal("malformed history escaped or returned a partial page")
	}
}
func TestRolePolicyHistoryCrossLinkedNamespaceNeverDiscloses(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	role := rbacRole(t, s, b, "primary_history", nil)
	other := fixture(t, s)
	otherRole := rbacRole(t, s, other, "other_namespace", nil)
	page := policyHistory(t, s, b, role)
	event, _ := ParseID(page.Items[0].ID)
	if _, e := s.db.Exec(`UPDATE role_policy_changes SET tenant_id=?,role_id=? WHERE event_id=?`, other.Tenant, otherRole, event); e != nil {
		t.Fatal("namespace fault fixture failed")
	}
	defer func() {
		if _, e := s.db.Exec(`UPDATE role_policy_changes SET tenant_id=?,role_id=? WHERE event_id=?`, b.Tenant, role, event); e != nil {
			t.Error("namespace fixture restoration failed")
		}
	}()
	foreign := policyHistory(t, s, other, otherRole)
	if len(foreign.Items) != 1 || foreign.Items[0].ID == event.String() {
		t.Fatal("snapshot-only namespace trusted over audit namespace")
	}
	if _, e := s.ForTenant(other.Tenant, other.Membership).APIRolePolicyHistory(context.Background(), otherRole, 1, &event); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-linked cursor disclosed a foreign event")
	}
}
func TestRolePolicyHistoryEmptyPoliciesAndProtectedRole(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	repo := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	var owner ID
	if e := s.db.QueryRow(`SELECT id FROM roles WHERE tenant_id=? AND code='owner'`, b.Tenant).Scan(&owner); e != nil {
		t.Fatal("owner metadata fixture missing")
	}
	legacy := policyHistory(t, s, b, owner)
	if len(legacy.Items) != 0 || legacy.Items == nil {
		t.Fatal("legacy owner history was fabricated")
	}
	role := rbacRole(t, s, b, "empty_history", nil)
	version := rbacVersion(t, s, b)
	if e := repo.APIReplaceRole(ctx, owner, nil, version); !errors.Is(e, ErrReservedRole) {
		t.Fatal("owner policy update allowed")
	}
	if e := repo.APIDeleteRole(ctx, owner, version); !errors.Is(e, ErrReservedRole) {
		t.Fatal("owner policy deletion allowed")
	}
	if e := repo.APIReplaceRole(ctx, role, []RolePolicy{{"roles.manage", "allow"}}, version); !errors.Is(e, ErrInvalid) {
		t.Fatal("private capability admitted into snapshots")
	}
	if e := repo.APIReplaceRole(ctx, role, []RolePolicy{{"audit.read", "allow"}}, version-1); !errors.Is(e, ErrConflict) {
		t.Fatal("stale role write captured")
	}
	if len(policyHistory(t, s, b, role).Items) != 1 || rbacVersion(t, s, b) != version {
		t.Fatal("refused mutations changed history/version")
	}
	if e := repo.APIDeleteRole(ctx, role, version); e != nil {
		t.Fatal("empty policy role deletion failed")
	}
	raw, _ := json.Marshal(policyHistory(t, s, b, role))
	if strings.Contains(string(raw), `"before":null`) || strings.Contains(string(raw), `"after":null`) {
		t.Fatal("empty rules serialized as unknown/null")
	}
}
func TestStoredRolePolicyHistoryCanonicalReviewedShape(t *testing.T) {
	valid := []byte(`[{"permission":"members.read","effect":"deny"},{"permission":"audit.read","effect":"allow"}]`)
	policies, e := storedRolePolicies(valid)
	if e != nil || len(policies) != 2 || policies[0].Permission != "audit.read" {
		t.Fatal("stored policies not normalized")
	}
	for _, body := range []string{`null`, `{}`, `[{"permission":"roles.manage","effect":"allow"}]`, `[{"Permission":"audit.read","effect":"allow"}]`, `[{"permission":"audit.read","effect":"allow","email":"private-shape"}]`, `[{"permission":"audit.read","effect":"allow"},{"permission":"audit.read","effect":"allow"}]`} {
		if _, e := storedRolePolicies([]byte(body)); !errors.Is(e, ErrPolicyHistoryUnavailable) {
			t.Fatal("stored private or malformed policy admitted")
		}
	}
	if _, e := normalizePolicyChange("role.updated", rolePolicyChange{Code: "same", Before: policies, After: policies}); !errors.Is(e, ErrPolicyHistoryUnavailable) {
		t.Fatal("no-op snapshot accepted")
	}
	if _, e := normalizePolicyChange("role.created", rolePolicyChange{Code: "owner"}); !errors.Is(e, ErrPolicyHistoryUnavailable) {
		t.Fatal("reserved role snapshot accepted")
	}
}
