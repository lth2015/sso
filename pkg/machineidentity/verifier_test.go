package machineidentity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const tenant = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const app = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const principal = "cccccccccccccccccccccccccccccccc"
const credential = "dddddddddddddddddddddddddddddddd"
const rawBearer = "fixture.header.signature"

func validBody() string {
	b, _ := json.Marshal(Identity{"service_principal", tenant, app, principal, credential, "machine:invoke", "urn:sso:application:" + app})
	return string(b)
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "Cache-Control": {"no-store"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func stub(t *testing.T, f roundTrip) *Verifier {
	t.Helper()
	v, e := New(Config{Origin: "https://sso.example.invalid", Transport: f})
	if e != nil {
		t.Fatal("trusted fixture configuration failed")
	}
	return v
}
func TestConfigurationRejectsRequestDestinationsAndInsecureOrigins(t *testing.T) {
	for _, origin := range []string{"", "http://sso.example.invalid", "http://127.0.0.1:8083", "http://api.localhost:80", "https://user:pass@sso.example.invalid", "https://sso.example.invalid/path", "https://sso.example.invalid/?x=y", "https://sso.example.invalid?", "https://sso.example.invalid/#fragment", "//sso.example.invalid", "file:///tmp/data", "https://:443", "https://sso.example.invalid:65536", "https://sso.example.invalid:0"} {
		if _, e := New(Config{Origin: origin, AllowLocalHTTP: true}); !errors.Is(e, ErrConfiguration) {
			t.Fatal("untrusted configuration accepted")
		}
	}
	if _, e := New(Config{Origin: localOrigin}); !errors.Is(e, ErrConfiguration) {
		t.Fatal("local HTTP silently enabled")
	}
	if _, e := New(Config{Origin: localOrigin, AllowLocalHTTP: true}); e != nil {
		t.Fatal("explicit fixed local origin refused")
	}
	for _, timeout := range []time.Duration{-1, 6 * time.Second} {
		if _, e := New(Config{Origin: "https://sso.example.invalid", Timeout: timeout}); !errors.Is(e, ErrConfiguration) {
			t.Fatal("unbounded timeout accepted")
		}
	}
}
func TestTLSOnlineIdentityAndNoAuthorizationCacheOrCookieJar(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/machine/applications/"+app+"/identity" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer "+rawBearer || r.Header.Get("Cookie") != "" || r.Header.Get("Cache-Control") != "no-store" {
			t.Error("identity request escaped fixed resource boundary")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Set-Cookie", "unexpected=private; Secure")
		if requests.Add(1) == 1 {
			io.WriteString(w, validBody())
			return
		}
		w.WriteHeader(401)
		io.WriteString(w, `{"private":"never expose upstream body"}`)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	v, e := New(Config{Origin: server.URL, Transport: server.Client().Transport})
	if e != nil {
		t.Fatal("TLS origin refused")
	}
	identity, e := v.Verify(context.Background(), tenant, app, rawBearer)
	if e != nil || identity.ServicePrincipalID != principal {
		t.Fatal("valid online identity refused")
	}
	empty, e := v.Verify(context.Background(), tenant, app, rawBearer)
	if !errors.Is(e, ErrUnauthenticated) || empty != (Identity{}) {
		t.Fatal("revoked authority was cached or returned partial identity")
	}
	if requests.Load() != 2 {
		t.Fatal("identity verification retried or skipped primary request")
	}
	// Default TLS trust refuses an untrusted certificate; no insecure fallback.
	strict, e := New(Config{Origin: server.URL})
	if e != nil {
		t.Fatal("TLS origin refused")
	}
	if _, e = strict.Verify(context.Background(), tenant, app, rawBearer); !errors.Is(e, ErrUnavailable) {
		t.Fatal("untrusted TLS authority accepted")
	}
}
func TestNoRedirectsOrUpstreamErrorLeak(t *testing.T) {
	var calls atomic.Int32
	for _, status := range []int{301, 302, 307, 308, 401, 403, 429, 500, 503} {
		v := stub(t, func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			res := response("private-upstream-"+rawBearer, status)
			res.Header.Set("Location", "https://unexpected.example.invalid/collect")
			return res, nil
		})
		identity, e := v.Verify(context.Background(), tenant, app, rawBearer)
		if e == nil || identity != (Identity{}) || strings.Contains(e.Error(), rawBearer) || strings.Contains(e.Error(), "upstream") {
			t.Fatal("redirect, non-200 or private upstream body accepted")
		}
	}
	if calls.Load() != 9 {
		t.Fatal("unexpected redirect or retry")
	}
	v := stub(t, func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("private-transport-" + rawBearer)
	})
	if _, e := v.Verify(context.Background(), tenant, app, rawBearer); e != ErrUnavailable {
		t.Fatal("raw transport error escaped")
	}
}
func TestStrictIdentityResponseHasNoAmbiguousOrForeignClaims(t *testing.T) {
	good := validBody()
	for name, body := range map[string]string{
		"foreign-tenant":       strings.ReplaceAll(good, tenant, strings.Repeat("e", 32)),
		"foreign-application":  strings.ReplaceAll(good, app, strings.Repeat("e", 32)),
		"user-realm":           strings.ReplaceAll(good, "service_principal", "user"),
		"platform-scope":       strings.ReplaceAll(good, "machine:invoke", "platform:write"),
		"bad-principal":        strings.ReplaceAll(good, principal, "not-an-id"),
		"bad-credential":       strings.ReplaceAll(good, credential, strings.ToUpper(credential)),
		"unknown-secret-field": strings.TrimSuffix(good, "}") + `,"secret_key":"private"}`,
		"duplicate":            strings.TrimSuffix(good, "}") + `,"tenant_id":"` + tenant + `"}`,
		"case-variant":         strings.Replace(good, "tenant_id", "Tenant_ID", 1),
		"multiple-documents":   good + good,
		"missing":              `{}`,
		"null":                 `null`,
		"array":                "[" + good + "]",
		"wrong-field-type":     strings.Replace(good, `"scope":"machine:invoke"`, `"scope":17`, 1),
		"oversize":             good + strings.Repeat(" ", 16385),
	} {
		t.Run(name, func(t *testing.T) {
			v := stub(t, func(*http.Request) (*http.Response, error) { return response(body, 200), nil })
			identity, e := v.Verify(context.Background(), tenant, app, rawBearer)
			if e != ErrInvalidResponse || identity != (Identity{}) {
				t.Fatal("malformed or foreign authority response accepted")
			}
		})
	}
}
func TestCacheAndEncodingBoundary(t *testing.T) {
	for _, test := range []struct{ key, value string }{{"Cache-Control", ""}, {"Cache-Control", "public, max-age=300"}, {"Age", "1"}, {"Content-Type", "text/html"}, {"Content-Encoding", "gzip"}} {
		v := stub(t, func(*http.Request) (*http.Response, error) {
			res := response(validBody(), 200)
			res.Header.Set(test.key, test.value)
			return res, nil
		})
		if _, e := v.Verify(context.Background(), tenant, app, rawBearer); e != ErrInvalidResponse {
			t.Fatal("cached, encoded or non-contract response accepted")
		}
	}
}
func TestInvalidInputsDoNotReachTheAuthority(t *testing.T) {
	v := stub(t, func(*http.Request) (*http.Response, error) {
		t.Error("invalid request reached authority")
		return nil, ErrUnavailable
	})
	for _, tok := range []string{"", "Bearer " + rawBearer, "injected\r\nCookie: secret", strings.Repeat("x", 16385), "nonascii-令牌"} {
		if _, e := v.Verify(context.Background(), tenant, app, tok); e != ErrInvalidRequest {
			t.Fatal("unsafe bearer accepted")
		}
	}
	for _, target := range []string{"invalid", strings.ToUpper(app), "../" + app} {
		if _, e := v.Verify(context.Background(), tenant, target, rawBearer); e != ErrInvalidRequest {
			t.Fatal("untrusted application path accepted")
		}
	}
	if _, e := v.Verify(nil, tenant, app, rawBearer); e != ErrInvalidRequest {
		t.Fatal("nil context accepted")
	}
	var absent *Verifier
	if _, e := absent.Verify(context.Background(), tenant, app, rawBearer); e != ErrInvalidRequest {
		t.Fatal("nil verifier panic or acceptance")
	}
}
func TestCanceledContextFailsClosedWithoutPrivateTransportError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v := stub(t, func(r *http.Request) (*http.Response, error) { return nil, ctx.Err() })
	if identity, e := v.Verify(ctx, tenant, app, rawBearer); !errors.Is(e, context.Canceled) || identity != (Identity{}) {
		t.Fatal("canceled verification returned authority")
	}
}
