package mysql

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/lth2015/sso/internal/adapter/hydra"
)

const MachineScope = "machine:invoke"

func MachineAudience(app ID) string { return "urn:sso:application:" + app.String() }

var ErrPrincipalDisabled = errors.New("service_principal_disabled")
var ErrCredentialLimit = errors.New("credential_limit")
var ErrCredentialState = errors.New("invalid_credential_state")

type ServicePrincipalSummary struct {
	ID              string `json:"id"`
	Application     string `json:"application_id"`
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	Version         int64  `json:"version"`
	CredentialCount int    `json:"credential_count"`
}

const principalSelect = `SELECT LOWER(HEX(sp.id)),LOWER(HEX(sp.application_id)),sp.name,sp.enabled,sp.security_version,(SELECT COUNT(*) FROM machine_credentials c WHERE c.tenant_id=sp.tenant_id AND c.application_id=sp.application_id AND c.service_principal_id=sp.id) FROM service_principals sp WHERE sp.tenant_id=? AND sp.application_id=?`

func scanPrincipal(row scanner) (v ServicePrincipalSummary, err error) {
	err = row.Scan(&v.ID, &v.Application, &v.Name, &v.Enabled, &v.Version, &v.CredentialCount)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	return
}
func (s TenantStore) principal(ctx context.Context, tx *sql.Tx, app, principal ID, active bool) (v ServicePrincipalSummary, err error) {
	if err = s.ownedApplication(ctx, tx, app, active); err != nil {
		return
	}
	v, err = scanPrincipal(tx.QueryRowContext(ctx, principalSelect+` AND sp.id=?`, s.tenant, app, principal))
	if err == nil && active && !v.Enabled {
		err = ErrPrincipalDisabled
	}
	return
}
func (s TenantStore) APICreatePrincipal(ctx context.Context, app ID, name string) (v ServicePrincipalSummary, err error) {
	name = strings.TrimSpace(name)
	if !validManagedName(name) {
		return v, ErrInvalid
	}
	s = s.withRecentMFA()
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if e := s.ownedApplication(ctx, tx, app, true); e != nil {
			return e
		}
		var n int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM service_principals WHERE tenant_id=? AND application_id=?`, s.tenant, app).Scan(&n); e != nil {
			return e
		}
		if n >= 100 {
			return ErrCredentialLimit
		}
		id, e := NewID()
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO service_principals(id,tenant_id,application_id,creator_membership_id,name) VALUES(?,?,?,?,?)`, id, s.tenant, app, s.actor, name); e != nil {
			return e
		}
		if e = record(ctx, tx, s.tenant, s.actor, "service_principal.created", id, 1); e != nil {
			return e
		}
		v, e = scanPrincipal(tx.QueryRowContext(ctx, principalSelect+` AND sp.id=?`, s.tenant, app, id))
		return e
	})
	return
}
func (s TenantStore) APIPrincipals(ctx context.Context, app ID, limit int, after ID) (items []ServicePrincipalSummary, err error) {
	items = []ServicePrincipalSummary{}
	if limit < 1 || limit > 101 {
		return items, ErrInvalid
	}
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if e := s.ownedApplication(ctx, tx, app, false); e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, principalSelect+` AND sp.id>? ORDER BY sp.id LIMIT ?`, s.tenant, app, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanPrincipal(rows)
			if e != nil {
				return e
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	return
}
func (s TenantStore) APIPrincipal(ctx context.Context, app, principal ID) (v ServicePrincipalSummary, err error) {
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		var e error
		v, e = s.principal(ctx, tx, app, principal, false)
		return e
	})
	return
}
func (s TenantStore) SetPrincipalEnabled(ctx context.Context, app, principal ID, enabled bool, expected int64) (v ServicePrincipalSummary, err error) {
	if expected < 1 {
		return v, ErrInvalid
	}
	s = s.withRecentMFA()
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		var e error
		v, e = s.principal(ctx, tx, app, principal, false)
		if e != nil {
			return e
		}
		if v.Version != expected {
			return ErrConflict
		}
		if enabled {
			if e = s.ownedApplication(ctx, tx, app, true); e != nil {
				return e
			}
		}
		if v.Enabled == enabled {
			return nil
		}
		v.Enabled = enabled
		v.Version++
		if _, e = tx.ExecContext(ctx, `UPDATE service_principals SET enabled=?,security_version=security_version+1 WHERE tenant_id=? AND application_id=? AND id=?`, enabled, s.tenant, app, principal); e != nil {
			return e
		}
		action := "service_principal.disabled"
		if enabled {
			action = "service_principal.enabled"
		}
		return record(ctx, tx, s.tenant, s.actor, action, principal, v.Version)
	})
	return
}

