package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/lth2015/sso/internal/mfa"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

func replacementAAD(f store.FactorChange) string {
	return "replacement:" + f.User.String() + ":" + f.ID.String()
}
func (l *Login) startFactorChange(ctx context.Context, f store.FactorChange, browser, session, csrf []byte, password, code string) (string, error) {
	if f.State != "pending" {
		return "", store.ErrDenied
	}
	proof, err := l.Account.checkUserPassword(ctx, f.User, password)
	if errors.Is(err, store.ErrDenied) {
		return "", l.Account.Store.FailFactorChange(ctx, f.ID, browser, session, csrf)
	}
	if err != nil {
		return "", err
	}
	factor, err := l.Account.Store.Factor(ctx, f.User)
	if err != nil {
		return "", err
	}
	plain, err := openSeal(l.MFAKey, factor.Cipher, "factor:"+f.User.String())
	if err != nil {
		return "", err
	}
	defer clear(plain)
	step, ok := mfa.Match(string(plain), code, time.Now().Unix(), factor.LastStep)
	if !ok || !factor.Active {
		return "", l.Account.Store.FailFactorChange(ctx, f.ID, browser, session, csrf)
	}
	secret, err := mfa.NewSecret()
	if err != nil {
		return "", err
	}
	encrypted, err := seal(l.MFAKey, []byte(secret), replacementAAD(f))
	if err != nil {
		return "", err
	}
	if err = l.Account.Store.StageFactorChange(ctx, f.ID, browser, session, csrf, proof, factor.Version, step, encrypted); err != nil {
		return "", err
	}
	return secret, nil
}
func (l *Login) confirmFactorChange(ctx context.Context, f store.FactorChange, browser, session, csrf []byte, code string) ([]string, error) {
	if f.State != "enrolling" {
		return nil, store.ErrDenied
	}
	plain, err := openSeal(l.MFAKey, f.Pending, replacementAAD(f))
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	step, ok := mfa.Match(string(plain), code, time.Now().Unix(), -1)
	if !ok {
		return nil, l.Account.Store.FailFactorChange(ctx, f.ID, browser, session, csrf)
	}
	encrypted, err := seal(l.MFAKey, plain, "factor:"+f.User.String())
	if err != nil {
		return nil, err
	}
	codes, digests, err := newRecoveryCodeSet(f.User)
	if err != nil {
		return nil, err
	}
	if err = l.Account.Store.CommitFactorChange(ctx, f.ID, browser, session, csrf, f.Pending, encrypted, step, digests); err != nil {
		return nil, err
	}
	return codes, nil
}
func (l *Login) mountAuthenticator(mux *http.ServeMux) {
	page, err := template.ParseFS(web, "web/authenticator.html")
	if err != nil {
		panic("invalid authenticator template")
	}
	mux.HandleFunc("GET /sso/security/authenticator", func(w http.ResponseWriter, r *http.Request) {
		csrf, err := randomToken()
		if err != nil {
			flowError(w, err)
			return
		}
		f, err := l.Account.Store.BeginFactorChange(r.Context(), cookieDigest(r, SessionCookie), cookieDigest(r, BrowserCookie), digest(csrf))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err != nil {
			if errors.Is(err, store.ErrDenied) {
				w.WriteHeader(401)
				page.Execute(w, map[string]string{"State": "login"})
				return
			}
			flowError(w, err)
			return
		}
		page.Execute(w, map[string]string{"State": "ready", "ID": f.ID.String(), "CSRF": csrf})
	})
	mux.HandleFunc("GET /sso/authenticator.js", func(w http.ResponseWriter, r *http.Request) {
		content, err := web.ReadFile("web/authenticator.js")
		if err != nil {
			flowError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(content)
	})
	bound := func(w http.ResponseWriter, r *http.Request) (store.FactorChange, []byte, []byte, []byte, bool) {
		id, err := store.ParseID(r.PathValue("id"))
		if err != nil {
			flowError(w, store.ErrDenied)
			return store.FactorChange{}, nil, nil, nil, false
		}
		values := r.Header.Values("X-CSRF-Token")
		if len(values) != 1 {
			flowError(w, store.ErrDenied)
			return store.FactorChange{}, nil, nil, nil, false
		}
		if _, err = tokenDigest(values[0]); err != nil {
			flowError(w, store.ErrDenied)
			return store.FactorChange{}, nil, nil, nil, false
		}
		browser, session, csrf := cookieDigest(r, BrowserCookie), cookieDigest(r, SessionCookie), digest(values[0])
		f, err := l.Account.Store.FactorChange(r.Context(), id, browser, session, csrf)
		if err != nil {
			flowError(w, err)
			return f, nil, nil, nil, false
		}
		return f, browser, session, csrf, true
	}
	allowed := func(w http.ResponseWriter, f store.FactorChange) bool {
		if !l.Limit.Allow("security:"+f.User.String(), 5, time.Hour) {
			w.Header().Set("Retry-After", "3600")
			writeError(w, 429, "rate_limited")
			return false
		}
		return true
	}
	mux.HandleFunc("POST /v1/sso/authenticator/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Password string `json:"password"`
			Code     string `json:"code"`
		}
		if !decodeAuthenticator(w, r, &input) {
			return
		}
		f, browser, session, csrf, ok := bound(w, r)
		if !ok {
			return
		}
		if f.State != "pending" {
			flowError(w, store.ErrDenied)
			return
		}
		if !allowed(w, f) {
			return
		}
		secret, err := l.startFactorChange(r.Context(), f, browser, session, csrf, input.Password, input.Code)
		if err != nil {
			flowError(w, err)
			return
		}
		jsonResponse(w, map[string]string{"state": "enrolling", "secret": secret})
	})
	mux.HandleFunc("POST /v1/sso/authenticator/{id}/confirm", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Code string `json:"code"`
		}
		if !decodeAuthenticator(w, r, &input) {
			return
		}
		f, browser, session, csrf, ok := bound(w, r)
		if !ok {
			return
		}
		if f.State != "enrolling" {
			flowError(w, store.ErrDenied)
			return
		}
		if !allowed(w, f) {
			return
		}
		codes, err := l.confirmFactorChange(r.Context(), f, browser, session, csrf, input.Code)
		if err != nil {
			flowError(w, err)
			return
		}
		setLocalCookie(w, SessionCookie, "", -1)
		jsonResponse(w, map[string]any{"recovery_codes": codes, "sessions_revoked": true})
	})
	mux.HandleFunc("POST /v1/sso/authenticator/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		var input struct{}
		if !decodeAuthenticator(w, r, &input) {
			return
		}
		f, browser, session, csrf, ok := bound(w, r)
		if !ok {
			return
		}
		if err := l.Account.Store.CancelFactorChange(r.Context(), f.ID, browser, session, csrf); err != nil {
			flowError(w, err)
			return
		}
		w.WriteHeader(204)
	})
}

// Authenticator bodies are flat objects with exact, unique string fields.
func decodeAuthenticator(w http.ResponseWriter, r *http.Request, value any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	valid := err == nil && utf8.Valid(body)
	d := json.NewDecoder(bytes.NewReader(body))
	first, err := d.Token()
	valid = valid && err == nil && first == json.Delim('{')
	seen := map[string]bool{}
	for valid && d.More() {
		key, e := d.Token()
		name, ok := key.(string)
		if e != nil || !ok || seen[name] || (name != "password" && name != "code") {
			valid = false
			break
		}
		seen[name] = true
		token, e := d.Token()
		_, ok = token.(string)
		valid = e == nil && ok
	}
	if valid {
		last, e := d.Token()
		var extra any
		valid = e == nil && last == json.Delim('}') && d.Decode(&extra) == io.EOF
	}
	if valid {
		d = json.NewDecoder(bytes.NewReader(body))
		d.DisallowUnknownFields()
		valid = d.Decode(value) == nil
	}
	if !valid {
		writeError(w, 400, "invalid_request")
	}
	return valid
}
