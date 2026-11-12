package mysql

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	driver "github.com/go-sql-driver/mysql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func userAccessFixture(t *testing.T, s Store, b Bootstrap) ID {
	t.Helper()
	v, e := s.ForTenant(b.Tenant, b.Membership).APICreateApplication(context.Background(), "User admission fixture")
	if e != nil {
		t.Fatal("owned admission fixture failed")
	}
	id, _ := ParseID(v.ID)
	return id
}
func TestApplicationUserAccessDefaultOwnerMembershipAndNamespace(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	other := fixture(t, s)
	member := rbacMember(t, s, b)
	app := userAccessFixture(t, s, b)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, b.Membership)
	page, e := repo.APIApplicationUserAccess(ctx, app, 25, ID{})
	if e != nil || page.Policy.Mode != UserAccessAll || page.Policy.Version != "1" || len(page.Items) != 0 || page.Items == nil {
		t.Fatal("legacy default policy not lossless/empty")
	}
	d, e := repo.APIApplicationUserAccessDecision(ctx, app, member)
	if e != nil || !d.Allowed || d.Reason != "all_members" {
		t.Fatal("default member admission refused")
	}
	p, e := repo.SetApplicationUserAccessMode(ctx, app, UserAccessSelected, 1)
	if e != nil || p.Version != "2" {
		t.Fatal("restricted mode failed")
	}
	d, e = repo.APIApplicationUserAccessDecision(ctx, app, member)
	if e != nil || d.Allowed || d.Reason != "not_granted" {
		t.Fatal("ungranted member admitted")
	}
	d, e = repo.APIApplicationUserAccessDecision(ctx, app, b.Membership)
	if e != nil || !d.Allowed || d.Reason != "owner" {
		t.Fatal("owner recovery denied")
	}
	p, e = repo.SetApplicationUserAccessMember(ctx, app, member, true, 2)
	if e != nil || p.Version != "3" {
		t.Fatal("explicit member grant failed")
	}
	d, e = repo.APIApplicationUserAccessDecision(ctx, app, member)
	if e != nil || !d.Allowed || d.Reason != "explicit_grant" {
		t.Fatal("current explicit grant not reflected")
	}
	if _, e = s.ForTenant(b.Tenant, member).APIApplicationUserAccess(ctx, app, 25, ID{}); !errors.Is(e, ErrDenied) {
		t.Fatal("delegate read admission policy")
	}
	if _, e = s.ForTenant(other.Tenant, other.Membership).APIApplicationUserAccess(ctx, app, 25, ID{}); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign application disclosed")
	}
	if _, e = repo.APIApplicationUserAccessDecision(ctx, app, other.Membership); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign target disclosed")
	}
	if _, e = s.db.Exec(`UPDATE memberships SET status='suspended' WHERE tenant_id=? AND id=?`, b.Tenant, member); e != nil {
		t.Fatal("inactive target fixture failed")
	}
	d, e = repo.APIApplicationUserAccessDecision(ctx, app, member)
	if e != nil || d.Allowed || d.Reason != "membership_inactive" {
		t.Fatal("inactive target admitted")
	}
	if _, e = repo.SetApplicationUserAccessMember(ctx, app, member, true, 3); !errors.Is(e, ErrInvalid) {
		t.Fatal("inactive target grant permitted")
	}
	if p, e = repo.SetApplicationUserAccessMember(ctx, app, member, false, 3); e != nil || p.Version != "4" {
		t.Fatal("inactive target cleanup refused")
	}
	shared := applicationFixture(t, s, b.Tenant)
	if _, e = repo.APIApplicationUserAccess(ctx, shared, 25, ID{}); !errors.Is(e, ErrApplicationUnmanaged) {
		t.Fatal("shared application policy exposed")
	}
}
func TestApplicationUserAccessPaginationAndNoopVersions(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	app := userAccessFixture(t, s, b)
	a := rbacMember(t, s, b)
	c := rbacMember(t, s, b)
	repo := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	if _, e := repo.SetApplicationUserAccessMember(ctx, app, c, true, 1); e != nil {
		t.Fatal("first grant failed")
	}
	p, e := repo.SetApplicationUserAccessMember(ctx, app, a, true, 2)
	if e != nil || p.Version != "3" {
		t.Fatal("second grant failed")
	}
	for _, grant := range []bool{true, true} {
		p, e = repo.SetApplicationUserAccessMember(ctx, app, a, grant, 3)
		if e != nil || p.Version != "3" {
			t.Fatal("same grant changed revision")
		}
	}
	p, e = repo.SetApplicationUserAccessMode(ctx, app, UserAccessAll, 3)
	if e != nil || p.Version != "3" {
		t.Fatal("same mode changed revision")
	}
	first, e := repo.APIApplicationUserAccess(ctx, app, 1, ID{})
	if e != nil || len(first.Items) != 1 || first.Next == nil {
		t.Fatal("bounded admission page failed")
	}
	after, _ := ParseID(*first.Next)
	next, e := repo.APIApplicationUserAccess(ctx, app, 1, after)
	if e != nil || len(next.Items) != 1 || next.Next != nil || next.Items[0].Member <= first.Items[0].Member {
		t.Fatal("admission cursor did not advance")
	}
	if _, e = repo.SetApplicationUserAccessMember(ctx, app, a, false, 2); !errors.Is(e, ErrConflict) {
		t.Fatal("stale member write accepted")
	}
	for _, n := range []int{0, 101} {
		if _, e = repo.APIApplicationUserAccess(ctx, app, n, ID{}); !errors.Is(e, ErrInvalid) {
			t.Fatal("unbounded read allowed")
		}
	}
}
func TestApplicationUserAccessIndependentVersionsAndAuditGraph(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	app := userAccessFixture(t, s, b)
	member := rbacMember(t, s, b)
	repo := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	if _, e := repo.SetApplicationUserAccessMode(ctx, app, UserAccessSelected, 1); e != nil {
		t.Fatal("mode change failed")
	}
	if _, e := repo.SetApplicationUserAccessMember(ctx, app, member, true, 2); e != nil {
		t.Fatal("grant failed")
	}
	if _, e := repo.SetApplicationUserAccessMember(ctx, app, member, false, 3); e != nil {
		t.Fatal("revoke failed")
	}
	if _, e := repo.SetApplicationUserAccessMode(ctx, app, UserAccessAll, 4); e != nil {
		t.Fatal("mode restore failed")
	}
	if rbacVersion(t, s, b) != 1 || count(t, s, `SELECT security_version FROM tenant_applications WHERE tenant_id=? AND application_id=?`, b.Tenant, app) != 1 {
		t.Fatal("User admission reused tenant or machine authority counters")
	}
	n := count(t, s, `SELECT COUNT(*) FROM application_user_access_changes c JOIN audit_events a ON a.event_id=c.event_id JOIN outbox_events o ON o.event_id=c.event_id WHERE c.tenant_id=? AND c.application_id=? AND a.tenant_id=c.tenant_id AND a.target_id=c.application_id AND o.tenant_id=c.tenant_id AND o.aggregate_id=c.application_id AND o.aggregate_version=c.version AND a.action=o.type`, b.Tenant, app)
	if n != 4 || count(t, s, `SELECT COUNT(*) FROM application_user_access_changes WHERE tenant_id=? AND application_id=? AND membership_id=?`, b.Tenant, app, member) != 2 {
		t.Fatal("admission changes lost atomic actor/app/member/revision context")
	}
}

