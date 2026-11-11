package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"html/template"
	"net/http"
	"time"

	contract "github.com/lth2015/sso/api"
	"github.com/lth2015/sso/internal/mfa"
	"github.com/lth2015/sso/internal/password"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

func (s *Service) checkUserPassword(ctx context.Context, user store.ID, value string) (store.PasswordCredential, error) {
	select {
	case s.pool <- struct{}{}:
		defer func() { <-s.pool }()
	default:
		return store.PasswordCredential{}, ErrBusy
	}
	proof, err := s.Store.PasswordCredentialForUser(ctx, user)
	encoded := s.dummy
	if err == nil {
		encoded = proof.Hash
	}
	valid := password.Verify(encoded, value)
	if err != nil && !errors.Is(err, store.ErrDenied) {
		return store.PasswordCredential{}, err
	}
	if err != nil || !valid {
		return store.PasswordCredential{}, store.ErrDenied
	}
	return proof, nil
}
func (l *Login) regenerateRecovery(ctx context.Context, f store.SecurityAction, browser, session, csrf []byte, value, code string) ([]string, error) {
	proof, err := l.Account.checkUserPassword(ctx, f.User, value)
	if errors.Is(err, store.ErrDenied) {
		return nil, l.Account.Store.FailSecurityAction(ctx, f.ID, browser, session, csrf)
	}
	if err != nil {
		return nil, err
	}
	factor, err := l.Account.Store.Factor(ctx, f.User)
	if err != nil {
		return nil, err
	}
	plain, err := openSeal(l.MFAKey, factor.Cipher, "factor:"+f.User.String())
	if err != nil {
		return nil, err
	}
	step, ok := mfa.Match(string(plain), code, time.Now().Unix(), factor.LastStep)
	if !ok || !factor.Active {
		return nil, l.Account.Store.FailSecurityAction(ctx, f.ID, browser, session, csrf)
	}
	codes := []string{}
	digests := [][]byte{}
	for range 10 {
		raw := make([]byte, 16)
		if _, err = rand.Read(raw); err != nil {
			return nil, err
		}
		v := hex.EncodeToString(raw)
		v = v[:8] + "-" + v[8:16] + "-" + v[16:24] + "-" + v[24:]
		d, e := recoveryDigest(f.User, v)
		if e != nil {
			return nil, e
		}
		codes = append(codes, v)
		digests = append(digests, d)
	}
	if err = l.Account.Store.RegenerateRecovery(ctx, f.ID, browser, session, csrf, proof, factor.Version, step, digests); err != nil {
		return nil, err
	}
	return codes, nil
}
func (l *Login) mountSecurity(mux *http.ServeMux) {
	page, err := template.ParseFS(web, "web/security.html")
	if err != nil {
		panic("invalid security template")
	}
	mux.HandleFunc("GET /sso/security", func(w http.ResponseWriter, r *http.Request) {
		csrf, err := randomToken()
		if err != nil {
			flowError(w, err)
			return
		}
		f, err := l.Account.Store.BeginSecurityAction(r.Context(), cookieDigest(r, SessionCookie), cookieDigest(r, BrowserCookie), digest(csrf))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err != nil {
			if errors.Is(err, store.ErrDenied) {
				w.WriteHeader(401)
				_ = page.Execute(w, map[string]string{"State": "login"})
				return
			}
			flowError(w, err)
			return
		}
		_ = page.Execute(w, map[string]string{"State": "ready", "ID": f.ID.String(), "CSRF": csrf})
	})
	for _, asset := range []string{"security.js", "security.css"} {
		mux.HandleFunc("GET /sso/"+asset, func(w http.ResponseWriter, r *http.Request) {
			content, err := web.ReadFile("web/" + asset)
			if err != nil {
				flowError(w, err)
				return
			}
			kind := "text/javascript; charset=utf-8"
			if asset == "security.css" {
				kind = "text/css; charset=utf-8"
			}
			w.Header().Set("Content-Type", kind)
			w.Write(content)
		})
	}
	mux.HandleFunc("GET /sso/security/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(contract.SecuritySpec)
	})
	mux.HandleFunc("POST /v1/sso/recovery/regenerate/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Password string `json:"password"`
			Code     string `json:"code"`
		}
		if !decode(w, r, &input) {
			return
		}
		id, err := store.ParseID(r.PathValue("id"))
		if err != nil {
			flowError(w, store.ErrDenied)
			return
		}
		values := r.Header.Values("X-CSRF-Token")
		if len(values) != 1 {
			flowError(w, store.ErrDenied)
			return
		}
		if _, err = tokenDigest(values[0]); err != nil {
			flowError(w, store.ErrDenied)
			return
		}
		browser, session, csrf := cookieDigest(r, BrowserCookie), cookieDigest(r, SessionCookie), digest(values[0])
		f, err := l.Account.Store.SecurityAction(r.Context(), id, browser, session, csrf)
		if err != nil {
			flowError(w, err)
			return
		}
		if !l.Limit.Allow("security:"+f.User.String(), 5, time.Hour) {
			w.Header().Set("Retry-After", "3600")
			writeError(w, 429, "rate_limited")
			return
		}
		codes, err := l.regenerateRecovery(r.Context(), f, browser, session, csrf, input.Password, input.Code)
		if err != nil {
			flowError(w, err)
			return
		}
		setLocalCookie(w, SessionCookie, "", -1)
		jsonResponse(w, map[string]any{"recovery_codes": codes, "sessions_revoked": true})
	})
}
