package identity

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"github.com/lth2015/sso/internal/adapter/hydra"
	"github.com/lth2015/sso/internal/mfa"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const BrowserCookie = "local_sso_browser"
const SessionCookie = "local_sso_session"

type Login struct {
	Account         *Service
	Admin           *hydra.Client
	FlowKey, MFAKey cipher.AEAD
	Limit           *Limiter
}

func NewLogin(account *Service, flowKey, mfaKey []byte) (*Login, error) {
	flow, err := NewCipher(flowKey)
	if err != nil {
		return nil, err
	}
	factor, err := NewCipher(mfaKey)
	if err != nil {
		return nil, err
	}
	return &Login{account, hydra.New(), flow, factor, NewLimiter()}, nil
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func digest(value string) []byte { s := sha256.Sum256([]byte(value)); return s[:] }
func cookieDigest(r *http.Request, name string) []byte {
	cookie, err := r.Cookie(name)
	if err != nil {
		return nil
	}
	if _, err = tokenDigest(cookie.Value); err != nil {
		return nil
	}
	return digest(cookie.Value)
}
func setLocalCookie(w http.ResponseWriter, name, value string, seconds int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: false, MaxAge: seconds})
}
func seal(key cipher.AEAD, body []byte, aad string) ([]byte, error) {
	nonce := make([]byte, key.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return key.Seal(nonce, nonce, body, []byte(aad)), nil
}
func openSeal(key cipher.AEAD, body []byte, aad string) ([]byte, error) {
	if len(body) < key.NonceSize()+key.Overhead() {
		return nil, errors.New("invalid encrypted state")
	}
	value, err := key.Open(nil, body[:key.NonceSize()], body[key.NonceSize():], []byte(aad))
	if err != nil {
		return nil, errors.New("invalid encrypted state")
	}
	return value, nil
}
func (l *Login) Start(ctx context.Context, kind, challenge string, browser, csrf []byte, sessionDigest []byte) (store.Flow, bool, error) {
	var flow store.Flow
	if kind != "login" && kind != "consent" || len(challenge) == 0 || len(challenge) > 24576 {
		return flow, false, store.ErrInvalid
	}
	info, err := l.Admin.Get(ctx, kind, challenge)
	if err != nil {
		return flow, false, err
	}
	client, err := l.Account.Store.OAuthClient(ctx, info.Client.ID)
	if err != nil {
		return flow, false, err
	}
	requested, err := url.Parse(info.RequestURL)
	if err != nil {
		return flow, false, store.ErrDenied
	}
	query := requested.Query()
	if requested.Scheme != "http" || requested.Host != "auth.localhost:4464" || query.Get("redirect_uri") != client.Redirect || query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" || !hydra.ScopeSubset(info.RequestedScopes, client.Scopes) || len(info.RequestedAudience) != 1 || info.RequestedAudience[0] != client.Audience {
		return flow, false, store.ErrDenied
	}
	flow.ID, err = store.NewID()
	if err != nil {
		return flow, false, err
	}
	flow.Kind = kind
	flow.Client = client.ID
	flow.BrowserDigest = browser
	flow.CSRF = csrf
	flow.Scopes = strings.Join(info.RequestedScopes, " ")
	flow.Audience = client.Audience
	flow.Expires = time.Now().Add(5 * time.Minute).Unix()
	flow.State = "password"
	flow.HydraSID = info.SessionID
	flow.ChallengeDigest = digest(challenge)
	flow.Challenge, err = seal(l.FlowKey, []byte(challenge), "challenge:"+flow.ID.String())
	if err != nil {
		return flow, false, err
	}
	session, sessionErr := l.Account.Store.SSOSession(ctx, sessionDigest)
	if kind == "consent" {
		if sessionErr != nil || session.User.String() != info.Subject {
			return flow, false, store.ErrDenied
		}
		flow.State = "consent"
		flow.HydraSID = info.LoginSessionID
	} else if info.Skip && sessionErr == nil && session.User.String() == info.Subject {
		flow.State = "passed"
	}
	if flow.State == "consent" || flow.State == "passed" {
		flow.User = session.User
		flow.UserVersion = session.UserVersion
		flow.Session = session.ID
		flow.MFAVersion = session.MFAVersion
		flow.AuthenticatedAt = session.AuthTime
		flow.AMR = session.AMR
	}
	none := false
	for _, p := range strings.Fields(query.Get("prompt")) {
		if p == "none" {
			none = true
		}
	}
	if none && flow.State == "password" {
		redirect, err := l.Admin.Decide(ctx, "login", challenge, "reject", map[string]string{"error": "login_required"})
		flow.State = redirect
		return flow, true, err
	}
	if err = l.Account.Store.CreateFlow(ctx, flow); err != nil {
		return flow, false, err
	}
	if none && kind == "consent" {
		options, err := l.Account.Store.TenantOptions(ctx, flow.User, flow.Session, flow.Client)
		if err != nil {
			return flow, false, err
		}
		if len(options) != 1 || !info.Skip {
			redirect, err := l.Admin.Decide(ctx, "consent", challenge, "reject", map[string]string{"error": "interaction_required"})
			flow.State = redirect
			return flow, true, err
		}
		redirect, err := l.Consent(ctx, flow, browser, sessionDigest, options[0].ID)
		flow.State = redirect
		return flow, true, err
	}
	return flow, false, nil
}
func (l *Login) Password(ctx context.Context, f store.Flow, browser []byte, email, value string) (string, string, error) {
	proof, err := l.Account.CheckPasswordProof(ctx, email, value)
	if errors.Is(err, store.ErrDenied) || errors.Is(err, store.ErrInvalid) {
		_ = l.Account.Store.FailedPassword(ctx, f.ID, browser)
		return "", "", store.ErrDenied
	}
	if err != nil {
		return "", "", err
	}
	secret, err := mfa.NewSecret()
	if err != nil {
		return "", "", err
	}
	pending, err := seal(l.MFAKey, []byte(secret), "pending:"+proof.User.String()+":"+f.ID.String())
	if err != nil {
		return "", "", err
	}
	state, err := l.Account.Store.PasswordPassed(ctx, f.ID, browser, proof, pending)
	if err != nil {
		return "", "", err
	}
	if state != "enroll" {
		secret = ""
	}
	return state, secret, nil
}
func recoveryDigest(user store.ID, code string) ([]byte, error) {
	normalized := strings.ReplaceAll(strings.ToLower(code), "-", "")
	if len(normalized) != 32 {
		return nil, store.ErrDenied
	}
	if _, err := hex.DecodeString(normalized); err != nil {
		return nil, store.ErrDenied
	}
	return digest("recovery:" + user.String() + ":" + normalized), nil
}
func (l *Login) MFA(ctx context.Context, f store.Flow, browser []byte, code, recovery string) ([]string, error) {
	result := store.MFAResult{Step: -1}
	codes := []string{}
	if f.State == "enroll" {
		plain, err := openSeal(l.MFAKey, f.Pending, "pending:"+f.User.String()+":"+f.ID.String())
		if err != nil {
			return nil, err
		}
		step, ok := mfa.Match(string(plain), code, time.Now().Unix(), -1)
		if !ok || recovery != "" {
			return nil, l.Account.Store.FailedMFA(ctx, f.ID, browser)
		}
		result.Step = step
		result.NewCipher, err = seal(l.MFAKey, plain, "factor:"+f.User.String())
		if err != nil {
			return nil, err
		}
		for range 10 {
			raw := make([]byte, 16)
			if _, err = rand.Read(raw); err != nil {
				return nil, err
			}
			value := hex.EncodeToString(raw)
			formatted := value[:8] + "-" + value[8:16] + "-" + value[16:24] + "-" + value[24:]
			d, err := recoveryDigest(f.User, formatted)
			if err != nil {
				return nil, err
			}
			codes = append(codes, formatted)
			result.Recovery = append(result.Recovery, d)
		}
	} else if f.State == "mfa" {
		factor, err := l.Account.Store.Factor(ctx, f.User)
		if err != nil {
			return nil, err
		}
		if !factor.Active {
			return nil, store.ErrDenied
		}
		result.FactorVersion = factor.Version
		if recovery != "" {
			if code != "" {
				return nil, l.Account.Store.FailedMFA(ctx, f.ID, browser)
			}
			result.RecoveryDigest, err = recoveryDigest(f.User, recovery)
			if err != nil {
				return nil, l.Account.Store.FailedMFA(ctx, f.ID, browser)
			}
		} else {
			plain, err := openSeal(l.MFAKey, factor.Cipher, "factor:"+f.User.String())
			if err != nil {
				return nil, err
			}
			step, ok := mfa.Match(string(plain), code, time.Now().Unix(), factor.LastStep)
			if !ok {
				return nil, l.Account.Store.FailedMFA(ctx, f.ID, browser)
			}
			result.Step = step
		}
	} else {
		return nil, store.ErrConflict
	}
	if err := l.Account.Store.MFAPassed(ctx, f.ID, browser, result); err != nil {
		return nil, err
	}
	return codes, nil
}
func (l *Login) CompleteLogin(ctx context.Context, f store.Flow, browser []byte) (string, string, error) {
	secret, err := randomToken()
	if err != nil {
		return "", "", err
	}
	session, fresh, err := l.Account.Store.PrepareLogin(ctx, f.ID, browser, digest(secret))
	if err != nil {
		return "", "", err
	}
	challenge, err := openSeal(l.FlowKey, f.Challenge, "challenge:"+f.ID.String())
	if err != nil {
		return "", "", err
	}
	redirect, err := l.Admin.Decide(ctx, "login", string(challenge), "accept", map[string]any{"subject": session.User.String(), "remember": true, "remember_for": 8 * 3600, "acr": "urn:local:mfa", "amr": []string{"pwd", session.AMR}})
	if err != nil {
		revoke := store.ID{}
		if fresh {
			revoke = session.ID
		}
		_ = l.Account.Store.FinishFlow(ctx, f.ID, false, revoke)
		return "", "", err
	}
	if err = l.Account.Store.FinishFlow(ctx, f.ID, true, store.ID{}); err != nil {
		return "", "", err
	}
	if !fresh {
		secret = ""
	}
	return redirect, secret, nil
}
func (l *Login) Consent(ctx context.Context, f store.Flow, browser, session []byte, tenant store.ID) (string, error) {
	grant, err := l.Account.Store.PrepareConsent(ctx, f.ID, browser, session, tenant)
	if err != nil {
		return "", err
	}
	challenge, err := openSeal(l.FlowKey, f.Challenge, "challenge:"+f.ID.String())
	if err != nil {
		return "", err
	}
	redirect, err := l.Admin.Decide(ctx, "consent", string(challenge), "accept", map[string]any{"grant_scope": strings.Fields(f.Scopes), "grant_access_token_audience": []string{f.Audience}, "remember": true, "remember_for": 8 * 3600, "session": map[string]any{"access_token": map[string]string{"business_context_id": grant.String()}}})
	if err != nil {
		_ = l.Account.Store.FinishFlow(ctx, f.ID, false, store.ID{})
		return "", err
	}
	if err = l.Account.Store.FinishFlow(ctx, f.ID, true, store.ID{}); err != nil {
		return "", err
	}
	return redirect, nil
}

