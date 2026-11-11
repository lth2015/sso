package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lth2015/sso/internal/localruntime"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

const initialPassword = "First strong local passphrase 123!"
const updatedPassword = "Second strong local passphrase 456!"

type fixture struct {
	service *Service
	db      *sql.DB
	email   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if os.Getenv("SSO_ACCOUNT_TEST") != "true" {
		t.Skip("requires real local MySQL: scripts/m2 test")
	}
	db, err := localruntime.Database()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal(err)
	}
	service, err := New(store.NewStore(db), key)
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	email := "account-" + id.String() + "+tag@example.invalid"
	f := &fixture{service, db, email}
	t.Cleanup(func() { f.cleanup(t) })
	return f
}
func (f *fixture) cleanup(t *testing.T) {
	t.Helper()
	exec := func(q string, args ...any) {
		if _, err := f.db.Exec(q, args...); err != nil {
			t.Error("account fixture cleanup failed")
		}
	}
	exec(`DELETE n FROM account_notifications n JOIN account_challenges c ON c.id=n.challenge_id WHERE c.normalized_email=?`, f.email)
	exec(`DELETE e FROM identity_events e JOIN account_challenges c ON c.id=e.target_id WHERE c.normalized_email=?`, f.email)
	exec(`DELETE FROM account_challenges WHERE normalized_email=?`, f.email)
	var user store.ID
	err := f.db.QueryRow(`SELECT user_id FROM login_aliases WHERE normalized_email=?`, f.email).Scan(&user)
	if err == sql.ErrNoRows {
		return
	}
	if err != nil {
		t.Error(err)
		return
	}
	rows, err := f.db.Query(`SELECT tenant_id FROM memberships WHERE user_id=?`, user)
	if err != nil {
		t.Error(err)
		return
	}
	tenants := []store.ID{}
	for rows.Next() {
		var id store.ID
		if err = rows.Scan(&id); err != nil {
			t.Error(err)
		}
		tenants = append(tenants, id)
	}
	rows.Close()
	for _, table := range []string{"security_actions", "protocol_logout_jobs", "bff_sessions", "business_grants", "login_interactions", "sso_sessions", "mfa_recovery_codes", "mfa_factors"} {
		exec("DELETE FROM "+table+" WHERE user_id=?", user)
	}
	for _, tenant := range tenants {
		for _, table := range []string{"role_binding_changes", "role_policies", "outbox_events", "audit_events", "tenant_applications", "role_bindings", "roles", "org_closure", "tenant_roots", "org_nodes", "memberships"} {
			exec("DELETE FROM "+table+" WHERE tenant_id=?", tenant)
		}
		exec(`DELETE FROM tenants WHERE id=?`, tenant)
	}
	for _, table := range []string{"identity_events", "password_credentials", "account_profiles", "login_aliases"} {
		exec("DELETE FROM "+table+" WHERE user_id=?", user)
	}
	exec(`DELETE FROM users WHERE id=?`, user)
}
func (f *fixture) request(t *testing.T, purpose string) Message {
	t.Helper()
	if err := f.service.Request(context.Background(), f.email, purpose); err != nil {
		t.Fatal(err)
	}
	var n store.Notification
	err := f.db.QueryRow(`SELECT n.id,n.recipient,n.purpose,n.encrypted_payload,n.expires_at FROM account_notifications n JOIN account_challenges c ON c.id=n.challenge_id WHERE n.recipient=? AND n.purpose=? ORDER BY c.created_at DESC,c.id DESC LIMIT 1`, f.email, purpose).Scan(&n.ID, &n.Recipient, &n.Purpose, &n.Ciphertext, &n.Expires)
	if err != nil {
		t.Fatal("expected notification missing")
	}
	body, err := Decrypt(f.service.aead, n)
	if err != nil {
		t.Fatal(err)
	}
	var message Message
	if json.Unmarshal(body, &message) != nil {
		t.Fatal("invalid notification format")
	}
	if strings.Contains(string(n.Ciphertext), message.Token) {
		t.Fatal("plaintext proof stored in queue")
	}
	return message
}
func (f *fixture) registered(t *testing.T) store.ID {
	t.Helper()
	message := f.request(t, "register")
	if err := f.service.Complete(context.Background(), message.Token, initialPassword, "register"); err != nil {
		t.Fatal(err)
	}
	user, err := f.service.CheckPassword(context.Background(), f.email, initialPassword)
	if err != nil {
		t.Fatal(err)
	}
	return user
}
func TestRegistrationProofCreatesAtomicPersonalSpace(t *testing.T) {
	f := newFixture(t)
	message := f.request(t, "register")
	ctx := context.Background()
	if _, err := f.service.CheckPassword(ctx, f.email, initialPassword); !errors.Is(err, store.ErrDenied) {
		t.Fatal("unverified identity authenticated")
	}
	if err := f.service.Complete(ctx, message.Token, initialPassword, "register"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Complete(ctx, message.Token, initialPassword, "register"); !errors.Is(err, store.ErrChallenge) {
		t.Fatal("registration proof reused")
	}
	user, err := f.service.CheckPassword(ctx, f.email, initialPassword)
	if err != nil {
		t.Fatal(err)
	}
	var hash string
	var tenants, owners int
	if err = f.db.QueryRow(`SELECT password_hash FROM password_credentials WHERE user_id=?`, user).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=1$") || strings.Contains(hash, initialPassword) {
		t.Fatal("invalid password storage")
	}
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM memberships WHERE user_id=? AND status='active'`, user).Scan(&tenants); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM role_bindings b JOIN memberships m ON m.tenant_id=b.tenant_id AND m.id=b.membership_id JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE m.user_id=? AND r.code='owner'`, user).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if tenants != 1 || owners != 1 {
		t.Fatal("personal tenant bootstrap incomplete")
	}
	var before, after int
	f.db.QueryRow(`SELECT COUNT(*) FROM account_notifications WHERE recipient=?`, f.email).Scan(&before)
	if err = f.service.Request(ctx, strings.ToUpper(f.email), "register"); err != nil {
		t.Fatal(err)
	}
	f.db.QueryRow(`SELECT COUNT(*) FROM account_notifications WHERE recipient=?`, f.email).Scan(&after)
	if before != after {
		t.Fatal("existing account registration produced a new proof")
	}
	if _, err = f.service.CheckPassword(ctx, f.email, updatedPassword); !errors.Is(err, store.ErrDenied) {
		t.Fatal("incorrect password accepted")
	}
}
func TestPasswordResetInvalidatesOldCredentialsAndOtherProofs(t *testing.T) {
	f := newFixture(t)
	user := f.registered(t)
	one := f.request(t, "reset")
	time.Sleep(time.Millisecond)
	two := f.request(t, "reset")
	if one.Token == two.Token {
		t.Fatal("proof reused")
	}
	if err := f.service.Complete(context.Background(), one.Token, updatedPassword, "reset"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CheckPassword(context.Background(), f.email, initialPassword); !errors.Is(err, store.ErrDenied) {
		t.Fatal("old password accepted")
	}
	if _, err := f.service.CheckPassword(context.Background(), f.email, updatedPassword); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Complete(context.Background(), two.Token, initialPassword, "reset"); !errors.Is(err, store.ErrChallenge) {
		t.Fatal("stale reset proof accepted")
	}
	if err := f.service.Store.CredentialCurrent(context.Background(), user, 1); !errors.Is(err, store.ErrDenied) {
		t.Fatal("stale security version accepted")
	}
}
func TestProofPurposeExpiryAndUnknownReset(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.service.Request(ctx, f.email, "reset"); err != nil {
		t.Fatal(err)
	}
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM account_notifications WHERE recipient=?`, f.email).Scan(&count)
	if count != 0 {
		t.Fatal("unknown account reset produced notification")
	}
	proof := f.request(t, "register")
	if err := f.service.Complete(ctx, proof.Token, initialPassword, "reset"); !errors.Is(err, store.ErrChallenge) {
		t.Fatal("proof purpose confused")
	}
	if err := f.service.Complete(ctx, proof.Token, "short", "register"); err == nil {
		t.Fatal("weak password accepted")
	}
	digest := sha256.Sum256([]byte(proof.Token))
	if _, err := f.db.Exec(`UPDATE account_challenges SET expires_at=1 WHERE token_digest=?`, digest[:]); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Complete(ctx, proof.Token, initialPassword, "register"); !errors.Is(err, store.ErrChallenge) {
		t.Fatal("expired proof accepted")
	}
}
func TestConcurrentProofConsumptionIsSingleUse(t *testing.T) {
	f := newFixture(t)
	proof := f.request(t, "register")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- f.service.Complete(context.Background(), proof.Token, initialPassword, "register")
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	passed := 0
	for err := range results {
		if err == nil {
			passed++
		} else if !errors.Is(err, store.ErrChallenge) {
			t.Fatal(err)
		}
	}
	if passed != 1 {
		t.Fatal("proof consumed multiple times")
	}
}
func TestNotificationCipherBindsMetadataAndFileSinkIsIdempotent(t *testing.T) {
	f := newFixture(t)
	proof := f.request(t, "register")
	var n store.Notification
	if err := f.db.QueryRow(`SELECT id,recipient,purpose,encrypted_payload,expires_at FROM account_notifications WHERE recipient=?`, f.email).Scan(&n.ID, &n.Recipient, &n.Purpose, &n.Ciphertext, &n.Expires); err != nil {
		t.Fatal(err)
	}
	original := n.Recipient
	n.Recipient = "changed@example.invalid"
	if _, err := Decrypt(f.service.aead, n); err == nil {
		t.Fatal("recipient substitution accepted")
	}
	n.Recipient = original
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	sink := FileSink(dir, f.service.aead)
	if err := sink(n); err != nil {
		t.Fatal(err)
	}
	if err := sink(n); err != nil {
		t.Fatal("idempotent delivery failed")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("duplicate/partial mailbox files")
	}
	info, err := entries[0].Info()
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("mailbox file not private")
	}
	body, err := os.ReadFile(dir + "/" + entries[0].Name())
	if err != nil || !strings.Contains(string(body), proof.Token) {
		t.Fatal("mailbox proof mismatch")
	}
	processed, err := f.service.Store.DeliverOne(context.Background(), sink)
	if err != nil || !processed {
		t.Fatal("queue delivery failed")
	}
	var state string
	var size int
	if err = f.db.QueryRow(`SELECT state,LENGTH(encrypted_payload) FROM account_notifications WHERE id=?`, n.ID).Scan(&state, &size); err != nil {
		t.Fatal(err)
	}
	if state != "delivered" || size != 0 {
		t.Fatal("delivery acknowledgement or payload erasure failed")
	}
}
func TestDeliveryFailureBacksOffAndExpiredProofIsNotDelivered(t *testing.T) {
	f := newFixture(t)
	f.request(t, "register")
	processed, err := f.service.Store.DeliverOne(context.Background(), func(store.Notification) error { return errors.New("synthetic disk failure") })
	if processed || err == nil {
		t.Fatal("failed delivery acknowledged")
	}
	var attempts, size int
	var next int64
	if err = f.db.QueryRow(`SELECT attempts,next_attempt_at,LENGTH(encrypted_payload) FROM account_notifications WHERE recipient=?`, f.email).Scan(&attempts, &next, &size); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || next <= time.Now().Unix() || size == 0 {
		t.Fatal("retry state lost")
	}
	if _, err = f.db.Exec(`UPDATE account_notifications SET next_attempt_at=0,expires_at=1 WHERE recipient=?`, f.email); err != nil {
		t.Fatal(err)
	}
	processed, err = f.service.Store.DeliverOne(context.Background(), func(store.Notification) error { t.Error("expired proof delivered"); return nil })
	if !processed || err != nil {
		t.Fatal("expired notification not consumed")
	}
}

func TestConcurrentDistinctProofsCannotReplacePasswordOrLeavePartialUser(t *testing.T) {
	f := newFixture(t)
	one := f.request(t, "register")
	time.Sleep(time.Millisecond)
	two := f.request(t, "register")
	var before int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, token := range []string{one.Token, two.Token} {
		go func(token string) {
			<-start
			results <- f.service.Complete(context.Background(), token, initialPassword, "register")
		}(token)
	}
	close(start)
	success := 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if !errors.Is(err, store.ErrChallenge) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("distinct proofs created multiple accounts")
	}
	var after int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatal("failed competing registration left partial user")
	}
	if _, err := f.service.CheckPassword(context.Background(), f.email, initialPassword); err != nil {
		t.Fatal("winning credential invalid")
	}
}
