package identity

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lth2015/sso/internal/mfa"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

func pendingReplacement(t *testing.T) (*fixture, *Login, store.SSOSession, []byte, []byte, string, store.FactorChange, string, []string) {
	t.Helper()
	f, l, session, browser, cookie, old, codes := securityFixture(t)
	action, err := f.service.Store.BeginFactorChange(context.Background(), cookie, browser, digest("replacement csrf"))
	if err != nil {
		t.Fatal("replacement setup failed")
	}
	code, _ := mfa.Code(old, time.Now().Unix()/30+1)
	secret, err := l.startFactorChange(context.Background(), action, browser, cookie, action.CSRF, initialPassword, code)
	if err != nil {
		t.Fatal("replacement proof failed")
	}
	action, err = f.service.Store.FactorChange(context.Background(), action.ID, browser, cookie, action.CSRF)
	if err != nil {
		t.Fatal("replacement state unavailable")
	}
	return f, l, session, browser, cookie, old, action, secret, codes
}
func TestSSOAuthenticatorStagingAndCancellationPreserveAuthority(t *testing.T) {
	f, l, session, browser, cookie, old, a, secret, _ := pendingReplacement(t)
	ctx := context.Background()
	if a.State != "enrolling" || len(a.Pending) == 0 || bytes.Contains(a.Pending, []byte(secret)) {
		t.Fatal("pending seed not encrypted")
	}
	if _, err := openSeal(l.MFAKey, a.Pending, "replacement:"+a.User.String()+":wrong-action"); err == nil {
		t.Fatal("pending seed AAD did not bind action")
	}
	factor, err := f.service.Store.Factor(ctx, session.User)
	if err != nil {
		t.Fatal("factor unavailable")
	}
	plain, err := openSeal(l.MFAKey, factor.Cipher, "factor:"+session.User.String())
	if err != nil || string(plain) != old || factor.Version != 1 {
		t.Fatal("staging changed active factor")
	}
	if _, err = l.startFactorChange(ctx, a, browser, cookie, a.CSRF, initialPassword, "000000"); !errors.Is(err, store.ErrDenied) {
		t.Fatal("start replay returned another seed")
	}
	if err = f.service.Store.CancelFactorChange(ctx, a.ID, browser, cookie, a.CSRF); err != nil {
		t.Fatal("cancel failed")
	}
	if _, err = f.service.Store.SSOSession(ctx, cookie); err != nil {
		t.Fatal("cancel revoked existing session")
	}
	var pending []byte
	var state string
	if err = f.db.QueryRow(`SELECT state,pending_secret FROM authenticator_changes WHERE id=?`, a.ID).Scan(&state, &pending); err != nil || state != "cancelled" || pending != nil {
		t.Fatal("cancel did not erase pending seed")
	}
	code, _ := mfa.Code(secret, time.Now().Unix()/30)
	if _, err = l.confirmFactorChange(ctx, a, browser, cookie, a.CSRF, code); !errors.Is(err, store.ErrDenied) {
		t.Fatal("cancelled setup activated")
	}
}
func TestSSOAuthenticatorCommitReplacesKeyAndCodes(t *testing.T) {
	f, l, session, browser, cookie, old, a, secret, oldCodes := pendingReplacement(t)
	ctx := context.Background()
	if secret == old {
		t.Fatal("new factor reused old key")
	}
	code, _ := mfa.Code(secret, time.Now().Unix()/30)
	codes, err := l.confirmFactorChange(ctx, a, browser, cookie, a.CSRF, code)
	if err != nil || len(codes) != 10 {
		t.Fatal("replacement confirmation failed")
	}
	factor, err := f.service.Store.Factor(ctx, session.User)
	if err != nil || factor.Version != 2 {
		t.Fatal("factor version not advanced")
	}
	plain, err := openSeal(l.MFAKey, factor.Cipher, "factor:"+session.User.String())
	if err != nil || string(plain) != secret {
		t.Fatal("wrong factor activated")
	}
	if _, err = f.service.Store.SSOSession(ctx, cookie); !errors.Is(err, store.ErrDenied) {
		t.Fatal("old session survived key replacement")
	}
	var pending []byte
	if err = f.db.QueryRow(`SELECT pending_secret FROM authenticator_changes WHERE id=?`, a.ID).Scan(&pending); err != nil || pending != nil {
		t.Fatal("committed seed retained in staging")
	}
	flow, b, _ := testFlow(t, f, l)
	if _, _, err = l.Password(ctx, flow, b, f.email, initialPassword); err != nil {
		t.Fatal("replacement broke password")
	}
	flow = getFlow(t, f, flow.ID, b)
	if _, err = l.MFA(ctx, flow, b, "", oldCodes[9]); !errors.Is(err, store.ErrDenied) {
		t.Fatal("old recovery code survived")
	}
	if _, err = l.MFA(ctx, flow, b, "", codes[0]); err != nil {
		t.Fatal("new recovery code rejected")
	}
	flow, b, _ = testFlow(t, f, l)
	if _, _, err = l.Password(ctx, flow, b, f.email, initialPassword); err != nil {
		t.Fatal("new TOTP login password failed")
	}
	flow = getFlow(t, f, flow.ID, b)
	fresh, _ := mfa.Code(secret, time.Now().Unix()/30+1)
	if _, err = l.MFA(ctx, flow, b, fresh, ""); err != nil {
		t.Fatal("new authenticator did not verify")
	}
}
func TestSSOAuthenticatorBindingsAndPurposeSeparation(t *testing.T) {
	f, _, _, browser, cookie, _, a, _, _ := pendingReplacement(t)
	ctx := context.Background()
	for _, p := range []struct{ b, s, c []byte }{{digest("other"), cookie, a.CSRF}, {browser, digest("other"), a.CSRF}, {browser, cookie, digest("other")}} {
		if _, err := f.service.Store.FactorChange(ctx, a.ID, p.b, p.s, p.c); !errors.Is(err, store.ErrDenied) {
			t.Fatal("replacement binding bypass")
		}
	}
	if _, err := f.service.Store.SecurityAction(ctx, a.ID, browser, cookie, a.CSRF); !errors.Is(err, store.ErrDenied) {
		t.Fatal("recovery endpoint accepted replacement ID")
	}
	recovery := securityAction(t, f, browser, cookie)
	if _, err := f.service.Store.FactorChange(ctx, recovery.ID, browser, cookie, recovery.CSRF); !errors.Is(err, store.ErrDenied) {
		t.Fatal("replacement endpoint accepted recovery ID")
	}
	if _, err := f.db.Exec(`UPDATE authenticator_changes SET expires_at=0 WHERE id=?`, a.ID); err != nil {
		t.Fatal("expiry fixture failed")
	}
	if _, err := f.service.Store.FactorChange(ctx, a.ID, browser, cookie, a.CSRF); !errors.Is(err, store.ErrDenied) {
		t.Fatal("expired replacement accepted")
	}
	if err := f.service.Store.PruneAuth(ctx); err != nil {
		t.Fatal("expired setup pruning failed")
	}
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM authenticator_changes WHERE id=?`, a.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired encrypted seed not pruned")
	}
}
func TestSSOAuthenticatorFailedProofBudgetErasesPendingSeed(t *testing.T) {
	f, l, session, browser, cookie, old, a, _, _ := pendingReplacement(t)
	ctx := context.Background()
	for range 5 {
		if _, err := l.confirmFactorChange(ctx, a, browser, cookie, a.CSRF, "invalid"); !errors.Is(err, store.ErrDenied) {
			t.Fatal("invalid new factor accepted")
		}
	}
	var state string
	var pending []byte
	var attempts int
	if err := f.db.QueryRow(`SELECT state,pending_secret,attempts FROM authenticator_changes WHERE id=?`, a.ID).Scan(&state, &pending, &attempts); err != nil || state != "failed" || pending != nil || attempts != 5 {
		t.Fatal("proof budget did not erase pending seed")
	}
	factor, err := f.service.Store.Factor(ctx, session.User)
	if err != nil {
		t.Fatal("factor unavailable")
	}
	plain, err := openSeal(l.MFAKey, factor.Cipher, "factor:"+session.User.String())
	if err != nil || string(plain) != old || factor.Version != 1 {
		t.Fatal("failed setup replaced old factor")
	}
}
func TestSSOAuthenticatorPasswordResetInvalidatesPendingSetup(t *testing.T) {
	f, l, session, browser, cookie, old, a, secret, _ := pendingReplacement(t)
	reset := f.request(t, "reset")
	if err := f.service.Complete(context.Background(), reset.Token, updatedPassword, "reset"); err != nil {
		t.Fatal("reset fixture failed")
	}
	code, _ := mfa.Code(secret, time.Now().Unix()/30)
	if _, err := l.confirmFactorChange(context.Background(), a, browser, cookie, a.CSRF, code); !errors.Is(err, store.ErrDenied) {
		t.Fatal("old setup survived password reset")
	}
	factor, err := f.service.Store.Factor(context.Background(), session.User)
	if err != nil {
		t.Fatal("factor unavailable")
	}
	plain, err := openSeal(l.MFAKey, factor.Cipher, "factor:"+session.User.String())
	if err != nil || string(plain) != old || factor.Version != 1 {
		t.Fatal("reset race changed factor")
	}
}
func TestSSOAuthenticatorConcurrentConfirmOrCancelHasOneWinner(t *testing.T) {
	if os.Getenv("SSO_LOGIN_TEST") != "true" {
		t.Skip("requires real local SSO setup: scripts/sso test")
	}
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "two_confirms", true: "confirm_and_cancel"}[cancel], func(t *testing.T) {
			f, l, _, browser, cookie, _, a, secret, _ := pendingReplacement(t)
			code, _ := mfa.Code(secret, time.Now().Unix()/30)
			start := make(chan struct{})
			results := make(chan bool, 2)
			var wg sync.WaitGroup
			for i := range 2 {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					if cancel && i == 1 {
						results <- f.service.Store.CancelFactorChange(context.Background(), a.ID, browser, cookie, a.CSRF) == nil
						return
					}
					codes, err := l.confirmFactorChange(context.Background(), a, browser, cookie, a.CSRF, code)
					results <- err == nil && len(codes) == 10
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)
			count := 0
			for success := range results {
				if success {
					count++
				}
			}
			if count != 1 {
				t.Fatal("replacement had multiple or zero terminal winners")
			}
		})
	}
}
