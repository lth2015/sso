package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lth2015/sso/internal/adapter/hydra"
)

type observationTransport struct {
	base       http.RoundTripper
	requests   atomic.Int32
	writeCalls atomic.Int32
}

func (t *observationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.requests.Add(1)
	if r.Method != "GET" {
		t.writeCalls.Add(1)
	}
	return t.base.RoundTrip(r)
}

func observationCounts(t *testing.T, s Store, b Bootstrap) [6]int {
	t.Helper()
	return [6]int{int(rbacVersion(t, s, b)), count(t, s, `SELECT org_version FROM tenants WHERE id=?`, b.Tenant),
		count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=?`, b.Tenant), count(t, s, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=?`, b.Tenant),
		count(t, s, `SELECT COUNT(*) FROM machine_events WHERE tenant_id=?`, b.Tenant), count(t, s, `SELECT COUNT(*) FROM machine_outbox_events WHERE tenant_id=?`, b.Tenant)}
}

func TestProtocolObservationBothResourcesAreReadOnlyAndNeverActivatePending(t *testing.T) {
	s, b, r, app, principal, admin, remote := machineFixture(t)
	ctx := context.Background()
	client, err := r.APICreateManagedClient(ctx, app, "Observation desktop", "http://127.0.0.1:9876/callback")
	if err != nil {
		t.Fatal("public observation fixture failed")
	}
	cid, _ := ParseID(client.ID)
	created, err := r.CreateMachineCredential(ctx, app, principal, "Observation worker", 30, admin)
	if err != nil {
		t.Fatal("machine observation fixture failed")
	}
	key, _ := ParseID(created.Credential.ID)
	transport := &observationTransport{base: admin.HTTP.Transport}
	admin.HTTP.Transport = transport
	before := observationCounts(t, s, b)
	absent, err := r.APIManagedClientProtocol(ctx, app, cid, admin)
	if err != nil || absent.ProfileState != "absent" || absent.LocalState != "pending" || absent.Revision != 1 || absent.Principal != nil || len(absent.Differences) != 0 {
		t.Fatal("missing protocol observation failed")
	}
	profile := hydra.ManagedPublicClient(client.ID, client.Name, client.Redirect, b.Tenant.String(), app.String())
	remote.Lock()
	remote.profiles[client.ID] = profile
	remote.Unlock()
	matching, err := r.APIManagedClientProtocol(ctx, app, cid, admin)
	if err != nil || matching.ProfileState != "matching" || matching.LocalState != "pending" || matching.Revision != 1 || matching.Tenant != b.Tenant.String() || matching.Application != app.String() || matching.Resource != client.ID || matching.AuthorityCheckedAt.Before(matching.ObservedAt) {
		t.Fatal("matching observation changed pending eligibility or namespace")
	}
	const private = "private-upstream-value-never-exported"
	profile.Scopes = private
	remote.Lock()
	remote.profiles[client.ID] = profile
	remote.Unlock()
	conflict, err := r.APIManagedClientProtocol(ctx, app, cid, admin)
	if err != nil || conflict.ProfileState != "conflict" || len(conflict.Differences) != 1 || conflict.Differences[0] != "scope" {
		t.Fatal("controlled profile difference failed")
	}
	data, _ := json.Marshal(conflict)
	if strings.Contains(string(data), private) {
		t.Fatal("actual differing protocol value escaped")
	}
	machine, err := r.APIMachineCredentialProtocol(ctx, app, principal, key, admin)
	if err != nil || machine.ProfileState != "matching" || machine.Kind != "machine_credential" || machine.Principal == nil || *machine.Principal != principal.String() || machine.Resource != key.String() {
		t.Fatal("machine observation namespace failed")
	}
	data, _ = json.Marshal(machine)
	if strings.Contains(string(data), created.Secret) || strings.Contains(string(data), created.Credential.AccessKey) {
		t.Fatal("machine observation exposed private or unnecessary engine identifiers")
	}
	remote.Lock()
	p := remote.profiles[created.Credential.AccessKey]
	p.Metadata["unexpected"] = private
	remote.profiles[created.Credential.AccessKey] = p
	remote.Unlock()
	machine, err = r.APIMachineCredentialProtocol(ctx, app, principal, key, admin)
	if err != nil || machine.ProfileState != "conflict" || len(machine.Differences) != 1 || machine.Differences[0] != "metadata" {
		t.Fatal("machine metadata drift was not controlled")
	}
	data, _ = json.Marshal(machine)
	if strings.Contains(string(data), private) {
		t.Fatal("upstream metadata values escaped")
	}
	remote.Lock()
	delete(remote.profiles, created.Credential.AccessKey)
	remote.Unlock()
	machine, err = r.APIMachineCredentialProtocol(ctx, app, principal, key, admin)
	if err != nil || machine.ProfileState != "absent" || machine.LocalState != "active" {
		t.Fatal("absent protocol object changed primary credential state")
	}
	if observationCounts(t, s, b) != before || transport.writeCalls.Load() != 0 {
		t.Fatal("read-only observation mutated authority/audit/outbox or the protocol")
	}
	current, err := r.APIManagedClient(ctx, app, cid)
	if err != nil || current.State != "pending" || current.Active || current.Version != 1 {
		t.Fatal("inspection activated an unapproved public client")
	}
}

