package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
)

// These tests can connect only to a disposable MySQL on the local loopback port.
func isolatedTestDB(t *testing.T) (*sql.DB, IsolatedBFFStore, Store) {
	t.Helper()
	addr := os.Getenv("SSO_BFF_TEST_MYSQL_ADDR")
	if addr == "" {
		t.Skip("requires disposable local MySQL; see scripts/test-bff-isolation")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		t.Fatal("refusing non-loopback integration database")
	}
	cfg := driver.NewConfig()
	cfg.User = "root"
	cfg.Passwd = "local-bff-test-only"
	cfg.Net = "tcp"
	cfg.Addr = addr
	cfg.ParseTime = true
	cfg.MultiStatements = true
	root, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	id, _ := NewID()
	suffix := id.String()
	business := "bff_test_business_" + suffix
	local := "bff_test_sessions_" + suffix
	user := "bff_" + suffix[:20]
	for _, name := range []string{business, local} {
		if _, err = root.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		root.Exec("DROP USER IF EXISTS '" + user + "'@'%'")
		root.Exec("DROP DATABASE IF EXISTS " + local)
		root.Exec("DROP DATABASE IF EXISTS " + business)
	})
	cfg.DBName = business
	central, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { central.Close() })
	files, err := filepath.Glob("../../../migrations/business/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		body, e := os.ReadFile(f)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = central.Exec(string(body)); e != nil {
			t.Fatalf("migration %s: %v", filepath.Base(f), e)
		}
	}
	cfg.DBName = local
	setup, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { setup.Close() })
	body, err := os.ReadFile("../../../migrations/bff/001_sessions.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = setup.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	if _, err = root.Exec("CREATE USER '" + user + "'@'%' IDENTIFIED BY 'local-only'; GRANT SELECT,INSERT,UPDATE,DELETE ON " + local + ".* TO '" + user + "'@'%'"); err != nil {
		t.Fatal(err)
	}
	cfg.User, cfg.Passwd, cfg.MultiStatements = user, "local-only", false
	restricted, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restricted.Close() })
	if _, err = restricted.Exec("SELECT 1 FROM " + business + ".users LIMIT 0"); err == nil {
		t.Fatal("BFF account can read the business database")
	}
	return setup, NewIsolatedBFFStore(NewStore(restricted)), NewStore(central)
}
func TestIsolatedBFFSessionAndDurableRevocation(t *testing.T) {
	db, s, _ := isolatedTestDB(t)
	ctx := context.Background()
	client := "sso-bff"
	if err := s.BFFStorageReady(ctx, client); err != nil {
		t.Fatal(err)
	}
	state, browser := sha256.Sum256([]byte("state")), sha256.Sum256([]byte("browser"))
	if err := s.CreateBFFTransaction(ctx, state[:], browser[:], client, []byte("encrypted PKCE")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeBFFTransaction(ctx, state[:], make([]byte, 32), client); !errors.Is(err, ErrDenied) {
		t.Fatal("accepted wrong browser")
	}
	if _, err := s.ConsumeBFFTransaction(ctx, state[:], browser[:], client); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeBFFTransaction(ctx, state[:], browser[:], client); !errors.Is(err, ErrDenied) {
		t.Fatal("replayed transaction")
	}
	for _, reason := range []string{"authorization.logout", "authorization.refresh_failed", "authorization.session_expired"} {
		digest := sha256.Sum256([]byte(reason))
		user, _ := NewID()
		grant, _ := NewID()
		cipher := []byte("encrypted tokens and revocation credential")
		if err := s.CreateBFFSession(ctx, digest[:], client, user, grant, cipher); err != nil {
			t.Fatal(err)
		}
		if _, err := s.BFFSession(ctx, digest[:], client); err != nil {
			t.Fatal(err)
		}
		if reason == "authorization.logout" {
			if err := s.RevokeBFF(ctx, digest[:], client); err != nil {
				t.Fatal(err)
			}
			if err := s.RevokeBFF(ctx, digest[:], client); err != nil {
				t.Fatal(err)
			}
		} else if reason == "authorization.session_expired" {
			db.Exec("UPDATE bff_sessions SET idle_expires_at=0 WHERE token_digest=?", digest[:])
			if did, err := s.ExpireOneBFFSession(ctx, client); !did || err != nil {
				t.Fatal("expired session not queued")
			}
		} else {
			fail := errors.New("refresh unavailable")
			if err := s.RefreshBFF(ctx, digest[:], client, func(BFFSession) ([]byte, error) { return nil, fail }); !errors.Is(err, fail) {
				t.Fatal("refresh failure not returned")
			}
		}
		if _, err := s.BFFSession(ctx, digest[:], client); !errors.Is(err, ErrDenied) {
			t.Fatal("revoked local session remains active")
		}
		send := func(token, payload []byte, action string) error {
			if !bytes.Equal(token, digest[:]) || !bytes.Equal(payload, cipher) || action != reason {
				t.Fatal("outbox binding mismatch")
			}
			return errors.New("platform unavailable")
		}
		if did, err := s.DeliverOneRevocation(ctx, client, send); !did || err == nil {
			t.Fatal("failed delivery was not retained")
		}
		var pending int
		if err := db.QueryRow("SELECT COUNT(*) FROM bff_revocation_outbox WHERE token_digest=? AND state='pending' AND encrypted_tokens<>'' AND attempts=1", digest[:]).Scan(&pending); err != nil || pending != 1 {
			t.Fatal("lost retry after failure")
		}
		db.Exec("UPDATE bff_revocation_outbox SET next_attempt_at=0 WHERE token_digest=?", digest[:])
		if did, err := s.DeliverOneRevocation(ctx, client, func([]byte, []byte, string) error { return nil }); !did || err != nil {
			t.Fatal("retry failed")
		}
		if err := db.QueryRow("SELECT COUNT(*) FROM bff_revocation_outbox WHERE token_digest=? AND state='done' AND encrypted_tokens=''", digest[:]).Scan(&pending); err != nil || pending != 1 {
			t.Fatal("completed outbox retained secrets")
		}
	}
}
func grantFixture(t *testing.T, s Store) Authorization {
	t.Helper()
	ctx := context.Background()
	user, _ := NewID()
	app, _ := NewID()
	session, _ := NewID()
	flow, _ := NewID()
	grant, _ := NewID()
	client := "test-bff-" + grant.String()
	now := time.Now().Unix()
	digest := sha256.Sum256([]byte(grant.String()))
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	must("INSERT INTO users(id) VALUES(?)", user)
	b, err := s.BootstrapTenant(ctx, user, "bff-"+user.String(), "Test")
	if err != nil {
		t.Fatal(err)
	}
	must("INSERT INTO applications(id,code,name) VALUES(?,?,?)", app, client, "Test")
	must("INSERT INTO oauth_clients(client_id,application_id,redirect_uri,scopes,audience) VALUES(?,?,?,'openid platform:read platform:write','https://api.sso.local')", client, app, "https://bff.example/oidc/callback")
	must("INSERT INTO tenant_applications(tenant_id,application_id) VALUES(?,?)", b.Tenant, app)
	must("INSERT INTO mfa_factors(user_id,encrypted_secret,last_step,active) VALUES(?,?,0,TRUE)", user, []byte("test"))
	must("INSERT INTO sso_sessions(id,token_digest,user_id,user_version,mfa_version,auth_time,amr,idle_expires_at,absolute_expires_at) VALUES(?,?,?,1,1,?,'otp',?,?)", session, digest[:], user, now, now+1800, now+3600)
	must("INSERT INTO login_interactions(id,kind,challenge_digest,encrypted_challenge,browser_digest,csrf_digest,client_id,user_id,user_version,session_id,hydra_sid,requested_scopes,audience,state,expires_at) VALUES(?,'consent',?,?,?,?,?,?,1,?,'test-sid','openid platform:read platform:write','https://api.sso.local','complete',?)", flow, digest[:], []byte("test"), digest[:], digest[:], client, user, session, now+300)
	must("INSERT INTO business_grants(id,interaction_id,tenant_id,membership_id,user_id,session_id,client_id,scopes,audience,hydra_sid,user_version,membership_version,tenant_version,client_version,application_version,enabled_version,protocol_binding,absolute_expires_at) VALUES(?,?,?,?,?,?,?,'openid platform:read platform:write','https://api.sso.local','test-sid',1,1,1,1,1,1,?,?)", grant, flow, b.Tenant, b.Membership, user, session, client, digest[:], now+3600)
	return Authorization{Grant: grant, User: user, Tenant: b.Tenant, Member: b.Membership, Session: session, Client: client, Expires: now + 300}
}
func TestBFFCapabilityCannotEscalateAndRevokesAfterUserDisable(t *testing.T) {
	_, _, s := isolatedTestDB(t)
	ctx := context.Background()
	a := grantFixture(t, s)
	digest := sha256.Sum256([]byte("only one revocation credential"))
	expiry, err := s.APIIssueBFFRevocation(ctx, a, digest[:])
	if err != nil || expiry <= time.Now().Unix() {
		t.Fatalf("capability issue failed: %v", err)
	}
	if err = s.APIRevokeBFFGrant(ctx, digest[:], "other-client", "authorization.logout"); !errors.Is(err, ErrDenied) {
		t.Fatal("cross-client revoke accepted")
	}
	if err = s.APIRevokeBFFGrant(ctx, digest[:], a.Client, "tenant.delete"); !errors.Is(err, ErrInvalid) {
		t.Fatal("arbitrary action accepted")
	}
	// An outbox write failure must roll back both grant deactivation and audit.
	if _, err = s.db.Exec(`CREATE TRIGGER fail_bff_outbox BEFORE INSERT ON outbox_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='test outbox failure'`); err != nil {
		t.Fatal(err)
	}
	if err = s.APIRevokeBFFGrant(ctx, digest[:], a.Client, "authorization.logout"); err == nil {
		t.Fatal("revocation ignored outbox failure")
	}
	var stillActive bool
	var failedAudits int
	if err = s.db.QueryRow("SELECT active FROM business_grants WHERE id=?", a.Grant).Scan(&stillActive); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow("SELECT COUNT(*) FROM audit_events WHERE target_id=? AND action='authorization.logout'", a.Grant).Scan(&failedAudits); err != nil {
		t.Fatal(err)
	}
	if !stillActive || failedAudits != 0 {
		t.Fatal("failed outbox escaped transaction rollback")
	}
	if _, err = s.db.Exec("DROP TRIGGER fail_bff_outbox"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE users SET status='suspended' WHERE id=?", a.User); err != nil {
		t.Fatal(err)
	}
	bad := sha256.Sum256([]byte("cannot issue after disable"))
	if _, err = s.APIIssueBFFRevocation(ctx, a, bad[:]); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked authority issued a capability")
	}
	// A previously issued one-purpose capability can still revoke, never authenticate.
	for range 2 {
		if err = s.APIRevokeBFFGrant(ctx, digest[:], a.Client, "authorization.logout"); err != nil {
			t.Fatal(err)
		}
	}
	var active bool
	var audits, outbox int
	s.db.QueryRow("SELECT active FROM business_grants WHERE id=?", a.Grant).Scan(&active)
	s.db.QueryRow("SELECT COUNT(*) FROM audit_events WHERE target_id=? AND action='authorization.logout'", a.Grant).Scan(&audits)
	s.db.QueryRow("SELECT COUNT(*) FROM outbox_events WHERE aggregate_id=? AND type='authorization.logout'", a.Grant).Scan(&outbox)
	if active || audits != 1 || outbox != 1 {
		t.Fatal("grant revoke/audit/outbox not atomic and idempotent")
	}
	if _, err = s.db.Exec("UPDATE bff_revocation_credentials SET expires_at=? WHERE token_digest=?", time.Now().Unix()-1, digest[:]); err != nil {
		t.Fatal(err)
	}
	if err = s.APIRevokeBFFGrant(ctx, digest[:], a.Client, "authorization.logout"); !errors.Is(err, ErrBFFRevocationExpired) {
		t.Fatal("expired capability accepted")
	}
	if strings.Contains(a.Client, "sso-bff") {
		t.Fatal("test client unexpectedly used a real deployment ID")
	}
}
