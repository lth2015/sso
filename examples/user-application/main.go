// Loopback-only OAuth code+PKCE demonstration. Identity is read online from the
// verified User API; this example does not use or trust an OIDC ID token.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const origin = "http://127.0.0.1:9876"
const cookieName = "sso_user_app_demo"

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var bearerPattern = regexp.MustCompile(`^[A-Za-z0-9._~+/-]+=*$`)
var errDenied = errors.New("demo authorization unavailable")

type me struct{ User, Tenant, Member, Session string }
type browserSession struct {
	CSRF, State, Verifier, Access string
	Until, Pending, Expires       int64
	Generation                    uint64
}
type demo struct {
	client   string
	mu       sync.Mutex
	sessions map[string]*browserSession
	exchange func(context.Context, string, string) (string, int, error)
	verify   func(context.Context, string) (me, error)
}

func random() string {
	v := make([]byte, 32)
	if _, e := rand.Read(v); e != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(v)
}
func same(a, b string) bool {
	return len(a) > 0 && len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func newDemo(client string) (*demo, error) {
	if !idPattern.MatchString(client) {
		return nil, errDenied
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	httpClient := &http.Client{Timeout: 3 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	d := &demo{client: client, sessions: map[string]*browserSession{}}
	d.exchange = func(ctx context.Context, code, verifier string) (string, int, error) {
		form := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "redirect_uri": {origin + "/callback"}, "code": {code}, "code_verifier": {verifier}}
		r, e := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:4464/oauth2/token", strings.NewReader(form.Encode()))
		if e != nil {
			return "", 0, errDenied
		}
		r.Host = "auth.localhost:4464"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Accept", "application/json")
		body, e := readJSON(httpClient, r)
		if e != nil {
			return "", 0, e
		}
		return tokenResult(body)
	}
	d.verify = func(ctx context.Context, token string) (me, error) {
		r, e := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8083/v1/me", nil)
		if e != nil {
			return me{}, errDenied
		}
		r.Host = "api.localhost:8083"
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Accept", "application/json")
		body, e := readJSON(httpClient, r)
		if e != nil {
			return me{}, e
		}
		return meResult(body)
	}
	return d, nil
}
func noStore(h http.Header) bool {
	for _, line := range h.Values("Cache-Control") {
		for _, v := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(v), "no-store") {
				return true
			}
		}
	}
	return false
}
func readJSON(client *http.Client, r *http.Request) ([]byte, error) {
	r.Header.Set("Accept-Encoding", "identity")
	r.Header.Set("Cache-Control", "no-store")
	response, e := client.Do(r)
	if e != nil {
		return nil, errDenied
	}
	defer response.Body.Close()
	media, _, e := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if e != nil || response.StatusCode != 200 || media != "application/json" || response.Uncompressed || !noStore(response.Header) || len(response.Header.Values("Age")) > 1 || response.Header.Get("Age") != "" && response.Header.Get("Age") != "0" || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return nil, errDenied
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	if e != nil || len(body) > 65536 || !utf8.Valid(body) || r.Context().Err() != nil {
		return nil, errDenied
	}
	return body, nil
}
func object(raw []byte, fields map[string]any, required []string) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	start, e := d.Token()
	if e != nil || start != json.Delim('{') {
		return errDenied
	}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		s, ok := key.(string)
		if e != nil || !ok || seen[s] || fields[s] == nil {
			return errDenied
		}
		seen[s] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, fields[s]) != nil {
			return errDenied
		}
	}
	end, e := d.Token()
	if e != nil || end != json.Delim('}') {
		return errDenied
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errDenied
	}
	for _, s := range required {
		if !seen[s] {
			return errDenied
		}
	}
	return nil
}
func tokenResult(raw []byte) (string, int, error) {
	var token, kind, scope string
	var expires int
	if object(raw, map[string]any{"access_token": &token, "token_type": &kind, "expires_in": &expires, "scope": &scope}, []string{"access_token", "token_type", "expires_in"}) != nil || !strings.EqualFold(kind, "bearer") || expires < 1 || expires > 86400 || len(token) > 16384 || !bearerPattern.MatchString(token) || (scope != "" && scope != "platform:read") {
		return "", 0, errDenied
	}
	if expires > 300 {
		expires = 300
	}
	return token, expires, nil
}
func meResult(raw []byte) (v me, e error) {
	e = object(raw, map[string]any{"user_id": &v.User, "tenant_id": &v.Tenant, "membership_id": &v.Member, "session_id": &v.Session}, []string{"user_id", "tenant_id", "membership_id", "session_id"})
	if e != nil || !idPattern.MatchString(v.User) || !idPattern.MatchString(v.Tenant) || !idPattern.MatchString(v.Member) || !idPattern.MatchString(v.Session) {
		return me{}, errDenied
	}
	return v, nil
}
func (d *demo) session(r *http.Request, w http.ResponseWriter, create bool) (string, *browserSession) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now().Unix()
	for key, s := range d.sessions {
		if s.Until <= now {
			delete(d.sessions, key)
		}
	}
	if c, e := r.Cookie(cookieName); e == nil {
		if s := d.sessions[c.Value]; s != nil {
			return c.Value, s
		}
	}
	if !create || len(d.sessions) >= 128 {
		return "", nil
	}
	key, csrf := random(), random()
	if key == "" || csrf == "" {
		return "", nil
	}
	s := &browserSession{CSRF: csrf, Until: now + 1800}
	d.sessions[key] = s
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: key, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 1800})
	return key, s
}
func (d *demo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; form-action 'self' http://auth.localhost:4464 http://auth.localhost:8080; base-uri 'none'; frame-ancestors 'none'")
	if r.Host != "127.0.0.1:9876" || r.Header.Get("Authorization") != "" {
		http.Error(w, "拒绝此请求。", 403)
		return
	}
	if r.URL.Path == "/style.css" && r.Method == "GET" {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		io.WriteString(w, stylesheet)
		return
	}
	if r.URL.Path != "/callback" && r.URL.RawQuery != "" {
		http.Error(w, "请求无效。", 400)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/" {
		d.home(w, r)
		return
	}
	key, s := d.session(r, w, false)
	if s == nil {
		http.Error(w, "请重新打开演示首页。", 403)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/callback" {
		d.callback(w, r, key, s)
		return
	}
	if r.Method != "POST" || (r.URL.Path != "/login" && r.URL.Path != "/logout" && r.URL.Path != "/check") {
		http.Error(w, "请求无效。", 405)
		return
	}
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/x-www-form-urlencoded" || r.Header.Get("Origin") != origin {
		http.Error(w, "拒绝此请求。", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil || len(r.PostForm) != 1 || len(r.PostForm["csrf"]) != 1 {
		http.Error(w, "请求无效。", 400)
		return
	}
	d.mu.Lock()
	if d.sessions[key] != s || !same(s.CSRF, r.PostForm.Get("csrf")) {
		d.mu.Unlock()
		http.Error(w, "拒绝此请求。", 403)
		return
	}
	if r.URL.Path == "/check" {
		d.mu.Unlock()
		http.Redirect(w, r, "/", 303)
		return
	}
	s.Generation++
	s.Access = ""
	s.Expires = 0
	s.State = ""
	s.Verifier = ""
	s.Pending = 0
	if r.URL.Path == "/logout" {
		delete(d.sessions, key)
		d.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		http.Redirect(w, r, "/", 303)
		return
	}
	state, verifier := random(), random()
	if state == "" || verifier == "" {
		d.mu.Unlock()
		http.Error(w, "暂时无法登录。", 503)
		return
	}
	s.State = state
	s.Verifier = verifier
	s.Pending = time.Now().Unix() + 300
	d.mu.Unlock()
	hash := sha256.Sum256([]byte(verifier))
	params := url.Values{"client_id": {d.client}, "response_type": {"code"}, "redirect_uri": {origin + "/callback"}, "scope": {"platform:read"}, "audience": {"https://api.sso.local"}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "code_challenge_method": {"S256"}}
	http.Redirect(w, r, "http://auth.localhost:4464/oauth2/auth?"+params.Encode(), 303)
}
func (d *demo) callback(w http.ResponseWriter, r *http.Request, key string, s *browserSession) {
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || len(q["state"]) != 1 || len(q["code"]) != 1 || len(q.Get("code")) > 4096 || q.Get("code") == "" {
		http.Error(w, "授权结果无效，请重新登录。", 400)
		return
	}
	for name, values := range q {
		if len(values) != 1 || (name != "state" && name != "code" && name != "scope" && name != "iss") || (name == "scope" && values[0] != "platform:read") || (name == "iss" && values[0] != "http://auth.localhost:4464/") {
			http.Error(w, "授权结果无效，请重新登录。", 400)
			return
		}
	}
	d.mu.Lock()
	if d.sessions[key] != s || s.Pending <= time.Now().Unix() || !same(s.State, q.Get("state")) {
		d.mu.Unlock()
		http.Error(w, "授权已过期，请重新登录。", 400)
		return
	}
	verifier := s.Verifier
	s.State = ""
	s.Verifier = ""
	s.Pending = 0
	s.Generation++
	generation := s.Generation
	d.mu.Unlock()
	token, seconds, e := d.exchange(r.Context(), q.Get("code"), verifier)
	if e != nil || r.Context().Err() != nil {
		http.Error(w, "未能取得当前授权，请重新登录。", 403)
		return
	}
	newKey, newCSRF := random(), random()
	if newKey == "" || newCSRF == "" {
		http.Error(w, "暂时无法登录。", 503)
		return
	}
	d.mu.Lock()
	if d.sessions[key] != s || s.Generation != generation || s.Until <= time.Now().Unix() {
		d.mu.Unlock()
		http.Error(w, "授权状态已变化，请重新登录。", 409)
		return
	}
	delete(d.sessions, key)
	s.CSRF = newCSRF
	s.Generation++
	s.Access = token
	s.Expires = time.Now().Unix() + int64(seconds)
	d.sessions[newKey] = s
	d.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: newKey, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 1800})
	http.Redirect(w, r, "/", 303)
}