func TestProtocolObservationRejectsForeignOrDelegatedResourcesBeforeAdminRequest(t *testing.T) {
	s, b, r, app, principal, admin, _ := machineFixture(t)
	ctx := context.Background()
	client, err := r.APICreateManagedClient(ctx, app, "Observation desktop", "https://app.example.invalid/callback")
	if err != nil {
		t.Fatal("observation fixture failed")
	}
	cid, _ := ParseID(client.ID)
	transport := &observationTransport{base: admin.HTTP.Transport}
	admin.HTTP.Transport = transport
	guest := rbacMember(t, s, b)
	role := rbacRole(t, s, b, "inspection_delegate", []RolePolicy{{"audit.read", "allow"}, {"invitations.manage", "allow"}})
	rbacBind(t, s, b, guest, role, false)
	if v, e := s.ForTenant(b.Tenant, guest).APIManagedClientProtocol(ctx, app, cid, admin); !errors.Is(e, ErrDenied) || v.Resource != "" {
		t.Fatal("delegation granted protocol visibility")
	}
	if v, e := s.ForTenant(b.Tenant, guest).APIMachineCredentialProtocol(ctx, app, principal, cid, admin); !errors.Is(e, ErrDenied) || v.Resource != "" {
		t.Fatal("delegation granted credential protocol visibility")
	}
	foreign := fixture(t, s)
	if v, e := s.ForTenant(foreign.Tenant, foreign.Membership).APIManagedClientProtocol(ctx, app, cid, admin); !errors.Is(e, ErrNotFound) || v.Resource != "" {
		t.Fatal("foreign protocol resource exposed")
	}
	shared := applicationFixture(t, s, b.Tenant)
	if v, e := r.APIManagedClientProtocol(ctx, shared, cid, admin); !errors.Is(e, ErrApplicationUnmanaged) || v.Resource != "" {
		t.Fatal("shared application protocol registry exposed")
	}
	if transport.requests.Load() != 0 {
		t.Fatal("unauthorized resource caused a private Admin request")
	}
}

func TestProtocolObservationDependencyFailureAndInFlightCancellationReturnNoPartialResult(t *testing.T) {
	s, b, r, app, _, _, _ := machineFixture(t)
	client, err := r.APICreateManagedClient(context.Background(), app, "Observation desktop", "https://app.example.invalid/callback")
	if err != nil {
		t.Fatal("observation fixture failed")
	}
	cid, _ := ParseID(client.ID)
	const private = "private-unavailable-response-never-returned"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503); w.Write([]byte(private)) }))
	defer server.Close()
	admin := hydra.New()
	admin.Base = server.URL
	before := observationCounts(t, s, b)
	result, err := r.APIManagedClientProtocol(context.Background(), app, cid, admin)
	if err == nil || strings.Contains(err.Error(), private) || result.Resource != "" || result.Differences != nil {
		t.Fatal("failure exposed a partial observation or upstream body")
	}
	entered := make(chan struct{})
	cancelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer cancelServer.Close()
	admin = hydra.New()
	admin.Base = cancelServer.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() { v, e := r.APIManagedClientProtocol(ctx, app, cid, admin); done <- e != nil && v.Resource == "" }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("observation did not reach the bounded Admin request")
	}
	cancel()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("cancelled observation returned an identity")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled observation did not finish")
	}
	if observationCounts(t, s, b) != before {
		t.Fatal("failed observation mutated authority/audit/outbox")
	}
}

func TestProtocolObservationRechecksAuthorityAndResourceAfterUnlockedRPC(t *testing.T) {
	s, b, r, app, _, _, _ := machineFixture(t)
	ctx := context.Background()
	client, err := r.APICreateManagedClient(ctx, app, "Observation desktop", "https://app.example.invalid/callback")
	if err != nil {
		t.Fatal("observation fixture failed")
	}
	cid, _ := ParseID(client.ID)
	profile := hydra.ManagedPublicClient(client.ID, client.Name, client.Redirect, b.Tenant.String(), app.String())
	callbackErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, e := r.RevokeManagedClient(c, app, cid, 1)
		callbackErrors <- e
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(profile)
	}))
	defer server.Close()
	admin := hydra.New()
	admin.Base = server.URL
	result, err := r.APIManagedClientProtocol(ctx, app, cid, admin)
	if e := <-callbackErrors; e != nil {
		t.Fatal("observation held the tenant lock while waiting for Admin")
	}
	if !errors.Is(err, ErrObservationChanged) || result.Resource != "" {
		t.Fatal("changed resource produced a stale observation")
	}
	loss := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, e := s.db.Exec(`UPDATE users u JOIN memberships m ON m.user_id=u.id SET u.status='suspended' WHERE m.tenant_id=? AND m.id=?`, b.Tenant, b.Membership)
		callbackErrors <- e
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(profile)
	}))
	defer loss.Close()
	admin = hydra.New()
	admin.Base = loss.URL
	result, err = r.APIManagedClientProtocol(ctx, app, cid, admin)
	if e := <-callbackErrors; e != nil {
		t.Fatal("authority-loss fixture failed")
	}
	if _, e := s.db.Exec(`UPDATE users u JOIN memberships m ON m.user_id=u.id SET u.status='active' WHERE m.tenant_id=? AND m.id=?`, b.Tenant, b.Membership); e != nil {
		t.Fatal("authority fixture restoration failed")
	}
	if !errors.Is(err, ErrDenied) || result.Resource != "" {
		t.Fatal("owner loss during Admin request exposed an observation")
	}
}
