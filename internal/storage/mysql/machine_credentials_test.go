package mysql

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lth2015/sso/internal/adapter/hydra"
)

type machineEngineFixture struct {
	sync.Mutex
	profiles        map[string]hydra.PublicClientProfile
	failAfterCreate bool
	afterCreate     func()
}

func machineEngine(t *testing.T) (*hydra.Client, *machineEngineFixture) {
	t.Helper()
	f := &machineEngineFixture{profiles: map[string]hydra.PublicClientProfile{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/admin/clients/")
		if r.Method == "POST" {
			var v struct {
				hydra.PublicClientProfile
				Secret string `json:"client_secret"`
			}
			if json.NewDecoder(r.Body).Decode(&v) != nil {
				w.WriteHeader(400)
				return
			}
			raw, e := base64.RawURLEncoding.DecodeString(v.Secret)
			if e != nil || len(raw) != 32 {
				w.WriteHeader(400)
				return
			}
			f.Lock()
			f.profiles[v.ID] = v.PublicClientProfile
			fail, callback := f.failAfterCreate, f.afterCreate
			f.Unlock()
			if callback != nil {
				callback()
			}
			if fail {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(v.PublicClientProfile)
			return
		}
		f.Lock()
		defer f.Unlock()
		p, exists := f.profiles[id]
		switch r.Method {
		case "GET":
			if !exists {
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(p)
		case "DELETE":
			delete(f.profiles, id)
			w.WriteHeader(204)
		default:
			w.WriteHeader(405)
		}
	}))
	t.Cleanup(server.Close)
	admin := hydra.New()
	admin.Base = server.URL
	return admin, f
}
func machineFixture(t *testing.T) (Store, Bootstrap, TenantStore, ID, ID, *hydra.Client, *machineEngineFixture) {
	t.Helper()
	s := testStore(t)
	b := fixture(t, s)
	repo := s.ForTenant(b.Tenant, b.Membership)
	app, e := repo.APICreateApplication(context.Background(), "Machine fixture")
	if e != nil {
		t.Fatal(e)
	}
	appID, _ := ParseID(app.ID)
	sp, e := repo.APICreatePrincipal(context.Background(), appID, "Worker")
	if e != nil {
		t.Fatal(e)
	}
	principal, _ := ParseID(sp.ID)
	admin, remote := machineEngine(t)
	return s, b, repo, appID, principal, admin, remote
}
func machineCheck(client string, app ID) MachineTokenCheck {
	return MachineTokenCheck{Client: client, Requested: []string{MachineScope}, Granted: []string{MachineScope}, Audience: []string{MachineAudience(app)}}
}
func machineClaims(t *testing.T, s Store, credential MachineCredentialSummary, app ID) MachineAuthorization {
	t.Helper()
	claims, e := s.CheckMachineToken(context.Background(), machineCheck(credential.AccessKey, app))
	if e != nil {
		t.Fatal("machine authority fixture failed")
	}
	claims.Expires = time.Now().Add(5 * time.Minute).Unix()
	return claims
}
func TestMachineCredentialOneTimeMetadataAndPrincipalIsolation(t *testing.T) {
	s, b, repo, app, principal, admin, _ := machineFixture(t)
	ctx := context.Background()
	created, e := repo.CreateMachineCredential(ctx, app, principal, "Primary", 90, admin)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := base64.RawURLEncoding.DecodeString(created.Secret)
	if e != nil || len(raw) != 32 || created.Credential.State != "active" || created.Credential.Version != 2 {
		t.Fatal("one-time credential result invalid")
	}
	credential, _ := ParseID(created.Credential.ID)
	metadata, e := repo.APIMachineCredential(ctx, app, principal, credential)
	if e != nil {
		t.Fatal(e)
	}
	body, e := json.Marshal(metadata)
	if e != nil || strings.Contains(string(body), created.Secret) || strings.Contains(string(body), "secret_key") || strings.Contains(string(body), "security_version") {
		t.Fatal("metadata leaked secret/internal authority")
	}
	if count(t, s, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='sso_business' AND TABLE_NAME='machine_credentials' AND COLUMN_NAME LIKE '%secret%'`) != 0 {
		t.Fatal("recoverable secret column introduced")
	}
	other := fixture(t, s)
	if _, e = s.ForTenant(other.Tenant, other.Membership).APIMachineCredential(ctx, app, principal, credential); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-tenant credential metadata disclosed")
	}
	if _, e = s.ForTenant(b.Tenant, other.Membership).APIPrincipals(ctx, app, 2, ID{}); !errors.Is(e, ErrDenied) {
		t.Fatal("foreign actor managed principals")
	}
	if _, e = s.OAuthClient(ctx, created.Credential.AccessKey); !errors.Is(e, ErrDenied) {
		t.Fatal("machine key entered user login registry")
	}
	if count(t, s, `SELECT authz_version FROM tenants WHERE id=?`, b.Tenant) != 1 {
		t.Fatal("machine creation revoked unrelated user grants")
	}
	claims := machineClaims(t, s, metadata, app)
	if e = s.CheckMachineResource(ctx, claims); e != nil {
		t.Fatal("active credential resource denied")
	}
	events, e := repo.APIMachineEvents(ctx, 2, ID{})
	if e != nil || len(events) != 1 || events[0].Principal != principal.String() {
		t.Fatal("machine audit actor not principal")
	}
	if count(t, s, `SELECT COUNT(*) FROM machine_outbox_events WHERE tenant_id=?`, b.Tenant) != 1 {
		t.Fatal("machine issuance outbox missing")
	}
}
func TestMachineRotationOverlapExpiryPauseAndNoResurrection(t *testing.T) {
	s, _, repo, app, principal, admin, _ := machineFixture(t)
	ctx := context.Background()
	first, e := repo.CreateMachineCredential(ctx, app, principal, "First", 90, admin)
	if e != nil {
		t.Fatal(e)
	}
	firstID, _ := ParseID(first.Credential.ID)
	firstClaims := machineClaims(t, s, first.Credential, app)
	second, e := repo.CreateMachineCredential(ctx, app, principal, "Second", 90, admin)
	if e != nil {
		t.Fatal(e)
	}
	secondID, _ := ParseID(second.Credential.ID)
	secondClaims := machineClaims(t, s, second.Credential, app)
	changed, e := repo.APIMachineCredential(ctx, app, principal, firstID)
	if e != nil || changed.Version != 3 || changed.securityVersion != first.Credential.securityVersion || changed.Expires > time.Now().Add(24*time.Hour).Unix() {
		t.Fatal("rotation did not bound overlap with independent API revision")
	}
	if e = s.CheckMachineResource(ctx, firstClaims); e != nil {
		t.Fatal("overlap needlessly invalidated old cached token")
	}
	if _, e = repo.CreateMachineCredential(ctx, app, principal, "Third", 90, admin); !errors.Is(e, ErrCredentialLimit) {
		t.Fatal("two-live-key limit bypassed")
	}
	if _, e = repo.RevokeMachineCredential(ctx, app, principal, firstID, 2); !errors.Is(e, ErrConflict) {
		t.Fatal("old expiry ETag accepted")
	}
	if _, e = repo.RevokeMachineCredential(ctx, app, principal, firstID, 3); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckMachineResource(ctx, firstClaims); !errors.Is(e, ErrDenied) {
		t.Fatal("revoked credential token survived")
	}
	if _, e = s.CheckMachineToken(ctx, machineCheck(first.Credential.AccessKey, app)); !errors.Is(e, ErrDenied) {
		t.Fatal("revoked key issued token")
	}
	if _, e = repo.SetPrincipalEnabled(ctx, app, principal, false, 1); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckMachineResource(ctx, secondClaims); !errors.Is(e, ErrDenied) {
		t.Fatal("paused SP token survived")
	}
	if _, e = s.CheckMachineToken(ctx, machineCheck(second.Credential.AccessKey, app)); !errors.Is(e, ErrDenied) {
		t.Fatal("paused SP issued token")
	}
	if _, e = repo.SetPrincipalEnabled(ctx, app, principal, true, 2); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckMachineResource(ctx, secondClaims); !errors.Is(e, ErrDenied) {
		t.Fatal("SP restore resurrected old token")
	}
	restored := machineClaims(t, s, second.Credential, app)
	if e = s.CheckMachineResource(ctx, restored); e != nil {
		t.Fatal("restored SP fresh token rejected")
	}
	if _, e = repo.SetApplicationEnabled(ctx, app, false, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CheckMachineToken(ctx, machineCheck(second.Credential.AccessKey, app)); !errors.Is(e, ErrDenied) {
		t.Fatal("disabled app issued machine token")
	}
	if _, e = repo.SetApplicationEnabled(ctx, app, true, 2); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckMachineResource(ctx, restored); !errors.Is(e, ErrDenied) {
		t.Fatal("app restore resurrected old token")
	}
	fresh := machineClaims(t, s, second.Credential, app)
	if _, e = s.db.Exec(`UPDATE machine_credentials SET expires_at=UNIX_TIMESTAMP()-1 WHERE id=?`, secondID); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckMachineResource(ctx, fresh); !errors.Is(e, ErrDenied) {
		t.Fatal("key expiry ignored by resource")
	}
	if _, e = s.CheckMachineToken(ctx, machineCheck(second.Credential.AccessKey, app)); !errors.Is(e, ErrDenied) {
		t.Fatal("expired key issued token")
	}
	expired, e := repo.APIMachineCredential(ctx, app, principal, secondID)
	if e != nil || expired.State != "expired" || expired.Active {
		t.Fatal("expiry metadata incorrect")
	}
}
func TestMachineCredentialLostProtocolResponseNeverRecoversSecretOrShortensOldKey(t *testing.T) {
	_, _, repo, app, principal, admin, remote := machineFixture(t)
	ctx := context.Background()
	first, e := repo.CreateMachineCredential(ctx, app, principal, "First", 90, admin)
	if e != nil {
		t.Fatal(e)
	}
	firstID, _ := ParseID(first.Credential.ID)
	remote.Lock()
	remote.failAfterCreate = true
	remote.Unlock()
	lost, e := repo.CreateMachineCredential(ctx, app, principal, "Lost response", 90, admin)
	if e == nil || lost.Secret != "" {
		t.Fatal("failed creation returned secret")
	}
	keys, e := repo.APIMachineCredentials(ctx, app, principal, 10, ID{})
	if e != nil || len(keys) != 2 {
		t.Fatal("pending key not discoverable for revocation")
	}
	var pending MachineCredentialSummary
	for _, key := range keys {
		if key.State == "pending" {
			pending = key
		}
	}
	if pending.ID == "" {
		t.Fatal("lost response activated credential")
	}
	pendingID, _ := ParseID(pending.ID)
	old, e := repo.APIMachineCredential(ctx, app, principal, firstID)
	if e != nil || old.Version != 2 || old.Expires != first.Credential.Expires {
		t.Fatal("failed rotation changed old key")
	}
	if _, e = repo.activateMachineCredential(ctx, app, principal, pendingID, pending); !errors.Is(e, ErrCredentialState) {
		t.Fatal("metadata-only pending state could reactivate")
	}
	if _, _, e = repo.ReconcileManagedClient(ctx, app, pendingID, 1, admin); !errors.Is(e, ErrNotFound) {
		t.Fatal("public reconciliation adopted machine key")
	}
	if _, e = repo.CleanupMachineCredential(ctx, app, principal, pendingID, 1, admin); !errors.Is(e, ErrCredentialState) {
		t.Fatal("unrevoked key cleaned up")
	}
	if _, e = repo.RevokeMachineCredential(ctx, app, principal, pendingID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.CleanupMachineCredential(ctx, app, principal, pendingID, 2, admin); e != nil {
		t.Fatal(e)
	}
	remote.Lock()
	_, exists := remote.profiles[pending.AccessKey]
	remote.Unlock()
	if exists {
		t.Fatal("revoked pending protocol object retained")
	}
}
func TestMachineCreationCannotActivateAcrossPrincipalPauseRestore(t *testing.T) {
	_, _, repo, app, principal, admin, remote := machineFixture(t)
	ctx := context.Background()
	callbackResult := make(chan error, 1)
	remote.Lock()
	remote.afterCreate = func() {
		if _, e := repo.SetPrincipalEnabled(ctx, app, principal, false, 1); e != nil {
			callbackResult <- e
			return
		}
		_, e := repo.SetPrincipalEnabled(ctx, app, principal, true, 2)
		callbackResult <- e
	}
	remote.Unlock()
	result, e := repo.CreateMachineCredential(ctx, app, principal, "Racing registration", 90, admin)
	select {
	case callbackError := <-callbackResult:
		if callbackError != nil {
			t.Fatal("pause/restore fixture failed")
		}
	case <-time.After(time.Second):
		t.Fatal("pause/restore callback not reached")
	}
	if !errors.Is(e, ErrCredentialState) || result.Secret != "" {
		t.Fatal("stale pending registration activated after SP restore")
	}
	keys, e := repo.APIMachineCredentials(ctx, app, principal, 5, ID{})
	if e != nil || len(keys) != 1 || keys[0].State != "pending" || keys[0].Active {
		t.Fatal("racing key not left closed")
	}
}
func TestMachineTokenScopesAudienceUnknownAndAuditReadPermission(t *testing.T) {
	s, b, repo, app, principal, admin, _ := machineFixture(t)
	ctx := context.Background()
	created, e := repo.CreateMachineCredential(ctx, app, principal, "Primary", 90, admin)
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*MachineTokenCheck){func(v *MachineTokenCheck) { v.Granted = []string{"platform:write"} }, func(v *MachineTokenCheck) { v.Requested = []string{MachineScope, "platform:read"} }, func(v *MachineTokenCheck) { v.Audience = []string{"https://api.sso.local"} }, func(v *MachineTokenCheck) {
		v.PayloadAudience = []string{"urn:sso:application:" + strings.Repeat("f", 32)}
	}, func(v *MachineTokenCheck) { v.PayloadScopes = []string{"platform:write"} }, func(v *MachineTokenCheck) { v.Client = "local-console" }} {
		value := machineCheck(created.Credential.AccessKey, app)
		change(&value)
		if _, e = s.CheckMachineToken(ctx, value); !errors.Is(e, ErrDenied) {
			t.Fatal("machine scope/audience/client boundary bypassed")
		}
	}
	unknown, _ := NewID()
	if _, e = s.CheckMachineToken(ctx, machineCheck("ak_"+unknown.String(), app)); !errors.Is(e, ErrDenied) {
		t.Fatal("unknown client issued machine token")
	}
	if count(t, s, `SELECT COUNT(*) FROM machine_events WHERE tenant_id=?`, b.Tenant) != 0 {
		t.Fatal("denial recorded as successful issuance")
	}
	claims := machineClaims(t, s, created.Credential, app)
	claims.Application = unknown
	if e = s.CheckMachineResource(ctx, claims); !errors.Is(e, ErrDenied) {
		t.Fatal("cross-application resource accepted")
	}
	otherUser := newUser(t, s)
	member, _ := NewID()
	if _, e = s.db.Exec(`INSERT INTO memberships(id,tenant_id,user_id) VALUES(?,?,?)`, member, b.Tenant, otherUser); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ForTenant(b.Tenant, member).APIMachineEvents(ctx, 5, ID{}); !errors.Is(e, ErrDenied) {
		t.Fatal("member read machine audit")
	}
	var version int64
	if e = s.db.QueryRow(`SELECT authz_version FROM tenants WHERE id=?`, b.Tenant).Scan(&version); e != nil {
		t.Fatal("audit role fixture unavailable")
	}
	role, e := repo.APICreateRole(ctx, "machine_auditor", []RolePolicy{{Permission: "audit.read", Effect: "allow"}}, version)
	if e != nil {
		t.Fatal("audit role creation failed")
	}
	if e = repo.APIBindRole(ctx, member, role, version+1, false); e != nil {
		t.Fatal("audit role binding failed")
	}
	delegated := s.ForTenant(b.Tenant, member)
	events, e := delegated.APIMachineEvents(ctx, 5, ID{})
	if e != nil || len(events) != 1 {
		t.Fatal("delegated audit reader cannot read machine events")
	}
	if _, e = delegated.APIMachineCredentials(ctx, app, principal, 5, ID{}); !errors.Is(e, ErrDenied) {
		t.Fatal("audit permission granted credential management")
	}
}

func TestMachineCredentialConcurrentTwoKeyQuota(t *testing.T) {
	s, b, repo, app, principal, admin, _ := machineFixture(t)
	results := make(chan error, 3)
	start := make(chan struct{})
	for range 3 {
		go func() {
			<-start
			_, err := repo.CreateMachineCredential(context.Background(), app, principal, "Concurrent key", 90, admin)
			results <- err
		}()
	}
	close(start)
	successes, limited := 0, 0
	for range 3 {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, ErrCredentialLimit) {
			limited++
		} else {
			t.Fatal("concurrent credential creation failed outside quota bound")
		}
	}
	if successes != 2 || limited != 1 {
		t.Fatal("concurrent creation escaped the two-key quota")
	}
	var live int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM machine_credentials WHERE tenant_id=? AND application_id=? AND service_principal_id=? AND state='active'`, b.Tenant, app, principal).Scan(&live); err != nil || live != 2 {
		t.Fatal("primary active credential count differs from successful concurrent creations")
	}
}

func TestMachinePrincipalContinuesAfterCreatorOwnershipTransfer(t *testing.T) {
	s, b, repo, app, principal, admin, _ := machineFixture(t)
	ctx := context.Background()
	created, e := repo.CreateMachineCredential(ctx, app, principal, "Continuity key", 90, admin)
	if e != nil {
		t.Fatal("continuity credential creation failed")
	}
	old := machineClaims(t, s, created.Credential, app)
	successorUser := newUser(t, s)
	successor, _ := NewID()
	if _, e = s.db.Exec(`INSERT INTO memberships(id,tenant_id,user_id) VALUES(?,?,?)`, successor, b.Tenant, successorUser); e != nil {
		t.Fatal("successor fixture failed")
	}
	var owner ID
	if e = s.db.QueryRow(`SELECT id FROM roles WHERE tenant_id=? AND code='owner'`, b.Tenant).Scan(&owner); e != nil {
		t.Fatal("owner role unavailable")
	}
	version := func() int64 {
		var v int64
		if e := s.db.QueryRow(`SELECT authz_version FROM tenants WHERE id=?`, b.Tenant).Scan(&v); e != nil {
			t.Fatal("tenant version unavailable")
		}
		return v
	}
	if e = repo.APIBindRole(ctx, successor, owner, version(), false); e != nil {
		t.Fatal("successor grant failed")
	}
	successorRepo := s.ForTenant(b.Tenant, successor)
	if e = successorRepo.APIBindRole(ctx, b.Membership, owner, version(), true); e != nil {
		t.Fatal("creator role transfer failed")
	}
	if e = successorRepo.SuspendMember(ctx, b.Membership, version()); e != nil {
		t.Fatal("creator membership suspension failed")
	}
	if _, e = repo.APIPrincipal(ctx, app, principal); !errors.Is(e, ErrDenied) {
		t.Fatal("former owner retained management rights")
	}
	if e = s.CheckMachineResource(ctx, old); !errors.Is(e, ErrDenied) {
		t.Fatal("tenant revision change failed to retire old JWT")
	}
	fresh := machineClaims(t, s, created.Credential, app)
	if e = s.CheckMachineResource(ctx, fresh); e != nil {
		t.Fatal("machine authority incorrectly depends on creator membership")
	}
	key, _ := ParseID(created.Credential.ID)
	metadata, e := successorRepo.APIMachineCredential(ctx, app, principal, key)
	if e != nil {
		t.Fatal("successor cannot manage tenant-owned credential")
	}
	if _, e = successorRepo.RevokeMachineCredential(ctx, app, principal, key, metadata.Version); e != nil {
		t.Fatal("successor cannot revoke tenant-owned credential")
	}
	if e = s.CheckMachineResource(ctx, fresh); !errors.Is(e, ErrDenied) {
		t.Fatal("successor revocation failed")
	}
}
