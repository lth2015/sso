package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/lth2015/sso/internal/mfa"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func loginFixture(t *testing.T) (*fixture, *Login) {
	t.Helper()
	if os.Getenv("SSO_LOGIN_TEST") != "true" {
		t.Skip("requires real local SSO setup: scripts/sso test")
	}
	f := newFixture(t)
	f.registered(t)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	factor := make([]byte, 32)
	if _, err := rand.Read(factor); err != nil {
		t.Fatal(err)
	}
	l, err := NewLogin(f.service, key, factor)
	if err != nil {
		t.Fatal(err)
	}
	return f, l
}
func testFlow(t *testing.T, f *fixture, l *Login) (store.Flow, []byte, []byte) {
	t.Helper()
	id, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	browser, csrf := digest(id.String()+"browser"), digest(id.String()+"csrf")
	flow := store.Flow{ID: id, Kind: "login", Client: "local-demo-a", State: "password", HydraSID: "test-sid", Scopes: "openid offline_access demo:read", Audience: "https://api.demo.invalid", Challenge: []byte("synthetic test binding; no protocol acceptance"), ChallengeDigest: digest(id.String()), BrowserDigest: browser, CSRF: csrf, Expires: time.Now().Unix() + 300}
	if err = f.service.Store.CreateFlow(context.Background(), flow); err != nil {
		t.Fatal(err)
	}
	return flow, browser, csrf
}
func getFlow(t *testing.T, f *fixture, id store.ID, browser []byte) store.Flow {
	t.Helper()
	flow, err := f.service.Store.Flow(context.Background(), id, browser, nil)
	if err != nil {
		t.Fatal(err)
	}
	return flow
}
func TestSSOBrowserCSRFAndPasswordResetRace(t *testing.T) {
	f, l := loginFixture(t)
	flow, browser, csrf := testFlow(t, f, l)
	if _, err := f.service.Store.Flow(context.Background(), flow.ID, digest("other-browser"), csrf); !errors.Is(err, store.ErrDenied) {
		t.Fatal("other browser read flow")
	}
	if _, err := f.service.Store.Flow(context.Background(), flow.ID, browser, digest("other-csrf")); !errors.Is(err, store.ErrDenied) {
		t.Fatal("CSRF mismatch accepted")
	}
	proof, err := f.service.CheckPasswordProof(context.Background(), f.email, initialPassword)
	if err != nil {
		t.Fatal(err)
	}
	reset := f.request(t, "reset")
	if err = f.service.Complete(context.Background(), reset.Token, updatedPassword, "reset"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Store.PasswordPassed(context.Background(), flow.ID, browser, proof, []byte("pending")); !errors.Is(err, store.ErrDenied) {
		t.Fatal("old password proof survived reset")
	}
}
func TestSSOTOTPAndRecoveryCannotBeReplayed(t *testing.T) {
	f, l := loginFixture(t)
	ctx := context.Background()
	flow, browser, _ := testFlow(t, f, l)
	state, secret, err := l.Password(ctx, flow, browser, f.email, initialPassword)
	if err != nil || state != "enroll" {
		t.Fatal("password proof did not require enrollment")
	}
	flow = getFlow(t, f, flow.ID, browser)
	code, err := mfa.Code(secret, time.Now().Unix()/30)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := l.MFA(ctx, flow, browser, code, "")
	if err != nil || len(codes) != 10 {
		t.Fatal("MFA enrollment failed")
	}
	if _, err = l.MFA(ctx, flow, browser, code, ""); !errors.Is(err, store.ErrConflict) {
		t.Fatal("factor enrollment replayed")
	}
	next, other, _ := testFlow(t, f, l)
	if state, _, err = l.Password(ctx, next, other, f.email, initialPassword); err != nil || state != "mfa" {
		t.Fatal("existing factor bypassed")
	}
	next = getFlow(t, f, next.ID, other)
	if _, err = l.MFA(ctx, next, other, code, ""); !errors.Is(err, store.ErrDenied) {
		t.Fatal("TOTP time slice replayed")
	}
	if _, err = l.MFA(ctx, next, other, "", codes[0]); err != nil {
		t.Fatal("fresh recovery code rejected")
	}
	third, thirdBrowser, _ := testFlow(t, f, l)
	if _, _, err = l.Password(ctx, third, thirdBrowser, f.email, initialPassword); err != nil {
		t.Fatal(err)
	}
	third = getFlow(t, f, third.ID, thirdBrowser)
	if _, err = l.MFA(ctx, third, thirdBrowser, "", codes[0]); !errors.Is(err, store.ErrDenied) {
		t.Fatal("recovery code replayed")
	}
	var material []byte
	if err = f.db.QueryRow(`SELECT encrypted_secret FROM mfa_factors WHERE user_id=?`, flow.User).Scan(&material); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(material), secret) {
		t.Fatal("TOTP seed stored in plaintext")
	}
	var stored int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM mfa_recovery_codes WHERE user_id=? AND LENGTH(digest)=32`, flow.User).Scan(&stored); err != nil || stored != 10 {
		t.Fatal("recovery code digest storage invalid")
	}
}
func TestSSOConcurrentEnrollmentActivatesOnlyOneFactor(t *testing.T) {
	f, l := loginFixture(t)
	ctx := context.Background()
	type setup struct {
		flow    store.Flow
		browser []byte
		secret  string
	}
	items := []setup{}
	for range 2 {
		flow, browser, _ := testFlow(t, f, l)
		_, secret, err := l.Password(ctx, flow, browser, f.email, initialPassword)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, setup{getFlow(t, f, flow.ID, browser), browser, secret})
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, item := range items {
		wg.Add(1)
		go func(item setup) {
			defer wg.Done()
			<-start
			code, err := mfa.Code(item.secret, time.Now().Unix()/30)
			if err == nil {
				_, err = l.MFA(ctx, item.flow, item.browser, code, "")
			}
			results <- err
		}(item)
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, store.ErrDenied) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("multiple factors activated concurrently")
	}
}
func TestSSOAttemptBudgetAndExpiredInteraction(t *testing.T) {
	f, l := loginFixture(t)
	flow, browser, _ := testFlow(t, f, l)
	ctx := context.Background()
	for range 5 {
		_ = f.service.Store.FailedPassword(ctx, flow.ID, browser)
	}
	if _, err := f.service.Store.Flow(ctx, flow.ID, browser, nil); !errors.Is(err, store.ErrDenied) {
		t.Fatal("attempt budget not enforced")
	}
	next, other, _ := testFlow(t, f, l)
	if _, err := f.db.Exec(`UPDATE login_interactions SET expires_at=1 WHERE id=?`, next.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Store.Flow(ctx, next.ID, other, nil); !errors.Is(err, store.ErrDenied) {
		t.Fatal("expired interaction accepted")
	}
}

func TestSSOLogoutAllInvalidatesInflightFactorProof(t *testing.T) {
	f, l := loginFixture(t)
	ctx := context.Background()
	flow, browser, _ := testFlow(t, f, l)
	_, secret, err := l.Password(ctx, flow, browser, f.email, initialPassword)
	if err != nil {
		t.Fatal(err)
	}
	flow = getFlow(t, f, flow.ID, browser)
	code, _ := mfa.Code(secret, time.Now().Unix()/30)
	if _, err = l.MFA(ctx, flow, browser, code, ""); err != nil {
		t.Fatal(err)
	}
	flow = getFlow(t, f, flow.ID, browser)
	sessionToken, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := f.service.Store.PrepareLogin(ctx, flow.ID, browser, digest(sessionToken))
	if err != nil {
		t.Fatal(err)
	}
	other, otherBrowser, _ := testFlow(t, f, l)
	if _, _, err = l.Password(ctx, other, otherBrowser, f.email, initialPassword); err != nil {
		t.Fatal(err)
	}
	csrf := digest("logout-test-csrf")
	if err = f.service.Store.LogoutCSRF(ctx, digest(sessionToken), csrf); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Store.LogoutAll(ctx, digest(sessionToken), csrf); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Store.SSOSession(ctx, digest(sessionToken)); !errors.Is(err, store.ErrDenied) {
		t.Fatal("central session survived logout")
	}
	if err = f.service.Store.CredentialCurrent(ctx, session.User, flow.UserVersion); !errors.Is(err, store.ErrDenied) {
		t.Fatal("old identity epoch survived logout")
	}
	other = getFlow(t, f, other.ID, otherBrowser)
	factor, err := f.service.Store.Factor(ctx, other.User)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Store.MFAPassed(ctx, other.ID, otherBrowser, store.MFAResult{Step: factor.LastStep + 1, FactorVersion: factor.Version}); !errors.Is(err, store.ErrDenied) {
		t.Fatal("in-flight old password proof crossed logout epoch")
	}
}