var errUserAccessStorage = errors.New("injected user admission storage failure")

type userAccessFailureConnector struct{ sqldriver.Connector }

func (c userAccessFailureConnector) Connect(ctx context.Context) (sqldriver.Conn, error) {
	v, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &userAccessFailureConn{v}, nil
}

type userAccessFailureConn struct{ sqldriver.Conn }

func (c *userAccessFailureConn) BeginTx(ctx context.Context, o sqldriver.TxOptions) (sqldriver.Tx, error) {
	return c.Conn.(sqldriver.ConnBeginTx).BeginTx(ctx, o)
}
func (c *userAccessFailureConn) QueryContext(ctx context.Context, q string, args []sqldriver.NamedValue) (sqldriver.Rows, error) {
	return c.Conn.(sqldriver.QueryerContext).QueryContext(ctx, q, args)
}
func (c *userAccessFailureConn) ExecContext(ctx context.Context, q string, args []sqldriver.NamedValue) (sqldriver.Result, error) {
	if strings.HasPrefix(strings.TrimSpace(q), "INSERT INTO application_user_access_changes") {
		return nil, errUserAccessStorage
	}
	return c.Conn.(sqldriver.ExecerContext).ExecContext(ctx, q, args)
}
func TestApplicationUserAccessContextFailureRollsBackEveryWrite(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	app := userAccessFixture(t, s, b)
	member := rbacMember(t, s, b)
	ctx := context.Background()
	repo := s.ForTenant(b.Tenant, b.Membership)
	if _, e := repo.SetApplicationUserAccessMode(ctx, app, UserAccessSelected, 1); e != nil {
		t.Fatal("rollback policy fixture failed")
	}
	cfg := driver.NewConfig()
	cfg.User = "business_app"
	cfg.Passwd = os.Getenv("M1_DB_PASSWORD")
	cfg.Net = "tcp"
	cfg.Addr = "mysql:3306"
	cfg.DBName = "sso_business"
	cfg.ParseTime = true
	cfg.Timeout = 2 * time.Second
	connector, e := driver.NewConnector(cfg)
	if e != nil {
		t.Fatal("failure connector unavailable")
	}
	db := sql.OpenDB(userAccessFailureConnector{connector})
	defer db.Close()
	before := count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant)
	if _, e = (Store{db}).ForTenant(b.Tenant, b.Membership).SetApplicationUserAccessMember(ctx, app, member, true, 2); !errors.Is(e, errUserAccessStorage) {
		t.Fatal("real context storage failure not observed")
	}
	page, e := repo.APIApplicationUserAccess(ctx, app, 25, ID{})
	if e != nil || page.Policy.Version != "2" || len(page.Items) != 0 || count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant) != before || count(t, s, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=?`, b.Tenant) != before || count(t, s, `SELECT COUNT(*) FROM application_user_access_changes WHERE tenant_id=?`, b.Tenant) != 1 {
		t.Fatal("context failure partially committed policy/member/audit/outbox")
	}
}
func TestApplicationUserAccessConcurrentChangesHaveOneWinner(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	app := userAccessFixture(t, s, b)
	members := []ID{rbacMember(t, s, b), rbacMember(t, s, b)}
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, member := range members {
		group.Add(1)
		go func(id ID) {
			defer group.Done()
			_, e := s.ForTenant(b.Tenant, b.Membership).SetApplicationUserAccessMember(context.Background(), app, id, true, 1)
			results <- e
		}(member)
	}
	group.Wait()
	close(results)
	ok, conflicts := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal("unexpected concurrent admission result")
		}
	}
	page, e := s.ForTenant(b.Tenant, b.Membership).APIApplicationUserAccess(context.Background(), app, 25, ID{})
	if e != nil || ok != 1 || conflicts != 1 || len(page.Items) != 1 || page.Policy.Version != "2" {
		t.Fatal("concurrent admission changes committed more than one winner")
	}
}

func TestApplicationUserAccessGrantBoundariesRejectUnknownIdentity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	checks := []struct {
		name string
		run  func() error
	}{
		{"resource", func() error { return s.GrantCurrent(ctx, ID{}, ID{}, ID{}, "unknown") }},
		{"token", func() error {
			_, e := s.CheckBusinessToken(ctx, TokenCheck{Grant: "authorization_code", Client: "unknown"})
			return e
		}},
		{"authority", func() error {
			return s.APITransaction(ctx, Authorization{Client: "unknown"}, func(*sql.Tx) error { return nil })
		}},
	}
	for _, check := range checks {
		if e := check.run(); !errors.Is(e, ErrDenied) {
			var failure *driver.MySQLError
			number := uint16(0)
			if errors.As(e, &failure) {
				number = failure.Number
			}
			t.Fatalf("unknown identity boundary %s returned unexpected database class %d", check.name, number)
		}
	}
	rows, e := s.TenantOptions(ctx, ID{}, ID{}, "unknown")
	if e != nil || len(rows) != 0 {
		t.Fatal("unknown consent identity yielded options or database failure")
	}
}
