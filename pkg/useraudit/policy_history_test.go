package useraudit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

const testRole = "cccccccccccccccccccccccccccccccc"

func policyBody() string {
	return `{"format":"sso.role.policy-history.v1","tenant_id":"` + testTenant + `","role_id":"` + testRole + `","items":[{"id":"` + testID + `","actor_membership_id":"dddddddddddddddddddddddddddddddd","role_code":"sdk_auditor","action":"role.created","occurred_at":"` + testTime + `","authz_version":2,"before":[],"after":[{"permission":"audit.read","effect":"allow"}]}],"next_cursor":null}`
}
func TestPolicyHistoryEveryReadBindsOwnerResourceWithoutCaching(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/tenants/"+testTenant+"/roles/"+testRole+"/policy-history" || r.URL.Query().Get("limit") != "50" || r.URL.Query().Has("kind") || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "Bearer "+privateToken || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("owner history request escaped fixed resource boundary")
		}
		if calls.Add(1) == 1 {
			respond(w, policyBody())
		} else {
			w.WriteHeader(403)
			io.WriteString(w, privateToken)
		}
	})
	v, e := c.PolicyHistory(context.Background(), privateToken, testTenant, testRole, Page{})
	if e != nil || len(v.Items) != 1 || v.Format != PolicyHistoryFormat || v.Items[0].After[0].Permission != "audit.read" {
		t.Fatal("valid owner history refused")
	}
	denied, e := c.PolicyHistory(context.Background(), privateToken, testTenant, testRole, Page{})
	if !errors.Is(e, ErrForbidden) || denied.Items != nil || calls.Load() != 2 || strings.Contains(e.Error(), privateToken) {
		t.Fatal("owner status cached or private error body escaped")
	}
}
func TestPolicyHistoryStrictProjectionAndTransitions(t *testing.T) {
	mutations := []func(string) string{
		func(v string) string { return strings.Replace(v, testTenant, strings.Repeat("f", 32), 1) },
		func(v string) string { return strings.Replace(v, testRole, strings.Repeat("f", 32), 1) },
		func(v string) string { return strings.Replace(v, `"role_id":`, `"Role_id":`, 1) },
		func(v string) string {
			return strings.Replace(v, `"role_id":"`+testRole+`"`, `"role_id":"`+testRole+`","role_id":"`+testRole+`"`, 1)
		},
		func(v string) string {
			return strings.Replace(v, `"items":[`, `"secret_key":"`+privateToken+`","items":[`, 1)
		},
		func(v string) string {
			return strings.Replace(v, `"role_code":"sdk_auditor"`, `"role_code":"owner"`, 1)
		},
		func(v string) string {
			return strings.Replace(v, `"action":"role.created"`, `"action":"role.bound"`, 1)
		},
		func(v string) string { return strings.Replace(v, `"before":[]`, `"before":null`, 1) },
		func(v string) string {
			return strings.Replace(v, `"before":[]`, `"before":[{"permission":"audit.read","effect":"allow"}]`, 1)
		},
		func(v string) string {
			return strings.Replace(v, `"action":"role.created"`, `"action":"role.deleted"`, 1)
		},
		func(v string) string {
			return strings.Replace(strings.Replace(v, `"action":"role.created"`, `"action":"role.updated"`, 1), `"after":[{"permission":"audit.read","effect":"allow"}]`, `"after":[]`, 1)
		},
		func(v string) string {
			return strings.Replace(v, `"permission":"audit.read"`, `"permission":"roles.manage"`, 1)
		},
		func(v string) string {
			return strings.Replace(v, `"permission":"audit.read"`, `"Permission":"audit.read"`, 1)
		},
		func(v string) string {
			return strings.Replace(v, `"effect":"allow"`, `"effect":"allow","secret_key":"`+privateToken+`"`, 1)
		},
		func(v string) string {
			return strings.Replace(v, `"effect":"allow"`, `"effect":"allow","effect":"deny"`, 1)
		},
		func(v string) string {
			return strings.Replace(v, `"after":[{"permission":"audit.read","effect":"allow"}]`, `"after":[{"permission":"audit.read","effect":"allow"},{"permission":"audit.read","effect":"allow"}]`, 1)
		},
		func(v string) string {
			return strings.Replace(v, `"authz_version":2`, `"authz_version":9223372036854775808`, 1)
		},
		func(v string) string { return strings.Replace(v, testTime, "invalid-time", 1) },
		func(v string) string { return v + ` {"secret_key":"` + privateToken + `"}` },
	}
	for _, mutate := range mutations {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { respond(w, mutate(policyBody())) })
		v, e := c.PolicyHistory(context.Background(), privateToken, testTenant, testRole, Page{})
		if !errors.Is(e, ErrInvalidResponse) || v.Items != nil || v.Format != "" || strings.Contains(e.Error(), privateToken) {
			t.Fatal("invalid/private policy history escaped exact validation")
		}
	}
}
func TestPolicyHistoryPaginationAndCanonicalRuleArrays(t *testing.T) {
	first := strings.Replace(policyBody(), `"next_cursor":null`, `"next_cursor":"`+testID+`"`, 1)
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") == "" {
			respond(w, first)
		} else {
			if r.URL.Query().Get("after") != testID {
				t.Error("history cursor changed")
			}
			respond(w, strings.Replace(policyBody(), testID, strings.Repeat("e", 32), 1))
		}
	})
	v, e := c.PolicyHistory(context.Background(), privateToken, testTenant, testRole, Page{Limit: 1})
	if e != nil || v.NextCursor == nil {
		t.Fatal("history continuation missing")
	}
	next, e := c.PolicyHistory(context.Background(), privateToken, testTenant, testRole, Page{Limit: 1, After: *v.NextCursor})
	if e != nil || len(next.Items) != 1 || next.Items[0].ID <= v.Items[0].ID || next.NextCursor != nil {
		t.Fatal("history cursor did not advance")
	}
	for _, body := range []string{first, strings.Replace(policyBody(), `"after":[{"permission":"audit.read","effect":"allow"}]`, `"after":[{"permission":"members.read","effect":"allow"},{"permission":"audit.read","effect":"allow"}]`, 1)} {
		if _, e := decodePolicyHistory([]byte(body), testTenant, testRole, Page{Limit: 50, After: testID}); !errors.Is(e, ErrInvalidResponse) {
			t.Fatal("nonadvancing/canonical-invalid history accepted")
		}
	}
	empty := strings.Replace(policyBody(), `"items":[{"id":"`+testID+`","actor_membership_id":"dddddddddddddddddddddddddddddddd","role_code":"sdk_auditor","action":"role.created","occurred_at":"`+testTime+`","authz_version":2,"before":[],"after":[{"permission":"audit.read","effect":"allow"}]}]`, `"items":[]`, 1)
	decoded, e := decodePolicyHistory([]byte(empty), testTenant, testRole, Page{Limit: 50})
	if e != nil || decoded.Items == nil || len(decoded.Items) != 0 {
		t.Fatal("legacy empty captured history refused")
	}
}

