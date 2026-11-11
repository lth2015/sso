package platformapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lth2015/sso/internal/localruntime"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"os"
	"regexp"
	"testing"
	"time"
)

func TestAPIRejectsAmbiguousJSON(t *testing.T) {
	for _, value := range []string{`null`, `{} {}`, `{"email":"first","email":"second"}`, `{"nested":{"x":1}}`, `{"list":[]}`, `{broken}`} {
		if strictObject([]byte(value)) {
			t.Fatal("ambiguous input accepted")
		}
	}
	if !strictObject([]byte(`{"dry_run":false,"name":"组织"}`)) {
		t.Fatal("valid scalar object rejected")
	}
}
func TestAPIRevocationBetweenAuthenticationAndWrite(t *testing.T) {
	if os.Getenv("SSO_API_TEST") != "true" {
		t.Skip("requires the real OAuth fixture from tests/api/browser.cjs")
	}
	var fixture struct {
		Access string `json:"access_token"`
	}
	if json.NewDecoder(os.Stdin).Decode(&fixture) != nil {
		t.Fatal("fixture input unavailable")
	}
	db, err := localruntime.Database()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := store.NewStore(db)
	auth, err := NewAuthenticator(context.Background(), repo)
	if err != nil {
		t.Fatal("issuer unavailable")
	}
	p, err := auth.Authenticate(context.Background(), fixture.Access)
	if err != nil {
		t.Fatal("real access fixture not authenticated")
	}
	a := p.Authority
	a.Recent = true
	var email string
	if err = db.QueryRow(`SELECT normalized_email FROM login_aliases WHERE user_id=?`, a.User).Scan(&email); err != nil || !regexp.MustCompile(`^browser-[a-f0-9]{32}@example\.invalid$`).MatchString(email) {
		t.Fatal("refusing non-fixture mutation")
	}
	verifySecurityEventBoundary(t, db, repo, a)
	tenant, err := repo.APITenant(context.Background(), a)
	if err != nil || !tenant.Owner {
		t.Fatal("owner fixture required")
	}
	root, err := store.ParseID(tenant.Root)
	if err != nil {
		t.Fatal(err)
	}
	expired := a
	expired.Expires = time.Now().Unix() - 1
	if _, err = repo.ForAuthorization(expired).AddOrganization(context.Background(), root, "must-not-exist", tenant.OrgVersion); !errors.Is(err, store.ErrDenied) {
		t.Fatal("expired token was not rechecked in the write transaction")
	}
	if _, err = db.Exec(`UPDATE users SET security_version=security_version+1 WHERE id=?`, a.User); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ForAuthorization(a).AddOrganization(context.Background(), root, "must-not-exist", tenant.OrgVersion); !errors.Is(err, store.ErrDenied) {
		t.Fatal("revocation between authentication and mutation was ignored")
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM org_nodes WHERE tenant_id=? AND name='must-not-exist'`, a.Tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("unauthorized mutation escaped rollback")
	}
	// Global writes must also recheck authority, not just owner-scoped methods.
	if _, err = repo.APIBootstrap(context.Background(), a, "not-created", "must-not-exist"); !errors.Is(err, store.ErrDenied) {
		t.Fatal("global mutation accepted revoked authority")
	}
}

func TestRBACNestedInputRejectsDuplicateKeysAndDepth(t *testing.T) {
	for _, body := range []string{`{"policies":[{"permission":"audit.read","effect":"allow","effect":"deny"}]}`, `{"policies":[],"policies":[]}`, `{"x":[[[[[[[[]]]]]]]]}`, `[]`, `{} {}`} {
		if strictNestedObject([]byte(body)) {
			t.Fatal("ambiguous/deep role request accepted")
		}
	}
	if !strictNestedObject([]byte(`{"policies":[{"permission":"audit.read","effect":"allow"}]}`)) {
		t.Fatal("typed role policy container rejected")
	}
}
