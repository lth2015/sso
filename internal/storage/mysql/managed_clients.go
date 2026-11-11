package mysql

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lth2015/sso/internal/adapter/hydra"
)

var ErrClientState = errors.New("invalid_client_state")
var ErrApplicationUnmanaged = errors.New("application_unmanaged")

// Redirects are compared exactly by the engine and trusted login adapter.
// This service never fetches them. HTTP is limited to explicit IP loopback
// and a fixed unprivileged port; HTTPS has no fragment, credentials or wildcard.
func ValidManagedRedirect(value string) bool {
	if len(value) > 255 || len(value) == 0 || strings.ContainsAny(value, "\\*\r\n\t ") {
		return false
	}
	for _, c := range value {
		if c < 33 || c > 126 {
			return false
		}
	}
	u, e := url.Parse(value)
	if e != nil || u.Opaque != "" || u.User != nil || u.Fragment != "" || u.Hostname() == "" || u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		return false
	}
	if u.String() != value || u.RawFragment != "" || strings.Contains(value, "#") {
		return false
	}
	port := u.Port()
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return false
		}
	}
	if u.Scheme == "http" {
		if u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
			return false
		}
		n, e := strconv.Atoi(port)
		return e == nil && n >= 1024 && n <= 65535
	}
	return u.Scheme == "https" && !strings.Contains(u.Hostname(), "%")
}
func validManagedName(name string) bool {
	return name != "" && len(name) <= 128 && utf8.ValidString(name)
}

type ManagedClient struct {
	ID          string   `json:"client_id"`
	Application string   `json:"application_id"`
	Name        string   `json:"name"`
	Redirect    string   `json:"redirect_uri"`
	Scopes      []string `json:"scopes"`
	Audience    string   `json:"audience"`
	State       string   `json:"state"`
	Active      bool     `json:"active"`
	Version     int64    `json:"version"`
}

const managedClientSelect = `SELECT c.client_id,LOWER(HEX(c.application_id)),mc.name,c.redirect_uri,c.scopes,c.audience,mc.state,c.active,c.security_version FROM managed_oauth_clients mc JOIN oauth_clients c ON c.client_id=mc.client_id AND c.application_id=mc.application_id WHERE mc.tenant_id=? AND mc.application_id=? AND mc.client_id=?`

