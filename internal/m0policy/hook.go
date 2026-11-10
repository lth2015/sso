// Package m0policy validates synthetic, server-owned protocol laboratory contexts.
// It intentionally does not implement production membership/session management.
package m0policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Request struct {
	Session struct {
		IDToken struct {
			Subject string `json:"subject"`
			Claims  struct {
				Extra map[string]any `json:"ext"`
			} `json:"id_token_claims"`
		} `json:"id_token"`
		Extra            map[string]any `json:"extra"`
		ConsentChallenge string         `json:"consent_challenge"`
	} `json:"session"`
	Request struct {
		ClientID        string   `json:"client_id"`
		RequestedScopes []string `json:"requested_scopes"`
		Payload         struct {
			Scopes    []string `json:"scope"`
			Audiences []string `json:"audience"`
		} `json:"payload"`
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
	if len(r.Request.RequestedScopes) > 0 && !scopeSubset(r.Request.RequestedScopes, p.Scopes) {
		return false
	}
	for _, value := range r.Request.Payload.Scopes {
		if !scopeSubset(strings.Fields(value), p.Scopes) {
			return false
		}
	}
	for _, audience := range r.Request.Payload.Audiences {
		if audience != p.Audience {
			return false
		}
	}
	scopes, audiences := r.Request.GrantedScopes, r.Request.GrantedAudience
	// Hydra invokes the code hook before populating grants. The caller must
	// resolve a server-owned per-transaction DB context before Evaluate.
	if p.Kind == "user" && len(scopes) == 0 && len(audiences) == 0 {
		if r.Session.Extra["m0_context_id"] != p.ContextID {
			return false
		}
		scopes = strings.Fields(p.Scopes)
		audiences = []string{p.Audience}
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
		var pendingBinding []byte
		var loginSessionID string
		if p.Kind == "user" {
			contextID, ok := req.Session.Extra["m0_context_id"].(string)
			if !ok || contextID == "" || len(contextID) > 128 {
				http.Error(w, "denied", 403)
				return
			}
			registry := p
			var binding []byte
			var qualificationActive, contextActive bool
			var qualificationVersion, boundQualificationVersion, boundClientVersion int64
			err = tx.QueryRowContext(ctx, `SELECT c.tenant_id,c.principal_id,c.subject_id,c.id,c.audience,c.scopes,c.active,c.absolute_expires_at,c.protocol_binding,q.active,q.security_version,c.qualification_version,c.client_version,c.login_session_id
              FROM authorization_contexts c JOIN tenant_qualifications q ON q.client_id=c.client_id AND q.tenant_id=c.tenant_id AND q.subject_id=c.subject_id
              WHERE c.id=? AND c.client_id=? FOR UPDATE OF c FOR SHARE OF q`, contextID, req.Request.ClientID).Scan(&p.Tenant, &p.Principal, &p.Subject, &p.ContextID, &p.Audience, &p.Scopes, &contextActive, &p.Expires, &binding, &qualificationActive, &qualificationVersion, &boundQualificationVersion, &boundClientVersion, &loginSessionID)
			if err == sql.ErrNoRows {
				http.Error(w, "denied", 403)
				return
			}
			if err != nil {
				var databaseError *mysql.MySQLError
				if errors.As(err, &databaseError) {
					log.Printf("context_query_failed database_code=%d", databaseError.Number)
				}
				http.Error(w, "authority_unavailable", 503)
				return
			}
			// Binding is independent from the extensible claims. A context ID
			// copied from another authorization transaction cannot rebind its tenant.
			actualBinding := sha256.Sum256([]byte(req.Session.ConsentChallenge))
			p.Active = registry.Active && contextActive && qualificationActive && qualificationVersion == boundQualificationVersion && registry.Version == boundClientVersion && req.Session.IDToken.Claims.Extra["sid"] == loginSessionID && loginSessionID != "" && req.Session.ConsentChallenge != ""
			if binding == nil {
				p.Active = p.Active && len(req.Request.GrantTypes) == 1 && req.Request.GrantTypes[0] == "authorization_code"
				pendingBinding = actualBinding[:]
			} else {
				p.Active = p.Active && bytes.Equal(actualBinding[:], binding)
			}
			if registry.Expires < p.Expires {
				p.Expires = registry.Expires
			}
			p.Version = qualificationVersion
			if p.Audience != registry.Audience || !scopeSubset(strings.Fields(p.Scopes), registry.Scopes) {
				p.Active = false
			}
		}
		granted := Evaluate(p, req, time.Now())
		if granted && pendingBinding != nil {
			result, err := tx.ExecContext(ctx, `UPDATE authorization_contexts SET protocol_binding=? WHERE id=? AND protocol_binding IS NULL`, pendingBinding, p.ContextID)
			if err != nil {
				http.Error(w, "authority_unavailable", 503)
				return
			}
			count, err := result.RowsAffected()
			if err != nil || count != 1 {
				http.Error(w, "authority_unavailable", 503)
				return
			}
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

		if p.Kind == "service" {
			claims["credential_id"] = req.Request.ClientID
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"access_token": claims, "id_token": idTokenClaims(p, loginSessionID)}})
	})
	return mux
}

func scopeSubset(scopes []string, upper string) bool {
	allowed := map[string]bool{}
	for _, s := range strings.Fields(upper) {
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
	return true
}

func idTokenClaims(p Policy, sid string) map[string]any {
	if p.Kind == "user" {
		return map[string]any{"sid": sid}
	}
	return map[string]any{}
}
