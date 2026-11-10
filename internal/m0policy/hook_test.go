package m0policy

import (
	"testing"
	"time"
)

func TestEvaluateBoundaries(t *testing.T) {
	now := time.Unix(1000, 0)
	p := Policy{Tenant: "tenant-a", Principal: "user-a", Kind: "user", Subject: "user-a", ContextID: "ctx-a", Audience: "api-a", Scopes: "openid offline_access read", Active: true, Version: 1, Expires: 2000}
	var r Request
	r.Session.IDToken.Subject = "user-a"
	r.Session.Extra = map[string]any{"m0_context_id": "ctx-a"}
	r.Request.GrantTypes = []string{"authorization_code"}
	r.Request.GrantedAudience = []string{"api-a"}
	r.Request.GrantedScopes = []string{"read"}
	cases := []struct {
		name  string
		alter func(*Policy, *Request)
		want  bool
	}{
		{"valid code", func(*Policy, *Request) {}, true},
		{"valid refresh", func(_ *Policy, r *Request) { r.Request.GrantTypes = []string{"refresh_token"} }, true},
		{"disabled", func(p *Policy, _ *Request) { p.Active = false }, false},
		{"expired", func(p *Policy, _ *Request) { p.Expires = 1000 }, false},
		{"wrong subject", func(_ *Policy, r *Request) { r.Session.IDToken.Subject = "user-b" }, false},
		{"wrong context", func(_ *Policy, r *Request) { r.Session.Extra = map[string]any{"m0_context_id": "ctx-b"} }, false},
		{"scope escalation", func(_ *Policy, r *Request) { r.Request.GrantedScopes = []string{"write"} }, false},
		{"wrong audience", func(_ *Policy, r *Request) { r.Request.GrantedAudience = []string{"api-b"} }, false},
		{"multiple audiences", func(_ *Policy, r *Request) { r.Request.GrantedAudience = []string{"api-a", "api-b"} }, false},
		{"password grant", func(_ *Policy, r *Request) { r.Request.GrantTypes = []string{"password"} }, false},
		{"service cannot use user grant", func(p *Policy, _ *Request) { p.Kind = "service" }, false},
		{"user cannot use m2m", func(_ *Policy, r *Request) { r.Request.GrantTypes = []string{"client_credentials"} }, false},
		{"valid m2m", func(p *Policy, r *Request) { p.Kind = "service"; r.Request.GrantTypes = []string{"client_credentials"} }, true},
		{"unresolved context denied", func(p *Policy, r *Request) {
			p.ContextID = ""
			r.Request.GrantedScopes = nil
			r.Request.GrantedAudience = nil
		}, false},
		{"resolved code context", func(_ *Policy, r *Request) { r.Request.GrantedScopes = nil; r.Request.GrantedAudience = nil }, true},
		{"session metadata cannot override authority", func(_ *Policy, r *Request) {
			r.Request.GrantedScopes = nil
			r.Request.GrantedAudience = nil
			r.Session.Extra = map[string]any{"m0_context_id": "ctx-a", "m0_grant_audience": "api-b", "m0_grant_scopes": []any{"write"}}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pp, rr := p, r
			tc.alter(&pp, &rr)
			if got := Evaluate(pp, rr, now); got != tc.want {
				t.Fatalf("allow=%v, want %v", got, tc.want)
			}
		})
	}
}