type view struct {
	CSRF, Message string
	Me            me
	Allowed       bool
}

var homeTemplate = template.Must(template.New("home").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>应用接入演示</title><link rel="stylesheet" href="/style.css"></head><body><main><p class="eyebrow">SSO / 本地独立应用</p><h1>应用接入演示</h1><p>通过统一登录进入应用；每次刷新都会核对当前访问资格。</p>{{if .Allowed}}<p role="status" class="allowed">当前准入已确认</p><dl><dt>User</dt><dd><code>{{.Me.User}}</code></dd><dt>空间</dt><dd><code>{{.Me.Tenant}}</code></dd><dt>成员</dt><dd><code>{{.Me.Member}}</code></dd></dl><form method="post" action="/check"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>重新检查访问</button></form><form method="post" action="/logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="secondary">退出当前演示应用</button></form>{{else}}<p role="status">{{if .Message}}{{.Message}}{{else}}尚未登录此应用{{end}}</p><form method="post" action="/login"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>使用 SSO 登录</button></form>{{end}}<p class="note">所有者可在控制台的“应用接入 → 成员访问”中授权或撤销。此应用只展示已授权的身份与空间资料。</p></main></body></html>`))

const stylesheet = `*{box-sizing:border-box}body{margin:0;background:#f2f5fa;color:#172d4d;font:16px system-ui,sans-serif}main{max-width:700px;margin:7vh auto;padding:32px;background:white;border-radius:18px;border:1px solid #dbe4ef}h1{font-size:30px}.eyebrow,.note{color:#60738f}.note{font-size:14px;margin-top:30px}.allowed{color:#176345;background:#eaf7ef;padding:16px;border-radius:10px}code{overflow-wrap:anywhere}dd{margin:6px 0 20px}dt{font-weight:600}form{display:inline-block;margin:8px 12px 0 0}button{border:0;border-radius:9px;padding:13px 18px;background:#2455d9;color:white;font:inherit;cursor:pointer}.secondary{background:#eaf0fa;color:#203958}@media(max-width:740px){main{margin:18px 12px;padding:24px}h1{font-size:26px}}`

func (d *demo) home(w http.ResponseWriter, r *http.Request) {
	key, s := d.session(r, w, true)
	if s == nil {
		http.Error(w, "演示会话已满，请稍后再试。", 429)
		return
	}
	d.mu.Lock()
	v := view{CSRF: s.CSRF}
	token, generation := s.Access, s.Generation
	if s.Expires <= time.Now().Unix() {
		token = ""
		s.Access = ""
	}
	d.mu.Unlock()
	if token != "" {
		person, e := d.verify(r.Context(), token)
		d.mu.Lock()
		if d.sessions[key] == s && s.Generation == generation && s.Access == token && s.Expires > time.Now().Unix() {
			if e == nil {
				v.Me = person
				v.Allowed = true
			} else {
				s.Access = ""
				s.Expires = 0
				v.Message = "当前访问已失效或暂不可用，请重新登录。"
			}
		}
		d.mu.Unlock()
	}
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	homeTemplate.Execute(w, v)
}
func main() {
	if len(os.Args) != 3 || os.Args[1] != "--client" {
		fmt.Fprintln(os.Stderr, "Usage: user-application --client PUBLIC_CLIENT_ID")
		os.Exit(2)
	}
	d, e := newDemo(os.Args[2])
	if e != nil {
		fmt.Fprintln(os.Stderr, "User application demo configuration invalid")
		os.Exit(2)
	}
	server := &http.Server{Addr: "127.0.0.1:9876", Handler: d, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 8 * time.Second, IdleTimeout: 30 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	listener, e := net.Listen("tcp", server.Addr)
	if e != nil {
		fmt.Fprintln(os.Stderr, "User application demo unavailable")
		os.Exit(1)
	}
	fmt.Println("User application demo ready at http://127.0.0.1:9876")
	if server.Serve(listener) != nil {
		fmt.Fprintln(os.Stderr, "User application demo unavailable")
		os.Exit(1)
	}
}
