// Package demo is a local BFF reference; it is not a generic OAuth server.
package demo

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/lth2015/sso/internal/adapter/hydra"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"golang.org/x/oauth2"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const Audience = "https://api.demo.invalid"
const sessionCookie = "local_app_session"
const browserCookie = "local_app_browser"

type Bridge struct{ Base http.RoundTripper }

func (b Bridge) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "http" || request.URL.Host != "auth.localhost:4464" {
		return nil, errors.New("untrusted issuer destination")
	}
	clone := request.Clone(request.Context())
	u := *request.URL
	clone.URL = &u
	clone.URL.Host = "protocol-public:4444"
	clone.Host = ""
	return b.Base.RoundTrip(clone)
}

type App struct {
	Store                store.Store
	Client, Name, Origin string
	Key                  cipher.AEAD
	Provider             *oidc.Provider
	OAuth                oauth2.Config
	HTTP                 *http.Client
}

func New(ctx context.Context, repo store.Store, client, secret string, key []byte) (*App, error) {
	origin := ""
	name := ""
	switch client {
	case "local-demo-a":
		origin = "http://app-a.localhost:3001"
		name = "创作工作台"
	case "local-demo-b":
		origin = "http://app-b.localhost:3002"
		name = "研究工作台"
	default:
		return nil, errors.New("unknown local app")
	}
	if len(key) != 32 || len(secret) < 32 {
		return nil, errors.New("missing local app credentials")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Transport: Bridge{&http.Transport{Proxy: nil}}, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, httpClient), hydra.Issuer)
	if err != nil {
		return nil, errors.New("issuer unavailable")
	}
	endpoint := provider.Endpoint()
	endpoint.AuthStyle = oauth2.AuthStyleInHeader
	return &App{repo, client, name, origin, aead, provider, oauth2.Config{ClientID: client, ClientSecret: secret, RedirectURL: origin + "/oidc/callback", Scopes: []string{"openid", "offline_access", "demo:read"}, Endpoint: endpoint}, httpClient}, nil
}
func token() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func hash(value string) []byte { s := sha256.Sum256([]byte(value)); return s[:] }
func cookie(r *http.Request, name string) []byte {
	c, err := r.Cookie(name)
	if err != nil || len(c.Value) != 43 {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(raw) != 32 {
		return nil
	}
	return hash(c.Value)
}
func setCookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, MaxAge: age, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
}
func (a *App) seal(value any, aad string) ([]byte, error) {
	plain, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.Key.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return a.Key.Seal(nonce, nonce, plain, []byte(aad)), nil
}
func (a *App) open(body []byte, aad string, target any) error {
	if len(body) < a.Key.NonceSize()+a.Key.Overhead() {
		return errors.New("invalid BFF state")
	}
	plain, err := a.Key.Open(nil, body[:a.Key.NonceSize()], body[a.Key.NonceSize():], []byte(aad))
	if err != nil {
		return errors.New("invalid BFF state")
	}
	return json.Unmarshal(plain, target)
}
func (a *App) ctx(ctx context.Context) context.Context {
	return context.WithValue(oidc.ClientContext(ctx, a.HTTP), oauth2.HTTPClient, a.HTTP)
}

type transaction struct{ Verifier, Nonce string }
type Tokens struct {
	Access, Refresh, ID, Nonce, CSRF, User, Tenant, Grant string
	Expiry                                                time.Time
}
type accessClaims struct {
	Subject  string   `json:"sub"`
	Audience []string `json:"aud"`
	Client   string   `json:"client_id"`
	Scopes   []string `json:"scp"`
	Ext      struct {
		Principal string `json:"principal_id"`
		Type      string `json:"principal_type"`
		Tenant    string `json:"tenant_id"`
		Grant     string `json:"business_context_id"`
		Member    string `json:"membership_id"`
		Session   string `json:"session_id"`
	} `json:"ext"`
}

