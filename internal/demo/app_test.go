package demo

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	driver "github.com/go-sql-driver/mysql"
	"github.com/lth2015/sso/internal/localruntime"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"golang.org/x/oauth2"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDemoRejectsUnknownIssuerDestination(t *testing.T) {
	r, _ := http.NewRequest("GET", "https://evil.invalid/.well-known/jwks.json", nil)
	if _, err := (Bridge{http.DefaultTransport}).RoundTrip(r); err == nil {
		t.Fatal("untrusted issuer destination allowed")
	}
}
func TestDemoRealIssuedTokenValidation(t *testing.T) {
	if os.Getenv("SSO_DEMO_TEST") != "true" || os.Getenv("DEMO_TEST_PHASE") == "revoked" {
		t.Skip("real signed token fixture is created by tests/sso/browser.cjs")
	}
	user, err := store.ParseID(os.Getenv("DEMO_TEST_USER"))
	if err != nil {
		t.Fatal("fixture user invalid")
	}
	db, err := localruntime.Database()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT token_digest,client_id,encrypted_tokens FROM bff_sessions WHERE user_id=? AND active=TRUE`, user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var digest, cipher []byte
		var client string
		if err = rows.Scan(&digest, &client, &cipher); err != nil {
			t.Fatal(err)
		}
		secret := os.Getenv("DEMO_A_SECRET")
		if client == "local-demo-b" {
			secret = os.Getenv("DEMO_B_SECRET")
		}
		keyName := "DEMO_A_BFF_KEY"
		if client == "local-demo-b" {
			keyName = "DEMO_B_BFF_KEY"
		}
		key, err := hex.DecodeString(os.Getenv(keyName))
		if err != nil {
			t.Fatal(err)
		}
		app, err := New(context.Background(), store.NewStore(db), client, secret, key)
		if err != nil {
			t.Fatal("provider initialization failed")
		}
		var tokens Tokens
		if app.open(cipher, "session:"+client+":"+base64.RawURLEncoding.EncodeToString(digest), &tokens) != nil {
			t.Fatal("fixture decrypt failed")
		}
		if _, err = app.verifyID(context.Background(), tokens.ID, tokens.Nonce); err != nil {
			t.Fatal("legitimate ID token rejected")
		}
		if _, err = app.verifyAccess(context.Background(), tokens.Access, tokens.User); err != nil {
			t.Fatal("legitimate access token rejected")
		}
		if _, err = app.verifyID(context.Background(), tokens.ID, "wrong-nonce"); err == nil {
			t.Fatal("wrong nonce accepted")
		}
		if _, err = app.verifyAccess(context.Background(), tokens.ID, tokens.User); err == nil {
			t.Fatal("ID token accepted as access token")
		}
		parts := strings.Split(tokens.Access, ".")
		first := "A"
		if parts[2][0] == 'A' {
			first = "B"
		}
		parts[2] = first + parts[2][1:]
		if _, err = app.verifyAccess(context.Background(), strings.Join(parts, "."), tokens.User); err == nil {
			t.Fatal("modified signature accepted")
		}
		pieces := strings.Split(tokens.Access, ".")
		header, _ := base64.RawURLEncoding.DecodeString(pieces[0])
		var values map[string]any
		_ = json.Unmarshal(header, &values)
		values["alg"] = "none"
		encoded, _ := json.Marshal(values)
		pieces[0] = base64.RawURLEncoding.EncodeToString(encoded)
		pieces[2] = ""
		if _, err = app.verifyAccess(context.Background(), strings.Join(pieces, "."), tokens.User); err == nil {
			t.Fatal("unsigned token accepted")
		}
		u, _ := url.Parse("http://other.invalid/jwks")
		request := &http.Request{URL: u}
		if _, err = app.HTTP.Transport.RoundTrip(request); err == nil {
			t.Fatal("token-controlled JWKS destination allowed")
		}
		count++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count < 2 {
		t.Fatal("both real application fixtures required")
	}
}

func TestDemoPasswordResetRejectsActualRefresh(t *testing.T) {
	if os.Getenv("SSO_DEMO_TEST") != "true" || os.Getenv("DEMO_TEST_PHASE") != "revoked" {
		t.Skip("requires password-reset browser checkpoint")
	}
	user, err := store.ParseID(os.Getenv("DEMO_TEST_USER"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := localruntime.Database()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT b.token_digest,b.client_id,b.encrypted_tokens FROM bff_sessions b JOIN business_grants g ON g.id=b.grant_id JOIN users u ON u.id=b.user_id WHERE b.user_id=? AND b.active=TRUE AND g.user_version<u.security_version`, user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var digest, cipher []byte
		var client string
		if err = rows.Scan(&digest, &client, &cipher); err != nil {
			t.Fatal(err)
		}
		secret := os.Getenv("DEMO_A_SECRET")
		if client == "local-demo-b" {
			secret = os.Getenv("DEMO_B_SECRET")
		}
		keyName := "DEMO_A_BFF_KEY"
		if client == "local-demo-b" {
			keyName = "DEMO_B_BFF_KEY"
		}
		key, err := hex.DecodeString(os.Getenv(keyName))
		if err != nil {
			t.Fatal(err)
		}
		app, err := New(context.Background(), store.NewStore(db), client, secret, key)
		if err != nil {
			t.Fatal("issuer initialization failed")
		}
		var tokens Tokens
		if app.open(cipher, "session:"+client+":"+base64.RawURLEncoding.EncodeToString(digest), &tokens) != nil {
			t.Fatal("fixture decrypt failed")
		}
		if _, err = app.verifyAccess(context.Background(), tokens.Access, tokens.User); err == nil {
			t.Fatal("old JWT remained authorized after password reset")
		}
		next, err := app.OAuth.TokenSource(app.ctx(context.Background()), &oauth2.Token{AccessToken: tokens.Access, RefreshToken: tokens.Refresh, Expiry: time.Now().Add(-time.Minute)}).Token()
		if err == nil || next != nil {
			t.Fatal("old refresh issued a token after password reset")
		}
		var protocolError *oauth2.RetrieveError
		if !errors.As(err, &protocolError) || protocolError.Response.StatusCode != 403 {
			t.Fatal("refresh did not fail through the authoritative policy hook")
		}
		count++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count < 2 {
		t.Fatal("both prior application grants required")
	}
}