func scanManagedClient(row scanner) (v ManagedClient, err error) {
	var scopes string
	err = row.Scan(&v.ID, &v.Application, &v.Name, &v.Redirect, &scopes, &v.Audience, &v.State, &v.Active, &v.Version)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	v.Scopes = strings.Fields(scopes)
	return
}
func (s TenantStore) ownedApplication(ctx context.Context, tx *sql.Tx, app ID, requireAvailable bool) error {
	var active bool
	e := tx.QueryRowContext(ctx, `SELECT a.active AND ta.active FROM managed_applications own JOIN applications a ON a.id=own.application_id JOIN tenant_applications ta ON ta.tenant_id=own.tenant_id AND ta.application_id=own.application_id WHERE own.tenant_id=? AND own.application_id=?`, s.tenant, app).Scan(&active)
	if e == sql.ErrNoRows {
		var id ID
		if e = tx.QueryRowContext(ctx, `SELECT application_id FROM tenant_applications WHERE tenant_id=? AND application_id=?`, s.tenant, app).Scan(&id); e == sql.ErrNoRows {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		return ErrApplicationUnmanaged
	}
	if e != nil {
		return e
	}
	if requireAvailable && !active {
		return ErrApplicationUnavailable
	}
	return nil
}
func (s TenantStore) APICreateApplication(ctx context.Context, name string) (v ApplicationSummary, err error) {
	name = strings.TrimSpace(name)
	if !validManagedName(name) {
		return v, ErrInvalid
	}
	s = s.withRecentMFA()
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		var n int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_applications WHERE tenant_id=?`, s.tenant).Scan(&n); e != nil {
			return e
		}
		if n >= 20 {
			return ErrInvalid
		}
		id, e := NewID()
		if e != nil {
			return e
		}
		code := "managed-" + id.String()
		if _, e = tx.ExecContext(ctx, `INSERT INTO applications(id,code,name) VALUES(?,?,?)`, id, code, name); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO tenant_applications(tenant_id,application_id) VALUES(?,?)`, s.tenant, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO managed_applications(tenant_id,application_id,creator_membership_id) VALUES(?,?,?)`, s.tenant, id, s.actor); e != nil {
			return e
		}
		if e = record(ctx, tx, s.tenant, s.actor, "application.created", id, 1); e != nil {
			return e
		}
		v, e = scanApplication(tx.QueryRowContext(ctx, applicationSelect+` AND a.id=?`, s.tenant, id))
		return e
	})
	return
}
func (s TenantStore) APICreateManagedClient(ctx context.Context, app ID, name, redirect string) (v ManagedClient, err error) {
	name = strings.TrimSpace(name)
	if !validManagedName(name) || !ValidManagedRedirect(redirect) {
		return v, ErrInvalid
	}
	s = s.withRecentMFA()
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if e := s.ownedApplication(ctx, tx, app, true); e != nil {
			return e
		}
		var n int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_oauth_clients WHERE tenant_id=? AND application_id=?`, s.tenant, app).Scan(&n); e != nil {
			return e
		}
		if n >= 100 {
			return ErrInvalid
		}
		id, e := NewID()
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO oauth_clients(client_id,application_id,redirect_uri,scopes,audience,active) VALUES(?,?,?,?,?,FALSE)`, id.String(), app, redirect, hydra.ManagedClientScopes, hydra.ManagedClientAudience); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO managed_oauth_clients(tenant_id,application_id,client_id,creator_membership_id,name) VALUES(?,?,?,?,?)`, s.tenant, app, id.String(), s.actor, name); e != nil {
			return e
		}
		if e = record(ctx, tx, s.tenant, s.actor, "client.registration_pending", id, 1); e != nil {
			return e
		}
		v, e = scanManagedClient(tx.QueryRowContext(ctx, managedClientSelect, s.tenant, app, id.String()))
		return e
	})
	return
}
func (s TenantStore) APIManagedClient(ctx context.Context, app, client ID) (v ManagedClient, err error) {
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if e := s.ownedApplication(ctx, tx, app, false); e != nil {
			return e
		}
		var e error
		v, e = scanManagedClient(tx.QueryRowContext(ctx, managedClientSelect, s.tenant, app, client.String()))
		return e
	})
	return
}
func (s TenantStore) RevokeManagedClient(ctx context.Context, app, client ID, expected int64) (v ManagedClient, err error) {
	if expected < 1 {
		return v, ErrInvalid
	}
	s = s.withRecentMFA()
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if e := s.ownedApplication(ctx, tx, app, false); e != nil {
			return e
		}
		var e error
		v, e = scanManagedClient(tx.QueryRowContext(ctx, managedClientSelect, s.tenant, app, client.String()))
		if e != nil {
			return e
		}
		if v.Version != expected {
			return ErrConflict
		}
		if v.State == "revoked" {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE oauth_clients SET active=FALSE,security_version=security_version+1 WHERE client_id=?`, client.String()); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE managed_oauth_clients SET state='revoked' WHERE tenant_id=? AND application_id=? AND client_id=?`, s.tenant, app, client.String()); e != nil {
			return e
		}
		v.State = "revoked"
		v.Active = false
		v.Version++
		return record(ctx, tx, s.tenant, s.actor, "client.revoked", client, v.Version)
	})
	return
}

// Reconcile holds the same bounded tenant transaction used by revocation. No
// ordinary concurrent reconcile can publish a client after revocation. An
// ambiguous engine/DB result leaves the local authority fail-closed. Revoked
// tombstones are retained; protocol absence is an observation, not a promise
// that a timed-out remote transaction can never finish later.
func (s TenantStore) ReconcileManagedClient(ctx context.Context, app, client ID, expected int64, admin *hydra.Client) (v ManagedClient, observation string, err error) {
	if expected < 1 || admin == nil {
		return v, "", ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	s = s.withRecentMFA()
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if e := s.ownedApplication(ctx, tx, app, false); e != nil {
			return e
		}
		var e error
		v, e = scanManagedClient(tx.QueryRowContext(ctx, managedClientSelect, s.tenant, app, client.String()))
		if e != nil {
			return e
		}
		if v.Version != expected {
			return ErrConflict
		}
		profile := hydra.ManagedPublicClient(v.ID, v.Name, v.Redirect, s.tenant.String(), app.String())
		if v.State == "revoked" {
			if e = admin.RemovePublicClient(ctx, profile); e != nil {
				return e
			}
			observation = "absent"
			return record(ctx, tx, s.tenant, s.actor, "client.protocol_cleanup_checked", client, v.Version)
		}
		if e = s.ownedApplication(ctx, tx, app, true); e != nil {
			return e
		}
		if e = admin.EnsurePublicClient(ctx, profile); e != nil {
			return e
		}
		observation = "present"
		if v.State == "active" {
			return nil
		}
		if v.State != "pending" {
			return ErrClientState
		}
		if _, e = tx.ExecContext(ctx, `UPDATE oauth_clients SET active=TRUE,security_version=security_version+1 WHERE client_id=?`, client.String()); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE managed_oauth_clients SET state='active' WHERE tenant_id=? AND application_id=? AND client_id=?`, s.tenant, app, client.String()); e != nil {
			return e
		}
		v.State = "active"
		v.Active = true
		v.Version++
		return record(ctx, tx, s.tenant, s.actor, "client.activated", client, v.Version)
	})
	return
}
