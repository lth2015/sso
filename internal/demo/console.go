package demo

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	contract "github.com/lth2015/sso/api"
	"github.com/lth2015/sso/internal/consoleui"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"golang.org/x/oauth2"
	"io"
	"mime"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

const consoleCookiePrefix = "local_console_session_"

func (a *App) sessionCookieName(r *http.Request) (string, bool) {
	if a.Client != "local-console" {
		return sessionCookie, true
	}
	values := r.Header.Values("X-Console-Context")
	if len(values) != 1 {
		return "", false
	}
	value := values[0]
	id, err := store.ParseID(value)
	if err != nil || id.String() != value {
		return "", false
	}
	return consoleCookiePrefix + value, true
}
func (a *App) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	if name, ok := a.sessionCookieName(r); ok {
		setCookie(w, name, "", -1)
	}
}
func (a *App) limitContextCookies(w http.ResponseWriter, r *http.Request) {
	names := []string{}
	for _, c := range r.Cookies() {
		if strings.HasPrefix(c.Name, consoleCookiePrefix) {
			names = append(names, c.Name)
		}
	}
	sort.Strings(names)
	for len(names) >= 20 {
		setCookie(w, names[0], "", -1)
		names = names[1:]
	}
}

type consoleRoute struct {
	Method  string
	Pattern *regexp.Regexp
}

var consoleRoutes = func() []consoleRoute {
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if json.Unmarshal(contract.PublicJSON, &spec) != nil {
		panic("invalid embedded API contract")
	}
	result := []consoleRoute{}
	for path, methods := range spec.Paths {
		if !strings.HasPrefix(path, "/v1/") {
			continue
		}
		pattern := regexp.QuoteMeta(path)
		for _, name := range []string{"tenant", "member", "org", "invitation", "session", "role", "application"} {
			pattern = strings.ReplaceAll(pattern, regexp.QuoteMeta("{"+name+"}"), "[a-f0-9]{32}")
		}
		for method := range methods {
			result = append(result, consoleRoute{strings.ToUpper(method), regexp.MustCompile("^" + pattern + "$")})
		}
	}
	return result
}()

func consoleRouteAllowed(method, path string) bool {
	for _, v := range consoleRoutes {
		if v.Method == method && v.Pattern.MatchString(path) {
			return true
		}
	}
	return false
}
func (a *App) mountConsole(mux *http.ServeMux, load func(http.ResponseWriter, *http.Request) (Tokens, []byte, bool)) {
	client := &http.Client{Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 8 * time.Second}, Timeout: 9 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	mux.HandleFunc("GET /bff-openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(contract.ConsoleSpec)
	})
	mux.HandleFunc("GET /platform-openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(contract.PublicJSON)
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if !consoleui.Ready() {
			fail(w, 503)
			return
		}
		w.WriteHeader(204)
	})
	proxyHandler := func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/bff")
		if r.URL.RawPath != "" || !consoleRouteAllowed(r.Method, path) {
			fail(w, 404)
			return
		}
		if len(r.Header.Values("Authorization")) > 0 {
			fail(w, 400)
			return
		}
		tokens, digest, ok := load(w, r)
		if !ok {
			return
		}
		if hint := r.Header.Get("X-Tenant-ID"); hint != "" && hint != tokens.Tenant {
			fail(w, 403)
			return
		}
		// Refresh before forwarding. Never replay a business mutation after any response.
		if tokens.Expiry.Before(time.Now().Add(15 * time.Second)) {
			err := a.Store.RefreshBFF(r.Context(), digest, a.Client, func(entry store.BFFSession) ([]byte, error) {
				current, err := a.consoleRefresh(r, entry, digest)
				if err != nil {
					return nil, err
				}
				tokens = current
				return a.seal(current, a.sessionAAD(digest))
			})
			if err != nil {
				a.clearSessionCookie(w, r)
				fail(w, 401)
				return
			}
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if err != nil {
			fail(w, 413)
			return
		}
		if r.Method != "GET" {
			kind, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if e != nil || kind != "application/json" {
				fail(w, 415)
				return
			}
		}
		target := "http://platform-api:8083" + path
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		upstream, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
		if err != nil {
			fail(w, 400)
			return
		}
		upstream.Host = "api.localhost:8083"
		upstream.Header.Set("Authorization", "Bearer "+tokens.Access)
		for _, name := range []string{"Content-Type", "If-Match"} {
			if values := r.Header.Values(name); len(values) == 1 {
				upstream.Header.Set(name, values[0])
			} else if len(values) > 1 {
				fail(w, 400)
				return
			}
		}
		response, err := client.Do(upstream)
		if err != nil {
			fail(w, 503)
			return
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 1_048_577))
		if err != nil || len(data) > 1_048_576 {
			fail(w, 502)
			return
		}
		if response.StatusCode != 204 {
			kind, _, e := mime.ParseMediaType(response.Header.Get("Content-Type"))
			if e != nil || (kind != "application/json" && kind != "application/problem+json") || !json.Valid(data) {
				fail(w, 502)
				return
			}
		}
		// Only a public-contract JSON response and non-secret metadata cross the BFF.
		for _, name := range []string{"Content-Type", "ETag", "X-Request-ID", "Retry-After"} {
			if value := response.Header.Get(name); value != "" {
				w.Header().Set(name, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		w.Write(data)
	}
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		mux.HandleFunc(method+" /bff/", proxyHandler)
	}
}
func (a *App) sessionAAD(digest []byte) string {
	return "session:" + a.Client + ":" + base64.RawURLEncoding.EncodeToString(digest)
}
func (a *App) consoleRefresh(r *http.Request, entry store.BFFSession, digest []byte) (Tokens, error) {
	var current Tokens
	if err := a.open(entry.Tokens, a.sessionAAD(digest), &current); err != nil {
		return current, err
	}
	next, err := a.OAuth.TokenSource(a.ctx(r.Context()), &oauth2.Token{AccessToken: current.Access, RefreshToken: current.Refresh, Expiry: time.Now().Add(-time.Minute)}).Token()
	if err != nil {
		return current, errors.New("refresh failed")
	}
	access, err := a.verifyAccess(r.Context(), next.AccessToken, current.User)
	if err != nil || access.Ext.Grant != current.Grant || access.Ext.Tenant != current.Tenant {
		return current, store.ErrDenied
	}
	if rawID, ok := next.Extra("id_token").(string); ok && rawID != "" {
		subject, e := a.verifyID(r.Context(), rawID, current.Nonce)
		if e != nil || subject != current.User {
			return current, store.ErrDenied
		}
		current.ID = rawID
	}
	current.Access = next.AccessToken
	current.Refresh = next.RefreshToken
	current.Expiry = next.Expiry
	return current, nil
}