func TestDemoDatabaseRolesCannotReadCredentialsOrOtherClientState(t *testing.T) {
	if os.Getenv("SSO_DEMO_TEST") != "true" {
		t.Skip("requires real scoped BFF database roles")
	}
	for _, role := range []struct{ User, Database, Client, Password string }{
		{"demo_a", "local_demo_a_state", "local-demo-a", "DEMO_A_DB_PASSWORD"},
		{"demo_b", "local_demo_b_state", "local-demo-b", "DEMO_B_DB_PASSWORD"},
		{"console_bff", "local_console_state", "local-console", "CONSOLE_DB_PASSWORD"},
	} {
		cfg := driver.NewConfig()
		cfg.User = role.User
		cfg.Passwd = os.Getenv(role.Password)
		cfg.Net = "tcp"
		cfg.Addr = "mysql:3306"
		cfg.DBName = role.Database
		db, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err = db.Ping(); err != nil {
			t.Fatal("BFF database role unavailable")
		}
		for _, query := range []string{`SELECT mode FROM sso_business.application_user_access_policies LIMIT 1`, `SELECT membership_id FROM sso_business.application_user_access_members LIMIT 1`, `SELECT version FROM sso_business.application_user_grant_versions LIMIT 1`, `SELECT normalized_email FROM sso_business.login_aliases LIMIT 1`, `SELECT password_hash FROM sso_business.password_credentials LIMIT 1`, `SELECT encrypted_secret FROM sso_business.mfa_factors LIMIT 1`, `SELECT token_digest FROM sso_business.sso_sessions LIMIT 1`, `SELECT encrypted_tokens FROM sso_business.bff_sessions LIMIT 1`} {
			rows, err := db.Query(query)
			if err == nil {
				rows.Close()
				t.Fatal("BFF role could read protected data")
			}
			var denied *driver.MySQLError
			if !errors.As(err, &denied) || (denied.Number != 1142 && denied.Number != 1143) {
				t.Fatal("protected read did not fail through database permissions")
			}
		}
		for _, query := range []string{
			`SELECT COUNT(*) FROM application_user_grant_versions v LEFT JOIN business_grants g ON g.id=v.grant_id WHERE g.id IS NULL`,
			`SELECT COUNT(*) FROM roles WHERE code<>'owner'`,
			`SELECT COUNT(*) FROM role_bindings b LEFT JOIN memberships m ON m.tenant_id=b.tenant_id AND m.id=b.membership_id WHERE m.id IS NULL`,
		} {
			var n int
			if e := db.QueryRow(query).Scan(&n); e != nil || n != 0 {
				t.Fatal("BFF admission metadata escaped the client/member scope")
			}
		}
		var others int
		if err = db.QueryRow(`SELECT COUNT(*) FROM bff_sessions WHERE client_id<>?`, role.Client).Scan(&others); err != nil || others != 0 {
			t.Fatal("BFF state view leaked another client")
		}
		if _, err = db.Exec(`INSERT INTO bff_transactions VALUES(?,? ,?,?,FALSE,?)`, make([]byte, 32), "not-this-client", make([]byte, 32), []byte("invalid-cross-client-fixture"), time.Now().Unix()+60); err == nil {
			t.Fatal("view check allowed cross-client write")
		} else {
			var denied *driver.MySQLError
			if !errors.As(err, &denied) || denied.Number != 1369 {
				t.Fatal("cross-client insert did not fail through view check")
			}
		}
	}
}

// This profile runs on app_a, without a core network attachment.
func TestDemoNetworkBoundary(t *testing.T) {
	if os.Getenv("SSO_DEMO_NETWORK_TEST") != "true" {
		t.Skip("requires isolated app_a network")
	}
	for _, name := range []string{"CORE_HYDRA_IP", "CORE_IDENTITY_IP"} {
		if net.ParseIP(os.Getenv(name)) == nil {
			t.Fatal("core IP fixture missing")
		}
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 800 * time.Millisecond}
	for _, address := range []string{"http://hydra-login:4445/health/ready", "http://identity-api:8082/token-hook", "http://" + os.Getenv("CORE_HYDRA_IP") + ":4445/health/ready", "http://" + os.Getenv("CORE_IDENTITY_IP") + ":8082/token-hook"} {
		resp, err := client.Get(address)
		if err == nil {
			resp.Body.Close()
			t.Fatal("BFF reached private core service")
		}
	}
	for _, address := range []string{"http://protocol-public:4444/admin/clients", "http://protocol-public:4444/oauth2/auth", "http://protocol-public:4444/.well-known/jwks.json?admin=1"} {
		resp, err := client.Get(address)
		if err != nil {
			t.Fatal("public boundary unavailable")
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatal("public boundary accepted disallowed path")
		}
	}
	resp, err := client.Get("http://protocol-public:4444/.well-known/openid-configuration")
	if err != nil {
		t.Fatal("discovery unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("discovery rejected")
	}
}