type MachineCredentialSummary struct {
	ID                                                                        string `json:"id"`
	Application                                                               string `json:"application_id"`
	Principal                                                                 string `json:"service_principal_id"`
	AccessKey                                                                 string `json:"access_key"`
	Name                                                                      string `json:"name"`
	State                                                                     string `json:"state"`
	Active                                                                    bool   `json:"active"`
	Expires                                                                   int64  `json:"expires_at"`
	Version                                                                   int64  `json:"version"`
	Audience                                                                  string `json:"audience"`
	Scope                                                                     string `json:"scope"`
	securityVersion                                                           int64
	pendingPrincipalVersion, pendingApplicationVersion, pendingEnabledVersion uint64
}

// API revision tracks metadata/overlap changes; security_version only invalidates
// token authority. Expiry is checked live even when an older JWT has a longer hint.
const credentialSelect = `SELECT LOWER(HEX(c.id)),LOWER(HEX(c.application_id)),LOWER(HEX(c.service_principal_id)),c.client_id,c.name,CASE WHEN c.state<>'revoked' AND c.expires_at<=UNIX_TIMESTAMP() THEN 'expired' ELSE c.state END,(c.state='active' AND c.expires_at>UNIX_TIMESTAMP()),c.expires_at,c.version,c.security_version FROM machine_credentials c WHERE c.tenant_id=? AND c.application_id=? AND c.service_principal_id=?`

