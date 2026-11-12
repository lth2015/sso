package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const clientID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func requestDemo(d *demo, method, path string, cookie *http.Cookie, body url.Values) *httptest.ResponseRecorder {
	var input string
	if body != nil {
		input = body.Encode()
	}
	r := httptest.NewRequest(method, origin+path, strings.NewReader(input))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if method == "POST" {
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	d.ServeHTTP(w, r)
	return w
}
func sessionDemo(t *testing.T, d *demo) (*http.Cookie, string) {
	t.Helper()
	w := requestDemo(d, "GET", "/", nil, nil)
	cookies := w.Result().Cookies()
	if w.Code != 200 || len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("demo session cookie boundary unavailable")
	}
	return cookies[0], d.sessions[cookies[0].Value].CSRF
}
func TestUserDemoOriginCSRFAndFixedPKCEDestination(t *testing.T) {
	if _, e := newDemo("not-a-client"); e == nil {
		t.Fatal("untrusted client configuration accepted")
	}
	d, _ := newDemo(clientID)
	cookie, csrf := sessionDemo(t, d)
	for _, path := range []string{"/logout", "/login", "/check"} {
		w := requestDemo(d, "POST", path, cookie, url.Values{"csrf": {"wrong"}})
		if w.Code != 403 {
			t.Fatal("demo action admitted missing CSRF")
		}
	}
	r := httptest.NewRequest("POST", "http://outside.invalid/login", strings.NewReader(url.Values{"csrf": {csrf}}.Encode()))
	r.AddCookie(cookie)
	r.Header.Set("Origin", "http://outside.invalid")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	d.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("foreign origin admitted")
	}
	w = requestDemo(d, "POST", "/login", cookie, url.Values{"csrf": {csrf}})
	if w.Code != 303 {
		t.Fatal("valid demo login refused")
	}
	u, e := url.Parse(w.Header().Get("Location"))
	if e != nil || u.Host != "auth.localhost:4464" || u.Query().Get("scope") != "platform:read" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("redirect_uri") != origin+"/callback" || len(u.Query().Get("state")) != 43 {
		t.Fatal("OAuth destination/scopes/PKCE escaped fixed reference")
	}
	if strings.Contains(w.Body.String(), d.sessions[cookie.Value].Verifier) {
		t.Fatal("PKCE verifier escaped to the browser")
	}
}
func TestUserDemoCallbackIsSingleUseAndRotatesBrowserSession(t *testing.T) {
	d, _ := newDemo(clientID)
	cookie, csrf := sessionDemo(t, d)
	requestDemo(d, "POST", "/login", cookie, url.Values{"csrf": {csrf}})
	state := d.sessions[cookie.Value].State
	called := 0
	d.exchange = func(context.Context, string, string) (string, int, error) {
		called++
		return "private-demo-token", 300, nil
	}
	w := requestDemo(d, "GET", "/callback?state="+state+"&code=private-code&scope=platform%3Aread&iss=http%3A%2F%2Fauth.localhost%3A4464%2F", cookie, nil)
	if w.Code != 303 || called != 1 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].Value == cookie.Value {
		t.Fatal("callback did not consume state and rotate session")
	}
	if _, present := d.sessions[cookie.Value]; present {
		t.Fatal("pre-login session survived authentication")
	}
	fresh := w.Result().Cookies()[0]
	replay := requestDemo(d, "GET", "/callback?state="+state+"&code=private-code", fresh, nil)
	if replay.Code != 400 || called != 1 || strings.Contains(replay.Body.String(), "private-code") {
		t.Fatal("OAuth callback replay or raw secret reflection")
	}
}
func TestUserDemoEveryRefreshChecksPrimaryAndRejectsRevokedIdentity(t *testing.T) {
	d, _ := newDemo(clientID)
	cookie, _ := sessionDemo(t, d)
	s := d.sessions[cookie.Value]
	s.Access = "private-current-token"
	s.Expires = time.Now().Unix() + 300
	var calls atomic.Int32
	d.verify = func(_ context.Context, token string) (me, error) {
		if token != "private-current-token" {
			t.Fatal("browser selected the backend bearer")
		}
		if calls.Add(1) == 1 {
			return me{User: clientID, Tenant: clientID, Member: clientID, Session: clientID}, nil
		}
		return me{}, errors.New("private-upstream-failure")
	}
	first := requestDemo(d, "GET", "/", cookie, nil)
	if !strings.Contains(first.Body.String(), "当前准入已确认") {
		t.Fatal("current User not displayed")
	}
	second := requestDemo(d, "GET", "/", cookie, nil)
	if strings.Contains(second.Body.String(), "当前准入已确认") || strings.Contains(second.Body.String(), clientID) || strings.Contains(second.Body.String(), "private-") || calls.Load() != 2 || s.Access != "" {
		t.Fatal("old success/private error cached after primary refusal")
	}
}
func TestUserDemoLateCallbackAndVerificationCannotUndoLogout(t *testing.T) {
	d, _ := newDemo(clientID)
	cookie, csrf := sessionDemo(t, d)
	requestDemo(d, "POST", "/login", cookie, url.Values{"csrf": {csrf}})
	state := d.sessions[cookie.Value].State
	arrived, release := make(chan struct{}), make(chan struct{})
	d.exchange = func(context.Context, string, string) (string, int, error) {
		close(arrived)
		<-release
		return "private-late-token", 300, nil
	}
	var group sync.WaitGroup
	group.Add(1)
	var w *httptest.ResponseRecorder
	go func() {
		defer group.Done()
		w = requestDemo(d, "GET", "/callback?state="+state+"&code=private-code", cookie, nil)
	}()
	<-arrived
	logout := requestDemo(d, "POST", "/logout", cookie, url.Values{"csrf": {csrf}})
	close(release)
	group.Wait()
	if logout.Code != 303 || w.Code != 409 || len(d.sessions) != 0 {
		t.Fatal("late callback restored a logged-out browser session")
	}
	// A separate in-flight primary read must not render old identity after logout.
	cookie, csrf = sessionDemo(t, d)
	s := d.sessions[cookie.Value]
	s.Access = "private-current-token"
	s.Expires = time.Now().Unix() + 300
	arrived, release = make(chan struct{}), make(chan struct{})
	d.verify = func(context.Context, string) (me, error) {
		close(arrived)
		<-release
		return me{User: clientID, Tenant: clientID, Member: clientID, Session: clientID}, nil
	}
	group.Add(1)
	go func() { defer group.Done(); w = requestDemo(d, "GET", "/", cookie, nil) }()
	<-arrived
	requestDemo(d, "POST", "/logout", cookie, url.Values{"csrf": {csrf}})
	close(release)
	group.Wait()
	if strings.Contains(w.Body.String(), clientID) || strings.Contains(w.Body.String(), "当前准入已确认") {
		t.Fatal("late verification rendered identity after logout")
	}
}
func TestUserDemoStrictTokenAndIdentityProjections(t *testing.T) {
	valid := `{"access_token":"private-demo-token","token_type":"bearer","expires_in":900,"scope":"platform:read"}`
	if token, seconds, e := tokenResult([]byte(valid)); e != nil || token != "private-demo-token" || seconds != 300 {
		t.Fatal("valid OAuth result refused")
	}
	for _, s := range []string{strings.Replace(valid, `"scope":"platform:read"`, `"scope":"platform:write"`, 1), strings.Replace(valid, `"token_type":"bearer"`, `"token_type":"bearer","token_type":"bearer"`, 1), strings.Replace(valid, `"expires_in":900`, `"expires_in":null`, 1), valid + ` {}`, strings.Replace(valid, `"scope":"platform:read"`, `"secret_key":"private"`, 1)} {
		if token, _, e := tokenResult([]byte(s)); e == nil || token != "" {
			t.Fatal("invalid/private OAuth response exposed")
		}
	}
	body := `{"user_id":"` + clientID + `","tenant_id":"` + clientID + `","membership_id":"` + clientID + `","session_id":"` + clientID + `"}`
	for _, s := range []string{strings.Replace(body, `"user_id":`, `"User_id":`, 1), strings.Replace(body, `"session_id":`, `"secret_key":`, 1), body + ` {}`} {
		if v, e := meResult([]byte(s)); e == nil || v.User != "" {
			t.Fatal("invalid identity projection returned partial data")
		}
	}
}
