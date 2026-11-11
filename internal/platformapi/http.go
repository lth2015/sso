package platformapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	driver "github.com/go-sql-driver/mysql"
	contract "github.com/lth2015/sso/api"
	"github.com/lth2015/sso/internal/identity"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

type contextKey struct{}

func principal(r *http.Request) Principal           { return r.Context().Value(contextKey{}).(Principal) }
func authority(r *http.Request) store.Authorization { return principal(r).Authority }
func Handler(auth *Authenticator) http.Handler {
	mux := http.NewServeMux()
	repo := auth.Store
	mountRBAC(mux, repo)
	mountSecurity(mux, repo)
	limit := identity.NewLimiter()
	capacity := make(chan struct{}, 32)
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(contract.PublicSpec)
	})
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(contract.PublicJSON)
	})
	mux.HandleFunc("GET /docs", docs)
	mux.HandleFunc("GET /docs.css", docsCSS)
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := repo.SSOReady(r.Context()); err != nil {
			problem(w, 503, "temporarily_unavailable")
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		a := authority(r)
		respond(w, 200, map[string]string{"user_id": a.User.String(), "tenant_id": a.Tenant.String(), "membership_id": a.Member.String(), "session_id": a.Session.String()})
	})
	mux.HandleFunc("GET /v1/tenants", func(w http.ResponseWriter, r *http.Request) {
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, err := repo.APITenants(r.Context(), authority(r), n+1, after)
		if err != nil {
			failure(w, err)
			return
		}
		page(w, items, n, func(t store.TenantSummary) string { return t.ID })
	})
	mux.HandleFunc("POST /v1/tenants", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		}
		if !decode(w, r, &v) {
			return
		}
		v.Name = strings.TrimSpace(v.Name)
		b, err := repo.APIBootstrap(r.Context(), authority(r), v.Slug, v.Name)
		if err != nil {
			failure(w, err)
			return
		}
		w.Header().Set("Location", "/v1/tenants/"+b.Tenant.String())
		respond(w, 201, map[string]string{"id": b.Tenant.String(), "membership_id": b.Membership.String(), "root_id": b.Root.String()})
	})
	mux.HandleFunc("GET /v1/tenants/{tenant}", func(w http.ResponseWriter, r *http.Request) {
		v, err := repo.APITenant(r.Context(), authority(r))
		if err != nil {
			failure(w, err)
			return
		}
		etag(w, "authz", v.AuthzVersion)
		respond(w, 200, v)
	})
	mux.HandleFunc("GET /v1/tenants/{tenant}/members", func(w http.ResponseWriter, r *http.Request) {
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, version, err := repo.ForAuthorization(authority(r)).APIMembers(r.Context(), n+1, after)
		if err != nil {
			failure(w, err)
			return
		}
		etag(w, "authz", version)
		page(w, items, n, func(v store.MemberSummary) string { return v.ID })
	})
	for _, action := range []string{"suspend", "restore"} {
		mux.HandleFunc("POST /v1/tenants/{tenant}/members/{member}/"+action, func(w http.ResponseWriter, r *http.Request) {
			var v struct{}
			if !decode(w, r, &v) {
				return
			}
			id, ok := pathID(w, r, "member")
			if !ok {
				return
			}
			version, ok := precondition(w, r, "authz")
			if !ok {
				return
			}
			var err error
			if action == "suspend" {
				err = repo.ForAuthorization(authority(r)).SuspendMember(r.Context(), id, version)
			} else {
				err = repo.ForAuthorization(authority(r)).APIRestoreMember(r.Context(), id, version)
			}
			if err != nil {
				failure(w, err)
				return
			}
			w.WriteHeader(204)
		})
	}
	mux.HandleFunc("GET /v1/tenants/{tenant}/organizations", func(w http.ResponseWriter, r *http.Request) {
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, version, err := repo.ForAuthorization(authority(r)).APIOrganizations(r.Context(), n+1, after)
		if err != nil {
			failure(w, err)
			return
		}
		type organization struct {
			ID     string  `json:"id"`
			Parent *string `json:"parent_id"`
			Name   string  `json:"name"`
		}
		out := []organization{}
		for _, v := range items {
			var p *string
			if v.Parent != nil {
				s := v.Parent.String()
				p = &s
			}
			out = append(out, organization{v.ID.String(), p, v.Name})
		}
		etag(w, "org", version)
		page(w, out, n, func(v organization) string { return v.ID })
	})
	mux.HandleFunc("POST /v1/tenants/{tenant}/organizations", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Parent string `json:"parent_id"`
			Name   string `json:"name"`
		}
		if !decode(w, r, &v) {
			return
		}
		parent, err := store.ParseID(v.Parent)
		if err != nil {
			problem(w, 400, "invalid_request")
			return
		}
		version, ok := precondition(w, r, "org")
		if !ok {
			return
		}
		id, err := repo.ForAuthorization(authority(r)).AddOrganization(r.Context(), parent, strings.TrimSpace(v.Name), version)
		if err != nil {
			failure(w, err)
			return
		}
		etag(w, "org", version+1)
		respond(w, 201, map[string]any{"id": id.String(), "org_version": version + 1})
	})
	mux.HandleFunc("POST /v1/tenants/{tenant}/organizations/{org}/move", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Parent string `json:"parent_id"`
			DryRun *bool  `json:"dry_run"`
		}
		if !decode(w, r, &v) {
			return
		}
		if v.DryRun == nil {
			problem(w, 400, "invalid_request")
			return
		}
		parent, err := store.ParseID(v.Parent)
		if err != nil {
			problem(w, 400, "invalid_request")
			return
		}
		id, ok := pathID(w, r, "org")
		if !ok {
			return
		}
		version, ok := precondition(w, r, "org")
		if !ok {
			return
		}
		plan, err := repo.ForAuthorization(authority(r)).MoveOrganization(r.Context(), id, parent, version, *v.DryRun)
		if err != nil {
			failure(w, err)
			return
		}
		next := plan.Version
		if !*v.DryRun {
			next++
		}
		respond(w, 200, map[string]any{"subtree_nodes": plan.Nodes, "resulting_depth": plan.Depth, "org_version": next, "dry_run": *v.DryRun})
	})
	mux.HandleFunc("PUT /v1/tenants/{tenant}/organizations/{org}/members/{member}", func(w http.ResponseWriter, r *http.Request) {
		var v struct{}
		if !decode(w, r, &v) {
			return
		}
		org, ok := pathID(w, r, "org")
		if !ok {
			return
		}
		member, ok := pathID(w, r, "member")
		if !ok {
			return
		}
		version, ok := precondition(w, r, "authz")
		if !ok {
			return
		}
		if err := repo.ForAuthorization(authority(r)).AssignOrganization(r.Context(), member, org, version); err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /v1/tenants/{tenant}/invitations", func(w http.ResponseWriter, r *http.Request) {
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, err := repo.ForAuthorization(authority(r)).APIInvitations(r.Context(), n+1, after)
		if err != nil {
			failure(w, err)
			return
		}
		page(w, items, n, func(v store.InvitationSummary) string { return v.ID })
	})
	mux.HandleFunc("POST /v1/tenants/{tenant}/invitations", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Email string `json:"email"`
		}
		if !decode(w, r, &v) {
			return
		}
		id, token, err := repo.ForAuthorization(authority(r)).CreateInvitation(r.Context(), v.Email)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 201, map[string]any{"id": id.String(), "token": token, "expires_in": 86400})
	})
	mux.HandleFunc("POST /v1/tenants/{tenant}/invitations/{invitation}/revoke", func(w http.ResponseWriter, r *http.Request) {
		var v struct{}
		if !decode(w, r, &v) {
			return
		}
		id, ok := pathID(w, r, "invitation")
		if !ok {
			return
		}
		if err := repo.ForAuthorization(authority(r)).APIRevokeInvitation(r.Context(), id); err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /v1/invitations/accept", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Token string `json:"token"`
		}
		if !decode(w, r, &v) {
			return
		}
		id, err := repo.APIAcceptInvitation(r.Context(), authority(r), v.Token)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 201, map[string]string{"membership_id": id.String()})
	})
	mux.HandleFunc("GET /v1/tenants/{tenant}/audit-events", func(w http.ResponseWriter, r *http.Request) {
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, err := repo.ForAuthorization(authority(r)).APIAudit(r.Context(), n+1, after)
		if err != nil {
			failure(w, err)
			return
		}
		page(w, items, n, func(v store.AuditSummary) string { return v.ID })
	})
	mux.HandleFunc("GET /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, err := repo.APISessions(r.Context(), authority(r), n+1, after)
		if err != nil {
			failure(w, err)
			return
		}
		page(w, items, n, func(v store.SessionSummary) string { return v.ID })
	})
	mux.HandleFunc("POST /v1/sessions/{session}/revoke", func(w http.ResponseWriter, r *http.Request) {
		var v struct{}
		if !decode(w, r, &v) {
			return
		}
		id, ok := pathID(w, r, "session")
		if !ok {
			return
		}
		if err := repo.APIRevokeSession(r.Context(), authority(r), id); err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { problem(w, 404, "not_found") })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := make([]byte, 12)
		if _, err := rand.Read(requestID); err != nil {
			problem(w, 503, "temporarily_unavailable")
			return
		}
		w.Header().Set("X-Request-ID", hex.EncodeToString(requestID))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		if r.URL.RawPath != "" || strings.Contains(r.URL.Path, "//") || strings.Contains(r.URL.Path, "/../") || strings.Contains(r.URL.Path, "/./") {
			problem(w, 400, "invalid_request")
			return
		}
		if r.Host != "api.localhost:8083" {
			problem(w, 400, "invalid_host")
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			problem(w, 400, "invalid_request")
			return
		}
		if !limit.Allow("ip:"+host, 600, time.Minute) {
			w.Header().Set("Retry-After", "60")
			problem(w, 429, "rate_limited")
			return
		}
		select {
		case capacity <- struct{}{}:
			defer func() { <-capacity }()
		default:
			problem(w, 503, "temporarily_unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		if r.URL.Path == "/openapi.yaml" || r.URL.Path == "/openapi.json" || r.URL.Path == "/docs" || r.URL.Path == "/docs.css" || r.URL.Path == "/health/ready" {
			if r.Method != "GET" || r.URL.RawQuery != "" {
				problem(w, 400, "invalid_request")
				return
			}
			mux.ServeHTTP(w, r)
			return
		}
		raw, err := bearer(r)
		if err != nil {
			unauthorized(w)
			return
		}
		p, err := auth.Authenticate(ctx, raw)
		if errors.Is(err, store.ErrDenied) {
			unauthorized(w)
			return
		}
		if err != nil {
			failure(w, err)
			return
		}
		scope := "platform:read"
		if r.Method != "GET" && r.Method != "HEAD" {
			scope = "platform:write"
			p.Authority.Recent = true
		}
		if !p.Scopes[scope] {
			w.Header().Set("WWW-Authenticate", `Bearer realm="platform", error="insufficient_scope", scope="`+scope+`"`)
			problem(w, 403, "insufficient_scope")
			return
		}
		if !limit.Allow("user:"+p.Authority.User.String(), 300, time.Minute) {
			w.Header().Set("Retry-After", "60")
			problem(w, 429, "rate_limited")
			return
		}
		// Reject tenant hints; path context must equal the server-bound token context.
		if hint := r.Header.Get("X-Tenant-ID"); hint != "" && hint != p.Authority.Tenant.String() {
			problem(w, 403, "tenant_mismatch")
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 3 && parts[0] == "v1" && parts[1] == "tenants" && parts[2] != p.Authority.Tenant.String() {
			problem(w, 404, "not_found")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			kind, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if e != nil || kind != "application/json" {
				problem(w, 415, "unsupported_media_type")
				return
			}
		}
		if r.URL.RawQuery != "" && r.Method != "GET" {
			problem(w, 400, "invalid_request")
			return
		}
		if r.URL.RawQuery != "" {
			if _, _, ok := pagination(w, r); !ok {
				return
			}
			if !isList(r.URL.Path) {
				problem(w, 400, "invalid_request")
				return
			}
		}
		mux.ServeHTTP(w, r.WithContext(context.WithValue(ctx, contextKey{}, p)))
	})
}
func isList(path string) bool {
	return path == "/v1/me/security-events" || path == "/v1/tenants" || path == "/v1/sessions" || strings.HasSuffix(path, "/members") || strings.HasSuffix(path, "/organizations") || strings.HasSuffix(path, "/invitations") || strings.HasSuffix(path, "/audit-events") || (strings.HasSuffix(path, "/roles") && !strings.Contains(path, "/members/"))
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"type": "urn:sso:problem:" + code, "title": http.StatusText(status), "status": status, "code": code, "request_id": w.Header().Get("X-Request-ID")})
}
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="platform", error="invalid_token"`)
	problem(w, 401, "invalid_token")
}
func failure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrStepUp):
		problem(w, 403, "recent_mfa_required")
	case errors.Is(err, store.ErrDenied):
		problem(w, 403, "forbidden")
	case errors.Is(err, store.ErrNotFound):
		problem(w, 404, "not_found")
	case errors.Is(err, store.ErrConflict):
		problem(w, 412, "version_conflict")
	case errors.Is(err, store.ErrReservedRole):
		problem(w, 409, "reserved_role")
	case errors.Is(err, store.ErrRoleInUse):
		problem(w, 409, "role_in_use")
	case errors.Is(err, store.ErrLastOwner):
		problem(w, 409, "last_active_owner")
	case errors.Is(err, store.ErrRoot):
		problem(w, 409, "root_cannot_move")
	case errors.Is(err, store.ErrCycle):
		problem(w, 409, "organization_cycle")
	case errors.Is(err, store.ErrInvalid):
		problem(w, 400, "invalid_request")
	default:
		var dbErr *driver.MySQLError
		if errors.As(err, &dbErr) && dbErr.Number == 1062 {
			problem(w, 409, "already_exists")
		} else {
			problem(w, 503, "temporarily_unavailable")
		}
	}
}
func pathID(w http.ResponseWriter, r *http.Request, name string) (store.ID, bool) {
	value := r.PathValue(name)
	id, err := store.ParseID(value)
	if err != nil || value != id.String() {
		problem(w, 400, "invalid_request")
		return id, false
	}
	return id, true
}
func precondition(w http.ResponseWriter, r *http.Request, kind string) (int64, bool) {
	value := r.Header.Get("If-Match")
	if value == "" {
		problem(w, 428, "precondition_required")
		return 0, false
	}
	prefix := `"` + kind + `-`
	if len(r.Header.Values("If-Match")) != 1 || !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, `"`) {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(value, prefix), `"`), 10, 64)
	if err != nil || n < 1 || value != prefix+strconv.FormatInt(n, 10)+`"` {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	return n, true
}
func etag(w http.ResponseWriter, kind string, version int64) {
	w.Header().Set("ETag", `"`+kind+`-`+strconv.FormatInt(version, 10)+`"`)
}
func pagination(w http.ResponseWriter, r *http.Request) (int, store.ID, bool) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		problem(w, 400, "invalid_request")
		return 0, store.ID{}, false
	}
	for key, v := range q {
		if (key != "limit" && key != "after") || len(v) != 1 || v[0] == "" {
			problem(w, 400, "invalid_request")
			return 0, store.ID{}, false
		}
	}
	n := 50
	if v := q.Get("limit"); v != "" {
		n, err = strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			problem(w, 400, "invalid_request")
			return 0, store.ID{}, false
		}
	}
	var after store.ID
	if value := q.Get("after"); value != "" {
		after, err = store.ParseID(value)
		if err != nil || value != after.String() {
			problem(w, 400, "invalid_request")
			return 0, after, false
		}
	}
	return n, after, true
}
func page[T any](w http.ResponseWriter, items []T, n int, id func(T) string) {
	var next *string
	if len(items) > n {
		items = items[:n]
		s := id(items[len(items)-1])
		next = &s
	}
	respond(w, 200, map[string]any{"items": items, "next_cursor": next})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool { return decodeJSON(w, r, v, false) }
func decodeJSON(w http.ResponseWriter, r *http.Request, v any, nested bool) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			problem(w, 413, "request_too_large")
		} else {
			problem(w, 400, "invalid_request")
		}
		return false
	}
	valid := strictObject(body)
	if nested {
		valid = strictNestedObject(body)
	}
	if !utf8.Valid(body) || !valid {
		problem(w, 400, "invalid_request")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		problem(w, 400, "invalid_request")
		return false
	}
	return true
}
func strictObject(body []byte) bool {
	d := json.NewDecoder(bytes.NewReader(body))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		if e != nil {
			return false
		}
		s, ok := key.(string)
		if !ok || seen[s] {
			return false
		}
		seen[s] = true
		var value any
		if d.Decode(&value) != nil {
			return false
		}
		switch value.(type) {
		case map[string]any, []any:
			return false
		}
	}
	if _, e = d.Token(); e != nil {
		return false
	}
	var extra any
	return d.Decode(&extra) == io.EOF
}