func scanMachineCredential(row scanner) (v MachineCredentialSummary, err error) {
	err = row.Scan(&v.ID, &v.Application, &v.Principal, &v.AccessKey, &v.Name, &v.State, &v.Active, &v.Expires, &v.Version, &v.securityVersion)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	v.Scope = MachineScope
	v.Audience = "urn:sso:application:" + v.Application
	return
}
func (s TenantStore) APIMachineCredentials(ctx context.Context, app, principal ID, limit int, after ID) (items []MachineCredentialSummary, err error) {
	items = []MachineCredentialSummary{}
	if limit < 1 || limit > 101 {
		return items, ErrInvalid
	}
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if _, e := s.principal(ctx, tx, app, principal, false); e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, credentialSelect+` AND c.id>? ORDER BY c.id LIMIT ?`, s.tenant, app, principal, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanMachineCredential(rows)
			if e != nil {
				return e
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	return
}
func (s TenantStore) APIMachineCredential(ctx context.Context, app, principal, credential ID) (v MachineCredentialSummary, err error) {
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if _, e := s.principal(ctx, tx, app, principal, false); e != nil {
			return e
		}
		var e error
		v, e = scanMachineCredential(tx.QueryRowContext(ctx, credentialSelect+` AND c.id=?`, s.tenant, app, principal, credential))
		return e
	})
	return
}
func (s TenantStore) beginMachineCredential(ctx context.Context, app, principal ID, name string, days int) (v MachineCredentialSummary, err error) {
	name = strings.TrimSpace(name)
	if !validManagedName(name) || days < 1 || days > 90 {
		return v, ErrInvalid
	}
	s = s.withRecentMFA()
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		sp, e := s.principal(ctx, tx, app, principal, true)
		if e != nil {
			return e
		}
		var applicationVersion, enabledVersion uint64
		if e = tx.QueryRowContext(ctx, `SELECT a.security_version,ta.security_version FROM applications a JOIN tenant_applications ta ON ta.application_id=a.id WHERE ta.tenant_id=? AND a.id=? FOR SHARE`, s.tenant, app).Scan(&applicationVersion, &enabledVersion); e != nil {
			return e
		}
		var live, total int
		if e := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(state IN ('pending','active') AND expires_at>UNIX_TIMESTAMP()),0),COUNT(*) FROM machine_credentials WHERE tenant_id=? AND application_id=? AND service_principal_id=?`, s.tenant, app, principal).Scan(&live, &total); e != nil {
			return e
		}
		if live >= 2 || total >= 100 {
			return ErrCredentialLimit
		}
		id, e := NewID()
		if e != nil {
			return e
		}
		expires := time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
		if _, e = tx.ExecContext(ctx, `INSERT INTO machine_credentials(id,tenant_id,application_id,service_principal_id,creator_membership_id,client_id,name,expires_at) VALUES(?,?,?,?,?,?,?,?)`, id, s.tenant, app, principal, s.actor, "ak_"+id.String(), name, expires); e != nil {
			return e
		}
		if e = record(ctx, tx, s.tenant, s.actor, "credential.pending", id, 1); e != nil {
			return e
		}
		v, e = scanMachineCredential(tx.QueryRowContext(ctx, credentialSelect+` AND c.id=?`, s.tenant, app, principal, id))
		v.pendingPrincipalVersion = uint64(sp.Version)
		v.pendingApplicationVersion = applicationVersion
		v.pendingEnabledVersion = enabledVersion
		return e
	})
	return
}
func (s TenantStore) activateMachineCredential(ctx context.Context, app, principal, credential ID, binding MachineCredentialSummary) (v MachineCredentialSummary, err error) {
	s = s.withRecentMFA()
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		sp, e := s.principal(ctx, tx, app, principal, true)
		if e != nil {
			return e
		}
		var applicationVersion, enabledVersion uint64
		if e = tx.QueryRowContext(ctx, `SELECT a.security_version,ta.security_version FROM applications a JOIN tenant_applications ta ON ta.application_id=a.id WHERE ta.tenant_id=? AND a.id=? FOR SHARE`, s.tenant, app).Scan(&applicationVersion, &enabledVersion); e != nil {
			return e
		}
		if uint64(sp.Version) != binding.pendingPrincipalVersion || applicationVersion != binding.pendingApplicationVersion || enabledVersion != binding.pendingEnabledVersion {
			return ErrCredentialState
		}
		v, e = scanMachineCredential(tx.QueryRowContext(ctx, credentialSelect+` AND c.id=?`, s.tenant, app, principal, credential))
		if e != nil {
			return e
		}
		if v.State != "pending" || v.Version != 1 {
			return ErrCredentialState
		}
		// Shorten other active keys only when the new key is really activated. Keep
		// their security version, so cached JWTs stay valid during the overlap.
		cutoff := time.Now().Add(24 * time.Hour).Unix()
		rows, e := tx.QueryContext(ctx, `SELECT id,version FROM machine_credentials WHERE tenant_id=? AND application_id=? AND service_principal_id=? AND id<>? AND state='active' AND expires_at>?`, s.tenant, app, principal, credential, cutoff)
		if e != nil {
			return e
		}
		type older struct {
			id      ID
			version int64
		}
		olderKeys := []older{}
		for rows.Next() {
			var key older
			if e = rows.Scan(&key.id, &key.version); e != nil {
				rows.Close()
				return e
			}
			olderKeys = append(olderKeys, key)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, key := range olderKeys {
			if _, e = tx.ExecContext(ctx, `UPDATE machine_credentials SET expires_at=?,version=version+1 WHERE tenant_id=? AND id=?`, cutoff, s.tenant, key.id); e != nil {
				return e
			}
			if e = record(ctx, tx, s.tenant, s.actor, "credential.overlap_shortened", key.id, key.version+1); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE machine_credentials SET state='active',version=version+1,security_version=security_version+1 WHERE tenant_id=? AND id=?`, s.tenant, credential); e != nil {
			return e
		}
		v.State = "active"
		v.Active = true
		v.Version++
		v.securityVersion++
		return record(ctx, tx, s.tenant, s.actor, "credential.activated", credential, v.Version)
	})
	return
}

