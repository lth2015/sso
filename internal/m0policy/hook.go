// Package m0policy validates synthetic, server-owned protocol laboratory contexts.
// It intentionally does not implement production membership/session management.
package m0policy

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type Request struct {
	Session struct {
		IDToken struct {
			Subject string `json:"subject"`
		} `json:"id_token"`
		Extra map[string]any `json:"extra"`
	} `json:"session"`
	Request struct {
		ClientID        string   `json:"client_id"`
		GrantedScopes   []string `json:"granted_scopes"`
		GrantedAudience []string `json:"granted_audience"`
		GrantTypes      []string `json:"grant_types"`
	} `json:"request"`
}

type Policy struct {
	Tenant, Principal, Kind, Subject, ContextID, Audience, Scopes string
	Active                                                        bool
	Version                                                       int64
	Expires                                                       int64
}

// Evaluate permits only exact subsets and a single server-bound tenant context.
func Evaluate(p Policy, r Request, now time.Time) bool {
	if !p.Active || p.Expires <= now.Unix() || p.Version < 1 || p.Tenant == "" || p.Principal == "" || p.ContextID == "" {
		return false
	}
	if len(r.Request.GrantTypes) != 1 {
		return false
	}
	scopes, audiences := r.Request.GrantedScopes, r.Request.GrantedAudience
	// Hydra 2.3 invokes the code hook before populating GrantedScopes/Audience.
	// In that phase require the immutable server-owned consent binding instead.
	// An empty grant without this trusted context never authorizes issuance.
	if p.Kind == "user" && len(scopes) == 0 && len(audiences) == 0 {
		aud, ok := r.Session.Extra["m0_grant_audience"].(string)
		if !ok {
			return false
		}
		list, ok := r.Session.Extra["m0_grant_scopes"].([]any)
		if !ok {
			return false
		}
		audiences = []string{aud}
		for _, value := range list {
			scope, ok := value.(string)
			if !ok {
				return false
			}
			scopes = append(scopes, scope)
		}
	}
	if len(audiences) != 1 || audiences[0] != p.Audience {
		return false
	}
	allowed := map[string]bool{}
	for _, s := range strings.Fields(p.Scopes) {
		allowed[s] = true
	}
	if len(scopes) == 0 {
		return false
	}
	for _, s := range scopes {
		if !allowed[s] {
			return false
		}
	}
	grant := r.Request.GrantTypes[0]
	if p.Kind == "service" {
		return grant == "client_credentials"
	}
	if p.Kind != "user" || (grant != "authorization_code" && grant != "refresh_token") || r.Session.IDToken.Subject != p.Subject {
		return false
	}
	// Consent binds the opaque context; neither refresh form fields nor a tenant hint can replace it.
	return r.Session.Extra["m0_context_id"] == p.ContextID
}

func Handler(db *sql.DB, key string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if db.PingContext(ctx) != nil {
			http.Error(w, "not_ready", 503)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /token-hook", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Hook-Key")), []byte(key)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		var req Request
		decoder := json.NewDecoder(r.Body)
		if decoder.Decode(&req) != nil {
			http.Error(w, "invalid_request", 400)
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			http.Error(w, "invalid_request", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			http.Error(w, "authority_unavailable", 503)
			return
		}
		defer tx.Rollback()
		var p Policy
		err = tx.QueryRowContext(ctx, `SELECT tenant_id,principal_id,principal_kind,subject_id,context_id,audience,scopes,active,security_version,expires_at FROM policies WHERE client_id=? FOR SHARE`, req.Request.ClientID).Scan(&p.Tenant, &p.Principal, &p.Kind, &p.Subject, &p.ContextID, &p.Audience, &p.Scopes, &p.Active, &p.Version, &p.Expires)
		if err == sql.ErrNoRows {
			http.Error(w, "denied", 403)
			return
		}
		if err != nil {
			http.Error(w, "authority_unavailable", 503)
			return
		}
		granted := Evaluate(p, req, time.Now())
		if !granted {
			log.Printf("M0 denied: active=%t scopes=%d audiences=%d grants=%v subject_match=%t context_match=%t", p.Active, len(req.Request.GrantedScopes), len(req.Request.GrantedAudience), req.Request.GrantTypes, req.Session.IDToken.Subject == p.Subject, req.Session.Extra["m0_context_id"] == p.ContextID)
		}
		grant := "invalid"
		if len(req.Request.GrantTypes) == 1 {
			grant = req.Request.GrantTypes[0]
		}
		// Only controlled metadata is persisted, never the incoming request or tokens.
		if grant != "authorization_code" && grant != "refresh_token" && grant != "client_credentials" {
			grant = "invalid"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO hook_observations(client_id,grant_type,allowed) VALUES(?,?,?)`, req.Request.ClientID, grant, granted); err != nil {
			http.Error(w, "audit_unavailable", 503)
			return
		}
		if tx.Commit() != nil {
			http.Error(w, "audit_unavailable", 503)
			return
		}
		if !granted {
			http.Error(w, "denied", 403)
			return
		}
		claims := map[string]any{"tenant_id": p.Tenant, "principal_id": p.Principal, "principal_type": p.Kind, "security_version": p.Version, "m0_context_id": p.ContextID}
		if p.Kind == "user" {
			claims["m0_grant_audience"] = req.Session.Extra["m0_grant_audience"]
			claims["m0_grant_scopes"] = req.Session.Extra["m0_grant_scopes"]
		}
		if p.Kind == "service" {
			claims["credential_id"] = req.Request.ClientID
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"access_token": claims, "id_token": map[string]any{}}})
	})
	return mux
}
