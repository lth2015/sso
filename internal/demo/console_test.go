package demo

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"github.com/lth2015/sso/internal/localruntime"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestConsoleGatewayAllowsOnlyPublicContract(t *testing.T) {
	appPath := "/v1/tenants/0123456789abcdef0123456789abcdef/applications/0123456789abcdef0123456789abcdef"
	if !consoleRouteAllowed("GET", appPath+"/clients") || !consoleRouteAllowed("POST", appPath+"/disable") || consoleRouteAllowed("POST", appPath+"/clients") || consoleRouteAllowed("GET", appPath+"/clients/private-secret") {
		t.Fatal("application gateway method/path boundary mismatch")
	}
	for _, v := range []struct{ method, path string }{{"GET", "/v1/me"}, {"POST", "/v1/tenants"}, {"PUT", "/v1/tenants/" + "0123456789abcdef0123456789abcdef" + "/organizations/0123456789abcdef0123456789abcdef/members/0123456789abcdef0123456789abcdef"}} {
		if !consoleRouteAllowed(v.method, v.path) {
			t.Fatal("documented public operation rejected")
		}
	}
	for _, v := range []struct{ method, path string }{{"GET", "/admin/clients"}, {"GET", "/v1/tenants/../admin"}, {"POST", "/v1/me"}, {"CONNECT", "/v1/me"}, {"GET", "http://identity-api:8082/token-hook"}} {
		if consoleRouteAllowed(v.method, v.path) {
			t.Fatal("gateway exposed a non-contract operation")
		}
	}
	app := &App{Client: "local-console", Origin: "http://console.localhost:3003"}
	_ = app.Handler() // Registering the gateway and SPA must not produce overlapping routes.
	r := httptest.NewRequest("GET", "http://console.localhost:3003/session", nil)
	if _, ok := app.sessionCookieName(r); ok {
		t.Fatal("missing context accepted")
	}
	r.Header.Set("X-Console-Context", "0123456789abcdef0123456789abcdef")
	name, ok := app.sessionCookieName(r)
	if !ok || name != consoleCookiePrefix+"0123456789abcdef0123456789abcdef" {
		t.Fatal("context cookie binding invalid")
	}
	r.Header.Add("X-Console-Context", "other")
	if _, ok = app.sessionCookieName(r); ok {
		t.Fatal("ambiguous context accepted")
	}
}
func TestConsoleRealTokenIsolationAndExpiryFixture(t *testing.T) {
	if os.Getenv("SSO_CONSOLE_TEST") != "true" {
		t.Skip("requires real console browser fixture")
	}
	user, err := store.ParseID(os.Getenv("CONSOLE_TEST_USER"))
	if err != nil {
		t.Fatal("invalid fixture identity")
	}
	db, err := localruntime.Database()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM login_aliases WHERE user_id=? AND normalized_email REGEXP '^browser-[a-f0-9]{32}@example[.]invalid$'`, user).Scan(&count); err != nil || count != 1 {
		t.Fatal("refusing non-fixture operation")
	}
	key, err := hex.DecodeString(os.Getenv("CONSOLE_BFF_KEY"))
	if err != nil {
		t.Fatal("key unavailable")
	}
	app, err := New(context.Background(), store.NewStore(db), "local-console", os.Getenv("CONSOLE_SECRET"), key)
	if err != nil {
		t.Fatal("console provider unavailable")
	}
	foreignKey, err := hex.DecodeString(os.Getenv("DEMO_A_BFF_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := New(context.Background(), store.NewStore(db), "local-demo-a", os.Getenv("DEMO_A_SECRET"), foreignKey)
	if err != nil {
		t.Fatal("foreign provider unavailable")
	}
	rows, err := db.Query(`SELECT token_digest,encrypted_tokens FROM bff_sessions WHERE user_id=? AND client_id='local-console' AND active=TRUE`, user)
	if err != nil {
		t.Fatal(err)
	}
	type entry struct{ digest, cipher []byte }
	entries := []entry{}
	for rows.Next() {
		var v entry
		if err = rows.Scan(&v.digest, &v.cipher); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, v)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(entries) < 2 {
		t.Fatal("two independent console contexts required")
	}
	tenants := map[string]bool{}
	for _, v := range entries {
		var tokens Tokens
		aad := "session:local-console:" + base64.RawURLEncoding.EncodeToString(v.digest)
		if app.open(v.cipher, aad, &tokens) != nil {
			t.Fatal("console state decrypt failed")
		}
		if foreign.open(v.cipher, aad, &Tokens{}) == nil {
			t.Fatal("another application key decrypted console state")
		}
		if _, err = app.verifyID(context.Background(), tokens.ID, tokens.Nonce); err != nil {
			t.Fatal("real console ID token rejected")
		}
		if _, err = app.verifyAccess(context.Background(), tokens.Access, tokens.User); err != nil {
			t.Fatal("real platform access token rejected")
		}
		tenants[tokens.Tenant] = true
		tokens.Expiry = time.Now().Add(-time.Minute)
		cipher, err := app.seal(tokens, aad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`UPDATE bff_sessions SET encrypted_tokens=? WHERE token_digest=? AND client_id='local-console'`, cipher, v.digest); err != nil {
			t.Fatal(err)
		}
	}
	if len(tenants) < 2 {
		t.Fatal("separate tenant grants required")
	}
	// No token/cipher material is written to stdout or a fixture file.
}