func (l *Login) ReconcileLogout(ctx context.Context) {
	lastPrune := time.Time{}
	for ctx.Err() == nil {
		if time.Since(lastPrune) >= 30*time.Second {
			prune, cancel := context.WithTimeout(ctx, 2*time.Second)
			_ = l.Account.Store.PruneAuth(prune)
			cancel()
			lastPrune = time.Now()
		}
		step, cancel := context.WithTimeout(ctx, 5*time.Second)
		didWork, _ := l.Account.Store.DeliverProtocolLogout(step, func(user, sid string) error {
			return l.Admin.Do(step, "DELETE", "/admin/oauth2/auth/sessions/login?"+url.Values{"subject": {user}, "sid": {sid}}.Encode(), nil, nil)
		})
		cancel()
		if !didWork {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func (l *Login) Reject(ctx context.Context, f store.Flow, browser []byte) (string, error) {
	if err := l.Account.Store.BeginReject(ctx, f.ID, browser); err != nil {
		return "", err
	}
	challenge, err := openSeal(l.FlowKey, f.Challenge, "challenge:"+f.ID.String())
	if err != nil {
		return "", err
	}
	redirect, err := l.Admin.Decide(ctx, f.Kind, string(challenge), "reject", map[string]string{"error": "access_denied"})
	if err != nil {
		_ = l.Account.Store.FinishFlow(ctx, f.ID, false, store.ID{})
		return "", err
	}
	if err = l.Account.Store.FinishFlow(ctx, f.ID, true, store.ID{}); err != nil {
		return "", err
	}
	return redirect, nil
}
