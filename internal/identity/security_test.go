package identity

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lth2015/sso/internal/mfa"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

func securityFixture(t *testing.T) (*fixture, *Login, store.SSOSession, []byte, []byte, string, []string) {
	t.Helper()
	f, l := loginFixture(t)
	flow, browser, _ := testFlow(t, f, l)
	_, secret, err := l.Password(context.Background(), flow, browser, f.email, initialPassword)
	if err != nil {
		t.Fatal("security fixture password failed")
	}
	flow = getFlow(t, f, flow.ID, browser)
	code, _ := mfa.Code(secret, time.Now().Unix()/30)
	codes, err := l.MFA(context.Background(), flow, browser, code, "")
	if err != nil {
		t.Fatal("security fixture enrollment failed")
	}
	sessionDigest := digest(flow.ID.String() + "session")
	session, _, err := f.service.Store.PrepareLogin(context.Background(), flow.ID, browser, sessionDigest)
	if err != nil {
		t.Fatal("security fixture session failed")
	}
	if err = f.service.Store.FinishFlow(context.Background(), flow.ID, true, store.ID{}); err != nil {
		t.Fatal("security fixture completion failed")
	}
	return f, l, session, browser, sessionDigest, secret, codes
}
func securityAction(t *testing.T, f *fixture, browser, session []byte) store.SecurityAction {
	t.Helper()
	a, err := f.service.Store.BeginSecurityAction(context.Background(), session, browser, digest("security csrf"))
	if err != nil {
		t.Fatal("security action failed")
	}
	return a
}
func TestSSOSecurityActionBindingsExpiryAndFiveAttempts(t *testing.T) {
	f, _, _, browser, session, _, _ := securityFixture(t)
	a := securityAction(t, f, browser, session)
	ctx := context.Background()
	for _, proof := range []struct{ b, s, c []byte }{{digest("other"), session, a.CSRF}, {browser, digest("other"), a.CSRF}, {browser, session, digest("other")}} {
		if _, err := f.service.Store.SecurityAction(ctx, a.ID, proof.b, proof.s, proof.c); !errors.Is(err, store.ErrDenied) {
			t.Fatal("security binding bypass")
		}
	}
	for range 5 {
		if err := f.service.Store.FailSecurityAction(ctx, a.ID, browser, session, a.CSRF); !errors.Is(err, store.ErrDenied) {
			t.Fatal("failed attempt not denied")
		}
	}
	if _, err := f.service.Store.SecurityAction(ctx, a.ID, browser, session, a.CSRF); !errors.Is(err, store.ErrDenied) {
		t.Fatal("five attempts not exhausted")
	}
	var attempts int
	if err := f.db.QueryRow(`SELECT attempts FROM security_actions WHERE id=?`, a.ID).Scan(&attempts); err != nil || attempts != 5 {
		t.Fatal("attempt count incorrect")
	}
	next := securityAction(t, f, browser, session)
	if _, err := f.service.Store.SecurityAction(ctx, a.ID, browser, session, a.CSRF); !errors.Is(err, store.ErrDenied) {
		t.Fatal("superseded action accepted")
	}
	if _, err := f.db.Exec(`UPDATE security_actions SET expires_at=0 WHERE id=?`, next.ID); err != nil {
		t.Fatal("expiry fixture failed")
	}
	if _, err := f.service.Store.SecurityAction(ctx, next.ID, browser, session, next.CSRF); !errors.Is(err, store.ErrDenied) {
		t.Fatal("expired action accepted")
	}
}
func TestSSORecoveryRegenerationRevokesAuthorityAndPreservesFactor(t *testing.T) {
	f, l, sess, browser, session, secret, oldCodes := securityFixture(t)
	a := securityAction(t, f, browser, session)
	ctx := context.Background()
	before, err := f.service.Store.Factor(ctx, sess.User)
	if err != nil {
		t.Fatal("factor unavailable")
	}
	code, _ := mfa.Code(secret, time.Now().Unix()/30+1)
	codes, err := l.regenerateRecovery(ctx, a, browser, session, a.CSRF, initialPassword, code)
	if err != nil || len(codes) != 10 {
		t.Fatal("regeneration failed")
	}
	after, err := f.service.Store.Factor(ctx, sess.User)
	if err != nil || after.Version != before.Version+1 || !bytes.Equal(after.Cipher, before.Cipher) {
		t.Fatal("factor key changed or version not advanced")
	}
	if _, err = f.service.Store.SSOSession(ctx, session); !errors.Is(err, store.ErrDenied) {
		t.Fatal("old session remained valid")
	}
	if _, err = l.regenerateRecovery(ctx, a, browser, session, a.CSRF, initialPassword, code); !errors.Is(err, store.ErrDenied) {
		t.Fatal("action replay returned codes")
	}
	oldDigest, _ := recoveryDigest(sess.User, oldCodes[9])
	var count int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM mfa_recovery_codes WHERE user_id=? AND digest=?`, sess.User, oldDigest).Scan(&count); err != nil || count != 0 {
		t.Fatal("old unused recovery code remained")
	}
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM mfa_recovery_codes WHERE user_id=? AND used_at IS NULL`, sess.User).Scan(&count); err != nil || count != 10 {
		t.Fatal("new code set incomplete")
	}
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM identity_events WHERE user_id=? AND action='mfa.recovery_regenerated'`, sess.User).Scan(&count); err != nil || count != 1 {
		t.Fatal("security audit missing")
	}
	flow, b, _ := testFlow(t, f, l)
	if _, _, err = l.Password(ctx, flow, b, f.email, initialPassword); err != nil {
		t.Fatal("password no longer valid")
	}
	flow = getFlow(t, f, flow.ID, b)
	if _, err = l.MFA(ctx, flow, b, "", oldCodes[9]); !errors.Is(err, store.ErrDenied) {
		t.Fatal("old recovery code accepted")
	}
	if _, err = l.MFA(ctx, flow, b, "", codes[0]); err != nil {
		t.Fatal("new recovery code rejected")
	}
}
func TestSSOConcurrentRecoveryRegenerationReturnsOnlyOneCodeSet(t *testing.T) {
	f, l, _, browser, session, secret, _ := securityFixture(t)
	a := securityAction(t, f, browser, session)
	code, _ := mfa.Code(secret, time.Now().Unix()/30+1)
	start := make(chan struct{})
	results := make(chan bool, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes, err := l.regenerateRecovery(context.Background(), a, browser, session, a.CSRF, initialPassword, code)
			results <- err == nil && len(codes) == 10
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for ok := range results {
		if ok {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("concurrent regeneration returned multiple or zero code sets")
	}
}
func TestSSOSecurityActionCannotSurvivePasswordReset(t *testing.T) {
	f, l, session, browser, cookie, secret, _ := securityFixture(t)
	a := securityAction(t, f, browser, cookie)
	reset := f.request(t, "reset")
	if err := f.service.Complete(context.Background(), reset.Token, updatedPassword, "reset"); err != nil {
		t.Fatal("reset fixture failed")
	}
	code, _ := mfa.Code(secret, time.Now().Unix()/30+1)
	if _, err := l.regenerateRecovery(context.Background(), a, browser, cookie, a.CSRF, initialPassword, code); !errors.Is(err, store.ErrDenied) {
		t.Fatal("old management proof survived reset")
	}
	factor, err := f.service.Store.Factor(context.Background(), session.User)
	if err != nil || factor.Version != 1 {
		t.Fatal("denied rotation changed factor")
	}
}