type policyTransport func(*http.Request) (*http.Response, error)

func (t policyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }

type cancelAtEOF struct {
	io.Reader
	cancel context.CancelFunc
}

func (r cancelAtEOF) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	if e == io.EOF {
		r.cancel()
	}
	return n, e
}
func TestSharedUserReadCancellationAfterBodyNeverReturnsData(t *testing.T) {
	for _, kind := range []string{"management", "machine", "policy_history"} {
		ctx, cancel := context.WithCancel(context.Background())
		body := policyBody()
		if kind == "management" {
			body = managementBody()
		} else if kind == "machine" {
			body = machineBody()
		}
		c, e := New(Config{Origin: "https://trusted.example.invalid", Transport: policyTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "Cache-Control": {"no-store"}}, Body: io.NopCloser(cancelAtEOF{strings.NewReader(body), cancel})}, nil
		})})
		if e != nil {
			t.Fatal("cancellation fixture unavailable")
		}
		switch kind {
		case "management":
			v, e := c.Management(ctx, privateToken, testTenant, Page{})
			if !errors.Is(e, context.Canceled) || v.Items != nil {
				t.Fatal("canceled management data returned")
			}
		case "machine":
			v, e := c.Machine(ctx, privateToken, testTenant, Page{})
			if !errors.Is(e, context.Canceled) || v.Items != nil {
				t.Fatal("canceled machine data returned")
			}
		default:
			v, e := c.PolicyHistory(ctx, privateToken, testTenant, testRole, Page{})
			if !errors.Is(e, context.Canceled) || v.Items != nil {
				t.Fatal("canceled policy history returned")
			}
		}
		cancel()
	}
}
func TestPolicyHistoryInputsAndSharedHTTPResponseGuards(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); respond(w, policyBody()) })
	for _, p := range []Page{{Limit: -1}, {Limit: 101}, {After: "../private"}} {
		if _, e := c.PolicyHistory(context.Background(), privateToken, testTenant, testRole, p); !errors.Is(e, ErrInvalidRequest) {
			t.Fatal("invalid history page sent")
		}
	}
	if _, e := c.PolicyHistory(context.Background(), privateToken, testTenant, "../private", Page{}); !errors.Is(e, ErrInvalidRequest) || calls.Load() != 0 {
		t.Fatal("role path injection sent")
	}
	for _, header := range []struct{ name, value string }{{"Cache-Control", "public"}, {"Age", "1"}, {"Content-Type", "text/html"}, {"Content-Encoding", "gzip"}} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set(header.name, header.value)
			io.WriteString(w, policyBody())
		})
		v, e := c.PolicyHistory(context.Background(), privateToken, testTenant, testRole, Page{})
		if !errors.Is(e, ErrInvalidResponse) || v.Items != nil {
			t.Fatal("history bypassed shared cache/media guard")
		}
	}
}
