package useraudit

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

const testTenant = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const privateToken = "private-user-test-bearer-never-echoed"
const testTime = "2026-11-11T22:00:00.123456789Z"

func managementBody() string {
	return `{"format":"sso.audit.batch.v1","kind":"management","tenant_id":"` + testTenant + `","generated_at":"` + testTime + `","authz_version":1,"items":[{"id":"` + testID + `","actor_membership_id":"cccccccccccccccccccccccccccccccc","action":"invitation.reissued","target_id":"dddddddddddddddddddddddddddddddd","occurred_at":"` + testTime + `","role_id":null,"role_code":null,"membership_id":null}],"next_cursor":null}`
}
func machineBody() string {
	return `{"format":"sso.audit.batch.v1","kind":"machine","tenant_id":"` + testTenant + `","generated_at":"` + testTime + `","authz_version":2,"items":[{"id":"` + testID + `","application_id":"cccccccccccccccccccccccccccccccc","service_principal_id":"dddddddddddddddddddddddddddddddd","credential_id":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","action":"token.client_credentials","occurred_at":"` + testTime + `"}],"next_cursor":null}`
}
func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewTLSServer(h)
	t.Cleanup(server.Close)
	c, e := New(Config{Origin: server.URL, Transport: server.Client().Transport})
	if e != nil {
		t.Fatal("SDK TLS fixture configuration failed")
	}
	return c
}
func respond(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, body)
}
func TestTrustedOriginsRedirectAndDefaultTLSBoundaries(t *testing.T) {
	for _, origin := range []string{"", "http://outside.example.invalid", "https://user:private@example.invalid", "https://example.invalid/path", "https://example.invalid?token=private", "https://example.invalid#private", "https://:443", "https://example.invalid:0", "https://example.invalid:65536"} {
		if _, e := New(Config{Origin: origin}); !errors.Is(e, ErrConfiguration) {
			t.Fatal("untrusted origin accepted")
		}
	}
	if _, e := New(Config{Origin: localOrigin}); !errors.Is(e, ErrConfiguration) {
		t.Fatal("implicit local HTTP accepted")
	}
	if _, e := New(Config{Origin: localOrigin, AllowLocalHTTP: true}); e != nil {
		t.Fatal("explicit fixed local origin refused")
	}
	for _, timeout := range []time.Duration{-time.Second, 6 * time.Second} {
		if _, e := New(Config{Origin: "https://example.invalid", Timeout: timeout}); !errors.Is(e, ErrConfiguration) {
			t.Fatal("unbounded timeout accepted")
		}
	}
	var redirected atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) })
	if v, e := c.Management(context.Background(), privateToken, testTenant, Page{}); !errors.Is(e, ErrInvalidResponse) || v.Items != nil || redirected.Load() != 0 {
		t.Fatal("redirect followed or partial data returned")
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { respond(w, managementBody()) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	c, e := New(Config{Origin: server.URL})
	if e != nil {
		t.Fatal("default TLS client unavailable")
	}
	if _, e = c.Management(context.Background(), privateToken, testTenant, Page{}); !errors.Is(e, ErrUnavailable) || strings.Contains(e.Error(), privateToken) {
		t.Fatal("untrusted TLS accepted or raw transport error leaked")
	}
}
func TestEveryCallUsesCurrentAuthorityWithoutCookiesCacheOrRetries(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/tenants/"+testTenant+"/audit-exports" || r.Header.Get("Authorization") != "Bearer "+privateToken || r.Header.Get("Cookie") != "" || r.URL.Query().Get("limit") != "1" {
			t.Error("SDK request escaped fixed authority/path/header bounds")
		}
		w.Header().Set("Set-Cookie", "never-retain=private")
		if calls.Add(1) == 1 {
			respond(w, managementBody())
		} else {
			w.WriteHeader(403)
			w.Write([]byte(privateToken))
		}
	})
	first, e := c.Management(context.Background(), privateToken, testTenant, Page{Limit: 1})
	if e != nil || len(first.Items) != 1 {
		t.Fatal("valid batch refused")
	}
	denied, e := c.Management(context.Background(), privateToken, testTenant, Page{Limit: 1})
	if !errors.Is(e, ErrForbidden) || denied.Items != nil || calls.Load() != 2 || strings.Contains(e.Error(), privateToken) {
		t.Fatal("authority cached, Cookie retained, request retried or error body exposed")
	}
}
func TestFrozenBothKindProjectionsAndExactJSON(t *testing.T) {
	for _, kind := range []string{"management", "machine"} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			if kind == "management" {
				respond(w, managementBody())
			} else {
				respond(w, machineBody())
			}
		})
		if kind == "management" {
			v, e := c.Management(context.Background(), privateToken, testTenant, Page{})
			if e != nil || len(v.Items) != 1 || v.Items[0].RoleID != nil {
				t.Fatal("management projection failed")
			}
		} else {
			v, e := c.Machine(context.Background(), privateToken, testTenant, Page{})
			if e != nil || len(v.Items) != 1 || v.Items[0].Action != "token.client_credentials" {
				t.Fatal("machine projection failed")
			}
		}
	}
	mutations := []func(string) string{
		func(v string) string { return strings.Replace(v, `"kind":"management"`, `"kind":"machine"`, 1) },
		func(v string) string { return strings.Replace(v, testTenant, strings.Repeat("f", 32), 1) },
		func(v string) string {
			return strings.Replace(v, `"authz_version":1`, `"authz_version":1,"secret_key":"`+privateToken+`"`, 1)
		},
		func(v string) string {
			return strings.Replace(v, `"authz_version":1`, `"authz_version":1,"authz_version":1`, 1)
		},
		func(v string) string { return strings.Replace(v, `"authz_version":1`, `"Authz_version":1`, 1) },
		func(v string) string { return strings.Replace(v, `"items":[`, `"items":null,"unused":[`, 1) },
		func(v string) string { return strings.Replace(v, `"role_code":null`, `"role_code":"incoherent"`, 1) },
		func(v string) string {
			return strings.Replace(v, `"next_cursor":null`, `"next_cursor":"`+testID+`"`, 1)
		},
		func(v string) string { return v + ` {"secret":"` + privateToken + `"}` },
		func(v string) string { return v + strings.Repeat(" ", 256<<10) },
		func(v string) string { return strings.Replace(v, testTime, "invalid-time", 1) },
	}
	for _, mutate := range mutations {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { respond(w, mutate(managementBody())) })
		v, e := c.Management(context.Background(), privateToken, testTenant, Page{})
		if !errors.Is(e, ErrInvalidResponse) || v.Items != nil || strings.Contains(e.Error(), privateToken) {
			t.Fatal("untrusted batch escaped frozen/whole JSON validation")
		}
	}
}
func TestBoundedCursorOrderingAndIndependentBatchShape(t *testing.T) {
	page := Page{Limit: 1}
	body := strings.Replace(managementBody(), `"next_cursor":null`, `"next_cursor":"`+testID+`"`, 1)
	v, e := decodeBatch([]byte(body), testTenant, "management", page, decodeManagement, func(v ManagementEvent) string { return v.ID })
	if e != nil || v.NextCursor == nil {
		t.Fatal("valid bounded continuation refused")
	}
	for _, after := range []string{testID, strings.Repeat("f", 32)} {
		_, e = decodeBatch([]byte(managementBody()), testTenant, "management", Page{Limit: 1, After: after}, decodeManagement, func(v ManagementEvent) string { return v.ID })
		if !errors.Is(e, ErrInvalidResponse) {
			t.Fatal("nonadvancing keyset accepted")
		}
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(managementBody()), &raw) != nil {
		t.Fatal("batch fixture failed")
	}
	var rows []json.RawMessage
	json.Unmarshal(raw["items"], &rows)
	rows = append(rows, rows[0])
	raw["items"], _ = json.Marshal(rows)
	duplicate, _ := json.Marshal(raw)
	if _, e = decodeBatch(duplicate, testTenant, "management", Page{Limit: 2}, decodeManagement, func(v ManagementEvent) string { return v.ID }); !errors.Is(e, ErrInvalidResponse) {
		t.Fatal("duplicate event IDs accepted")
	}
}
func TestCacheMediaErrorsInputsAndCancellationFailClosed(t *testing.T) {
	for _, header := range []struct{ name, value string }{{"Content-Type", "text/html"}, {"Cache-Control", "public"}, {"Age", "1"}, {"Content-Encoding", "gzip"}} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set(header.name, header.value)
			io.WriteString(w, managementBody())
		})
		if v, e := c.Management(context.Background(), privateToken, testTenant, Page{}); !errors.Is(e, ErrInvalidResponse) || v.Items != nil {
			t.Fatal("cached or incompatible response accepted")
		}
	}
	for _, status := range []struct {
		n   int
		err error
	}{{401, ErrUnauthenticated}, {403, ErrForbidden}, {404, ErrNotFound}, {429, ErrUnavailable}, {503, ErrUnavailable}, {302, ErrInvalidResponse}} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status.n); w.Write([]byte(privateToken)) })
		if _, e := c.Management(context.Background(), privateToken, testTenant, Page{}); !errors.Is(e, status.err) || strings.Contains(e.Error(), privateToken) {
			t.Fatal("status handling leaked an upstream body or accepted failure")
		}
	}
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); respond(w, managementBody()) })
	for _, page := range []Page{{Limit: -1}, {Limit: 101}, {After: "../private"}} {
		if _, e := c.Management(context.Background(), privateToken, testTenant, page); !errors.Is(e, ErrInvalidRequest) {
			t.Fatal("invalid pagination accepted")
		}
	}
	if _, e := c.Management(context.Background(), "bad\r\nCookie: private", testTenant, Page{}); !errors.Is(e, ErrInvalidRequest) || calls.Load() != 0 {
		t.Fatal("invalid input reached transport")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.Management(ctx, privateToken, testTenant, Page{}); !errors.Is(e, context.Canceled) {
		t.Fatal("cancelled request lost its context category")
	}
}

func TestCancellationDuringResponseBodyReturnsNoPartialBatch(t *testing.T) {
	entered := make(chan struct{})
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte(`{"format":`))
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() {
		v, e := c.Management(ctx, privateToken, testTenant, Page{})
		done <- errors.Is(e, context.Canceled) && v.Items == nil && v.TenantID == ""
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("SDK did not begin the bounded response")
	}
	cancel()
	select {
	case valid := <-done:
		if !valid {
			t.Fatal("inflight cancellation returned partial data or a raw error")
		}
	case <-time.After(time.Second):
		t.Fatal("SDK response cancellation did not finish")
	}
}