type MachineCredentialCreated struct {
	Credential MachineCredentialSummary `json:"credential"`
	Secret     string                   `json:"secret_key"`
}

func (s TenantStore) CreateMachineCredential(ctx context.Context, app, principal ID, name string, days int, admin *hydra.Client) (result MachineCredentialCreated, err error) {
	if admin == nil {
		return result, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	// No recoverable secret is written to either a business row or an event.
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return result, err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	pending, err := s.beginMachineCredential(ctx, app, principal, name, days)
	if err != nil {
		return result, err
	}
	profile := hydra.MachineClientProfile(pending.AccessKey, pending.Name, s.tenant.String(), app.String(), principal.String(), pending.ID, pending.Audience)
	if err = admin.CreateMachineClient(ctx, profile, secret); err != nil {
		return result, err
	}
	id, _ := ParseID(pending.ID)
	value, err := s.activateMachineCredential(ctx, app, principal, id, pending)
	if err != nil {
		return result, err
	}
	return MachineCredentialCreated{Credential: value, Secret: secret}, nil
}
func (s TenantStore) RevokeMachineCredential(ctx context.Context, app, principal, credential ID, expected int64) (v MachineCredentialSummary, err error) {
	if expected < 1 {
		return v, ErrInvalid
	}
	s = s.withRecentMFA()
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if _, e := s.principal(ctx, tx, app, principal, false); e != nil {
			return e
		}
		var e error
		v, e = scanMachineCredential(tx.QueryRowContext(ctx, credentialSelect+` AND c.id=?`, s.tenant, app, principal, credential))
		if e != nil {
			return e
		}
		if v.Version != expected {
			return ErrConflict
		}
		if v.State == "revoked" {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE machine_credentials SET state='revoked',version=version+1,security_version=security_version+1 WHERE tenant_id=? AND id=?`, s.tenant, credential); e != nil {
			return e
		}
		v.State = "revoked"
		v.Active = false
		v.Version++
		v.securityVersion++
		return record(ctx, tx, s.tenant, s.actor, "credential.revoked", credential, v.Version)
	})
	return
}
func (s TenantStore) CleanupMachineCredential(ctx context.Context, app, principal, credential ID, expected int64, admin *hydra.Client) (v MachineCredentialSummary, err error) {
	if expected < 1 || admin == nil {
		return v, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	s = s.withRecentMFA()
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if _, e := s.principal(ctx, tx, app, principal, false); e != nil {
			return e
		}
		var e error
		v, e = scanMachineCredential(tx.QueryRowContext(ctx, credentialSelect+` AND c.id=?`, s.tenant, app, principal, credential))
		if e != nil {
			return e
		}
		if v.Version != expected {
			return ErrConflict
		}
		if v.State != "revoked" {
			return ErrCredentialState
		}
		profile := hydra.MachineClientProfile(v.AccessKey, v.Name, s.tenant.String(), app.String(), principal.String(), v.ID, v.Audience)
		if e = admin.RemovePublicClient(ctx, profile); e != nil {
			return e
		}
		return record(ctx, tx, s.tenant, s.actor, "credential.protocol_cleanup_checked", credential, v.Version)
	})
	return
}
