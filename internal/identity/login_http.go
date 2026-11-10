package identity

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lth2015/sso/internal/adapter/hydra"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"html/template"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"
)

func (l *Login) Handler() http.Handler {
	mux := http.NewServeMux()
	page, err := template.ParseFS(web, "web/login.html")
	if err != nil {
		panic("invalid login template")
	}
	render := func(w http.ResponseWriter, id, csrf, kind string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = page.Execute(w, map[string]string{"ID": id, "CSRF": csrf, "Kind": kind})
	}
	for _, kind := range []string{"login", "consent"} {
		mux.HandleFunc("GET /"+kind, func(w http.ResponseWriter, r *http.Request) {
			values := r.URL.Query()
			if len(values) != 1 || len(values[kind+"_challenge"]) != 1 {
				writeError(w, 400, "invalid_flow")
				return
			}
			browser := cookieDigest(r, BrowserCookie)
			if browser == nil {
				value, err := randomToken()
				if err != nil {
					writeError(w, 503, "temporarily_unavailable")
					return
				}
				setLocalCookie(w, BrowserCookie, value, 8*3600)
				browser = digest(value)
			}
			csrf, err := randomToken()
			if err != nil {
				writeError(w, 503, "temporarily_unavailable")
				return
			}
			f, direct, err := l.Start(r.Context(), kind, values.Get(kind+"_challenge"), browser, digest(csrf), cookieDigest(r, SessionCookie))
			if err != nil {
				flowError(w, err)
				return
			}
			if direct {
				http.Redirect(w, r, f.State, http.StatusSeeOther)
				return
			}
			if f.State == "passed" {
				redirect, session, err := l.CompleteLogin(r.Context(), f, browser)
				if err != nil {
					flowError(w, err)
					return
				}
				if session != "" {
					setLocalCookie(w, SessionCookie, session, 24*3600)
				}
				http.Redirect(w, r, redirect, http.StatusSeeOther)
				return
			}
			render(w, f.ID.String(), csrf, "flow")
		})
	}
	mux.HandleFunc("GET /flow", func(w http.ResponseWriter, r *http.Request) { render(w, "", "", "expired") })
	for _, asset := range []string{"login.js", "login.css"} {
		mux.HandleFunc("GET /flow/"+asset, func(w http.ResponseWriter, r *http.Request) {
			body, err := web.ReadFile("web/" + asset)
			if err != nil {
				writeError(w, 503, "temporarily_unavailable")
				return
			}
			kind := "text/javascript; charset=utf-8"
			if asset == "login.css" {
				kind = "text/css; charset=utf-8"
			}
			w.Header().Set("Content-Type", kind)
			_, _ = w.Write(body)
		})
	}
	bound := func(w http.ResponseWriter, r *http.Request) (store.Flow, bool) {
		id, err := store.ParseID(r.PathValue("id"))
		if err != nil {
			flowError(w, store.ErrDenied)
			return store.Flow{}, false
		}
		var csrf []byte
		if r.Method == "POST" {
			value := r.Header.Get("X-CSRF-Token")
			if _, err := tokenDigest(value); err != nil {
				flowError(w, store.ErrDenied)
				return store.Flow{}, false
			}
			csrf = digest(value)
		}
		f, err := l.Account.Store.Flow(r.Context(), id, cookieDigest(r, BrowserCookie), csrf)
		if err != nil {
			flowError(w, err)
			return f, false
		}
		if _, err = l.Account.Store.OAuthClient(r.Context(), f.Client); err != nil {
			flowError(w, err)
			return f, false
		}
		return f, true
	}
	mux.HandleFunc("GET /v1/flows/{id}", func(w http.ResponseWriter, r *http.Request) {
		f, ok := bound(w, r)
		if !ok {
			return
		}
		client, err := l.Account.Store.OAuthClient(r.Context(), f.Client)
		if err != nil {
			flowError(w, err)
			return
		}
		response := map[string]any{"state": f.State, "application": client.Name, "scopes": strings.Fields(f.Scopes)}
		if f.State == "consent" {
			session, err := l.Account.Store.SSOSession(r.Context(), cookieDigest(r, SessionCookie))
			if err != nil || session.ID != f.Session {
				flowError(w, store.ErrDenied)
				return
			}
			options, err := l.Account.Store.TenantOptions(r.Context(), f.User, f.Session, f.Client)
			if err != nil {
				flowError(w, err)
				return
			}
			items := []map[string]string{}
			for _, o := range options {
				items = append(items, map[string]string{"id": o.ID.String(), "name": o.Name})
			}
			response["tenants"] = items
		}
		jsonResponse(w, response)
	})
	mux.HandleFunc("POST /v1/flows/{id}/password", func(w http.ResponseWriter, r *http.Request) {
		f, ok := bound(w, r)
		if !ok {
			return
		}
		var input struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if !decode(w, r, &input) {
			return
		}
		email, err := store.NormalizeEmail(input.Email)
		if err != nil {
			email = input.Email
		}
		if !l.Limit.Allow("password:"+hashAlias(email), 10, 10*time.Minute) {
			writeError(w, 429, "rate_limited")
			return
		}
		state, secret, err := l.Password(r.Context(), f, cookieDigest(r, BrowserCookie), input.Email, input.Password)
		if err != nil {
			flowError(w, err)
			return
		}
		response := map[string]any{"state": state}
		if secret != "" {
			response["secret"] = secret
		}
		jsonResponse(w, response)
	})
	mux.HandleFunc("POST /v1/flows/{id}/mfa", func(w http.ResponseWriter, r *http.Request) {
		f, ok := bound(w, r)
		if !ok {
			return
		}
		var input struct {
			Code     string `json:"code"`
			Recovery string `json:"recovery_code"`
		}
		if !decode(w, r, &input) {
			return
		}
		if !l.Limit.Allow("mfa:"+f.User.String(), 10, time.Minute) {
			writeError(w, 429, "rate_limited")
			return
		}
		codes, err := l.MFA(r.Context(), f, cookieDigest(r, BrowserCookie), input.Code, input.Recovery)
		if err != nil {
			flowError(w, err)
			return
		}
		jsonResponse(w, map[string]any{"state": "passed", "recovery_codes": codes})
	})
	mux.HandleFunc("POST /v1/flows/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		f, ok := bound(w, r)
		if !ok {
			return
		}
		var input struct{}
		if !decode(w, r, &input) {
			return
		}
		redirect, session, err := l.CompleteLogin(r.Context(), f, cookieDigest(r, BrowserCookie))
		if err != nil {
			flowError(w, err)
			return
		}
		if session != "" {
			setLocalCookie(w, SessionCookie, session, 24*3600)
		}
		jsonResponse(w, map[string]string{"redirect_to": redirect})
	})
	mux.HandleFunc("POST /v1/flows/{id}/consent", func(w http.ResponseWriter, r *http.Request) {
		f, ok := bound(w, r)
		if !ok {
			return
		}
		var input struct {
			Tenant string `json:"tenant_id"`
		}
		if !decode(w, r, &input) {
			return
		}
		tenant, err := store.ParseID(input.Tenant)
		if err != nil {
			flowError(w, store.ErrDenied)
			return
		}
		redirect, err := l.Consent(r.Context(), f, cookieDigest(r, BrowserCookie), cookieDigest(r, SessionCookie), tenant)
		if err != nil {
			flowError(w, err)
			return
		}
		jsonResponse(w, map[string]string{"redirect_to": redirect})
	})
	mux.HandleFunc("POST /v1/flows/{id}/reject", func(w http.ResponseWriter, r *http.Request) {
		f, ok := bound(w, r)
		if !ok {
			return
		}
		var input struct{}
		if !decode(w, r, &input) {
			return
		}
		redirect, err := l.Reject(r.Context(), f, cookieDigest(r, BrowserCookie))
		if err != nil {
			flowError(w, err)
			return
		}
		jsonResponse(w, map[string]string{"redirect_to": redirect})
	})
	mux.HandleFunc("GET /sso/logout", func(w http.ResponseWriter, r *http.Request) {
		csrf, err := randomToken()
		if err != nil {
			flowError(w, err)
			return
		}
		if err = l.Account.Store.LogoutCSRF(r.Context(), cookieDigest(r, SessionCookie), digest(csrf)); err != nil {
			flowError(w, err)
			return
		}
		render(w, "", csrf, "logout")
	})
	mux.HandleFunc("POST /v1/sso/logout", func(w http.ResponseWriter, r *http.Request) {
		var input struct{}
		if !decode(w, r, &input) {
			return
		}
		csrf := r.Header.Get("X-CSRF-Token")
		if _, err := tokenDigest(csrf); err != nil {
			flowError(w, store.ErrDenied)
			return
		}
		_, err := l.Account.Store.LogoutAll(r.Context(), cookieDigest(r, SessionCookie), digest(csrf))
		if err != nil {
			flowError(w, err)
			return
		}
		setLocalCookie(w, SessionCookie, "", -1)
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /sso/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := l.Account.Store.SSOReady(r.Context()); err != nil {
			writeError(w, 503, "not_ready")
			return
		}
		if err := l.Admin.Do(r.Context(), "GET", "/health/ready", nil, nil); err != nil {
			writeError(w, 503, "not_ready")
			return
		}
		w.WriteHeader(204)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		if r.Host != "auth.localhost:8080" {
			writeError(w, 403, "invalid_origin")
			return
		}
		if r.URL.RawQuery != "" && r.URL.Path != "/login" && r.URL.Path != "/consent" {
			writeError(w, 400, "invalid_flow")
			return
		}
		kind, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.Method == "POST" && (r.Header.Get("Origin") != hydra.AuthOrigin || mediaErr != nil || kind != "application/json" || r.Header.Get("Sec-Fetch-Site") == "cross-site") {
			writeError(w, 403, "invalid_origin")
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			writeError(w, 400, "invalid_flow")
			return
		}
		if !l.Limit.Allow("ip:"+host, 300, 10*time.Minute) {
			writeError(w, 429, "rate_limited")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
func flowError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrDenied), errors.Is(err, store.ErrInvalid), errors.Is(err, store.ErrNotFound):
		writeError(w, 403, "authentication_failed")
	case errors.Is(err, store.ErrConflict):
		writeError(w, 409, "flow_conflict")
	default:
		writeError(w, 503, "temporarily_unavailable")
	}
}
func jsonResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