func (a *App) verifyAccess(ctx context.Context, raw, subject string) (accessClaims, error) {
	var claims accessClaims
	verified, err := a.Provider.Verifier(&oidc.Config{ClientID: Audience, SupportedSigningAlgs: []string{"RS256"}}).Verify(a.ctx(ctx), raw)
	if err != nil {
		return claims, errors.New("access token rejected")
	}
	if err = verified.Claims(&claims); err != nil {
		return claims, err
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != Audience || claims.Client != a.Client || claims.Subject != subject || claims.Ext.Principal != subject || claims.Ext.Type != "user" {
		return claims, store.ErrDenied
	}
	allowed := false
	for _, scope := range claims.Scopes {
		if scope == "demo:read" {
			allowed = true
		}
	}
	if !allowed {
		return claims, store.ErrDenied
	}
	for _, value := range []string{claims.Ext.Tenant, claims.Ext.Grant, claims.Ext.Member, claims.Ext.Session, subject} {
		if _, err = store.ParseID(value); err != nil {
			return claims, store.ErrDenied
		}
	}
	user, _ := store.ParseID(subject)
	tenant, _ := store.ParseID(claims.Ext.Tenant)
	grant, _ := store.ParseID(claims.Ext.Grant)
	if err = a.Store.GrantCurrent(ctx, grant, user, tenant, a.Client); err != nil {
		return claims, err
	}
	return claims, nil
}
func (a *App) verifyID(ctx context.Context, raw, nonce string) (string, error) {
	id, err := a.Provider.Verifier(&oidc.Config{ClientID: a.Client, SupportedSigningAlgs: []string{"RS256"}}).Verify(a.ctx(ctx), raw)
	if err != nil {
		return "", errors.New("ID token rejected")
	}
	if subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 {
		return "", store.ErrDenied
	}
	var claims struct {
		ACR string   `json:"acr"`
		AMR []string `json:"amr"`
		SID string   `json:"sid"`
	}
	if id.Claims(&claims) != nil || claims.ACR != "urn:local:mfa" || claims.SID == "" {
		return "", store.ErrDenied
	}
	pwd, mfa := false, false
	for _, amr := range claims.AMR {
		if amr == "pwd" {
			pwd = true
		}
		if amr == "otp" || amr == "recovery" {
			mfa = true
		}
	}
	if !pwd || !mfa {
		return "", store.ErrDenied
	}
	return id.Subject, nil
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	var rateMu sync.Mutex
	var start = time.Now()
	attempts := 0
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		rateMu.Lock()
		if time.Since(start) >= 10*time.Minute {
			start = time.Now()
			attempts = 0
		}
		attempts++
		limited := attempts > 120
		rateMu.Unlock()
		if limited {
			fail(w, 429)
			return
		}
		for k, values := range r.URL.Query() {
			if (k != "force" && k != "silent") || len(values) != 1 || values[0] != "1" {
				fail(w, 400)
				return
			}
		}
		state, err := token()
		if err != nil {
			fail(w, 503)
			return
		}
		nonce, err := token()
		if err != nil {
			fail(w, 503)
			return
		}
		verifier := oauth2.GenerateVerifier()
		browser := cookie(r, browserCookie)
		if browser == nil {
			value, err := token()
			if err != nil {
				fail(w, 503)
				return
			}
			setCookie(w, browserCookie, value, 8*3600)
			browser = hash(value)
		}
		payload, err := a.seal(transaction{verifier, nonce}, "state:"+a.Client+":"+state)
		if err != nil {
			fail(w, 503)
			return
		}
		if err = a.Store.CreateBFFTransaction(r.Context(), hash(state), browser, a.Client, payload); err != nil {
			fail(w, 503)
			return
		}
		options := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce), oauth2.SetAuthURLParam("audience", Audience)}
		if r.URL.Query().Get("force") == "1" {
			options = append(options, oauth2.SetAuthURLParam("max_age", "0"))
		}
		if r.URL.Query().Get("silent") == "1" {
			options = append(options, oauth2.SetAuthURLParam("prompt", "none"))
		}
		http.Redirect(w, r, a.OAuth.AuthCodeURL(state, options...), 303)
	})
	mux.HandleFunc("GET /oidc/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if len(q["state"]) != 1 || len(q["code"]) > 1 || len(q["error"]) > 1 || q.Get("state") == "" {
			fail(w, 403)
			return
		}
		state := q.Get("state")
		payload, err := a.Store.ConsumeBFFTransaction(r.Context(), hash(state), cookie(r, browserCookie), a.Client)
		if err != nil {
			fail(w, 403)
			return
		}
		var data transaction
		if a.open(payload, "state:"+a.Client+":"+state, &data) != nil {
			fail(w, 403)
			return
		}
		if q.Get("error") != "" || q.Get("code") == "" {
			fail(w, 401)
			return
		}
		exchanged, err := a.OAuth.Exchange(a.ctx(r.Context()), q.Get("code"), oauth2.VerifierOption(data.Verifier))
		if err != nil {
			fail(w, 401)
			return
		}
		rawID, _ := exchanged.Extra("id_token").(string)
		subject, err := a.verifyID(r.Context(), rawID, data.Nonce)
		if err != nil {
			fail(w, 401)
			return
		}
		access, err := a.verifyAccess(r.Context(), exchanged.AccessToken, subject)
		if err != nil {
			fail(w, 401)
			return
		}
		session, err := token()
		if err != nil {
			fail(w, 503)
			return
		}
		csrf, err := token()
		if err != nil {
			fail(w, 503)
			return
		}
		tokens := Tokens{exchanged.AccessToken, exchanged.RefreshToken, rawID, data.Nonce, csrf, subject, access.Ext.Tenant, access.Ext.Grant, exchanged.Expiry}
		cipher, err := a.seal(tokens, "session:"+a.Client+":"+base64.RawURLEncoding.EncodeToString(hash(session)))
		if err != nil {
			fail(w, 503)
			return
		}
		user, _ := store.ParseID(subject)
		grant, _ := store.ParseID(access.Ext.Grant)
		if err = a.Store.CreateBFFSession(r.Context(), hash(session), a.Client, user, grant, cipher); err != nil {
			fail(w, 503)
			return
		}
		setCookie(w, sessionCookie, session, 8*3600)
		http.Redirect(w, r, "/", 303)
	})
	load := func(w http.ResponseWriter, r *http.Request) (Tokens, []byte, bool) {
		digest := cookie(r, sessionCookie)
		entry, err := a.Store.BFFSession(r.Context(), digest, a.Client)
		if err != nil {
			fail(w, 401)
			return Tokens{}, nil, false
		}
		var tokens Tokens
		if a.open(entry.Tokens, "session:"+a.Client+":"+base64.RawURLEncoding.EncodeToString(digest), &tokens) != nil || tokens.User != entry.User.String() || tokens.Grant != entry.Grant.String() {
			fail(w, 401)
			return tokens, nil, false
		}
		tenant, err := store.ParseID(tokens.Tenant)
		if err != nil || a.Store.GrantCurrent(r.Context(), entry.Grant, entry.User, tenant, a.Client) != nil {
			setCookie(w, sessionCookie, "", -1)
			fail(w, 401)
			return tokens, nil, false
		}
		if r.Method == "POST" && subtle.ConstantTimeCompare([]byte(tokens.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
			fail(w, 403)
			return tokens, nil, false
		}
		return tokens, digest, true
	}
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, r *http.Request) {
		tokens, _, ok := load(w, r)
		if !ok {
			return
		}
		respond(w, map[string]any{"csrf": tokens.CSRF, "application": a.Name, "expires_at": tokens.Expiry})
	})
	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		tokens, _, ok := load(w, r)
		if !ok {
			return
		}
		if r.Header.Get("X-Tenant-ID") != "" && r.Header.Get("X-Tenant-ID") != tokens.Tenant {
			fail(w, 403)
			return
		}
		access, err := a.verifyAccess(r.Context(), tokens.Access, tokens.User)
		if err != nil {
			fail(w, 401)
			return
		}
		respond(w, map[string]string{"user_id": tokens.User, "tenant_id": access.Ext.Tenant, "application": a.Name})
	})
	mux.HandleFunc("POST /session/refresh", func(w http.ResponseWriter, r *http.Request) {
		tokens, digest, ok := load(w, r)
		if !ok {
			return
		}
		err := a.Store.RefreshBFF(r.Context(), digest, a.Client, func(entry store.BFFSession) ([]byte, error) {
			var current Tokens
			if err := a.open(entry.Tokens, "session:"+a.Client+":"+base64.RawURLEncoding.EncodeToString(digest), &current); err != nil {
				return nil, err
			}
			if subtle.ConstantTimeCompare([]byte(current.CSRF), []byte(tokens.CSRF)) != 1 {
				return nil, store.ErrDenied
			}
			next, err := a.OAuth.TokenSource(a.ctx(r.Context()), &oauth2.Token{AccessToken: current.Access, RefreshToken: current.Refresh, Expiry: time.Now().Add(-time.Minute)}).Token()
			if err != nil {
				return nil, errors.New("refresh failed")
			}
			access, err := a.verifyAccess(r.Context(), next.AccessToken, current.User)
			if err != nil {
				return nil, err
			}
			if access.Ext.Tenant != current.Tenant || access.Ext.Grant != current.Grant {
				return nil, store.ErrDenied
			}
			if rawID, ok := next.Extra("id_token").(string); ok && rawID != "" {
				subject, err := a.verifyID(r.Context(), rawID, current.Nonce)
				if err != nil || subject != current.User {
					return nil, store.ErrDenied
				}
				current.ID = rawID
			}
			current.Access = next.AccessToken
			current.Refresh = next.RefreshToken
			current.Expiry = next.Expiry
			return a.seal(current, "session:"+a.Client+":"+base64.RawURLEncoding.EncodeToString(digest))
		})
		if err != nil {
			setCookie(w, sessionCookie, "", -1)
			fail(w, 401)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /session/logout", func(w http.ResponseWriter, r *http.Request) {
		_, digest, ok := load(w, r)
		if !ok {
			return
		}
		if err := a.Store.RevokeBFF(r.Context(), digest, a.Client); err != nil {
			fail(w, 503)
			return
		}
		setCookie(w, sessionCookie, "", -1)
		w.WriteHeader(204)
	})
	mux.Handle("GET /", a.assets())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		origin, _ := url.Parse(a.Origin)
		if r.Host != origin.Host {
			fail(w, 403)
			return
		}
		if r.URL.RawQuery != "" && r.URL.Path != "/oidc/callback" && r.URL.Path != "/login" {
			fail(w, 400)
			return
		}
		if r.Method == "POST" && (r.Header.Get("Origin") != a.Origin || r.Header.Get("Sec-Fetch-Site") == "cross-site") {
			fail(w, 403)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
func fail(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"code":"session_unavailable"}`))
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

// Keep response reads bounded in any future resource integration.
