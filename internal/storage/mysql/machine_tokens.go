package mysql

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type MachineTokenCheck struct {
	Client                                                       string
	Requested, Granted, Audience, PayloadScopes, PayloadAudience []string
}
type MachineAuthorization struct {
	Tenant, Application, Principal, Credential                                             ID
	Client                                                                                 string
	TenantVersion, ApplicationVersion, EnabledVersion, PrincipalVersion, CredentialVersion uint64
	Expires                                                                                int64
	CredentialExpires                                                                      int64
}

func (s Store) CheckMachineToken(ctx context.Context, input MachineTokenCheck) (claims MachineAuthorization, err error) {
	if len(input.Client) != 35 || !strings.HasPrefix(input.Client, "ak_") {
		return claims, ErrDenied
	}
	if _, e := ParseID(strings.TrimPrefix(input.Client, "ak_")); e != nil {
		return claims, ErrDenied
	}
	if len(input.Granted) != 1 || input.Granted[0] != MachineScope || len(input.Audience) != 1 {
		return claims, ErrDenied
	}
	if len(input.Requested) > 0 && !scopesWithin(input.Requested, MachineScope) {
		return claims, ErrDenied
	}
	for _, value := range input.PayloadScopes {
		if !scopesWithin(strings.Fields(value), MachineScope) {
			return claims, ErrDenied
		}
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return claims, e
	}
	defer tx.Rollback()
	var valid bool
	now := time.Now().Unix()
	e = tx.QueryRowContext(ctx, `SELECT c.tenant_id,c.application_id,c.service_principal_id,c.id,t.authz_version,a.security_version,ta.security_version,sp.security_version,c.security_version,c.expires_at,
 (c.state='active' AND c.expires_at>? AND sp.enabled AND t.status='active' AND a.active AND ta.active)
 FROM machine_credentials c JOIN service_principals sp ON sp.tenant_id=c.tenant_id AND sp.application_id=c.application_id AND sp.id=c.service_principal_id
 JOIN tenants t ON t.id=c.tenant_id JOIN applications a ON a.id=c.application_id JOIN tenant_applications ta ON ta.tenant_id=c.tenant_id AND ta.application_id=c.application_id
 JOIN managed_applications own ON own.tenant_id=c.tenant_id AND own.application_id=c.application_id
 WHERE c.client_id=? FOR SHARE`, now, input.Client).Scan(&claims.Tenant, &claims.Application, &claims.Principal, &claims.Credential, &claims.TenantVersion, &claims.ApplicationVersion, &claims.EnabledVersion, &claims.PrincipalVersion, &claims.CredentialVersion, &claims.CredentialExpires, &valid)
	if e == sql.ErrNoRows {
		return claims, ErrDenied
	}
	if e != nil {
		return claims, e
	}
	if !valid {
		return claims, ErrDenied
	}
	expected := MachineAudience(claims.Application)
	if input.Audience[0] != expected {
		return claims, ErrDenied
	}
	for _, value := range input.PayloadAudience {
		if value != expected {
			return claims, ErrDenied
		}
	}
	claims.Client = input.Client
	event, e := NewID()
	if e != nil {
		return claims, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO machine_events(event_id,tenant_id,application_id,service_principal_id,credential_id,action) VALUES(?,?,?,?,?,'token.client_credentials')`, event, claims.Tenant, claims.Application, claims.Principal, claims.Credential); e != nil {
		return claims, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO machine_outbox_events(event_id,tenant_id,aggregate_id,aggregate_version,type) VALUES(?,?,?,?,'token.client_credentials')`, event, claims.Tenant, claims.Credential, claims.CredentialVersion); e != nil {
		return claims, e
	}
	if e = tx.Commit(); e != nil {
		return claims, e
	}
	return claims, nil
}

// CheckMachineResource consumes only cryptographically verified machine claims.
// It reads primary authority rather than accepting a valid signature as proof
// that an unexpired key or principal is still enabled.
func (s Store) CheckMachineResource(ctx context.Context, a MachineAuthorization) error {
	now := time.Now().Unix()
	if a.Expires <= now || a.CredentialExpires <= now || a.Client != "ak_"+a.Credential.String() {
		return ErrDenied
	}
	var n int
	e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM machine_credentials c
 JOIN service_principals sp ON sp.tenant_id=c.tenant_id AND sp.application_id=c.application_id AND sp.id=c.service_principal_id
 JOIN tenants t ON t.id=c.tenant_id JOIN applications app ON app.id=c.application_id
 JOIN tenant_applications ta ON ta.tenant_id=c.tenant_id AND ta.application_id=c.application_id
 JOIN managed_applications own ON own.tenant_id=c.tenant_id AND own.application_id=c.application_id
 WHERE c.tenant_id=? AND c.application_id=? AND c.service_principal_id=? AND c.id=? AND c.client_id=?
 AND c.state='active' AND c.expires_at>? AND sp.enabled AND t.status='active' AND app.active AND ta.active
 AND c.security_version=? AND sp.security_version=? AND t.authz_version=? AND app.security_version=? AND ta.security_version=?`, a.Tenant, a.Application, a.Principal, a.Credential, a.Client, now, a.CredentialVersion, a.PrincipalVersion, a.TenantVersion, a.ApplicationVersion, a.EnabledVersion).Scan(&n)
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrDenied
	}
	return nil
}

type MachineEventSummary struct {
	ID          string    `json:"id"`
	Application string    `json:"application_id"`
	Principal   string    `json:"service_principal_id"`
	Credential  string    `json:"credential_id"`
	Action      string    `json:"action"`
	At          time.Time `json:"occurred_at"`
}

func (s TenantStore) APIMachineEvents(ctx context.Context, limit int, after ID) (items []MachineEventSummary, err error) {
	items = []MachineEventSummary{}
	if limit < 1 || limit > 101 {
		return items, ErrInvalid
	}
	err = s.publicRead(ctx, "audit.read", func(tx *sql.Tx, _, _ int64) error {
		rows, e := tx.QueryContext(ctx, `SELECT LOWER(HEX(event_id)),LOWER(HEX(application_id)),LOWER(HEX(service_principal_id)),LOWER(HEX(credential_id)),action,occurred_at FROM machine_events WHERE tenant_id=? AND event_id>? ORDER BY event_id LIMIT ?`, s.tenant, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v MachineEventSummary
			if e = rows.Scan(&v.ID, &v.Application, &v.Principal, &v.Credential, &v.Action, &v.At); e != nil {
				return e
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	return
}
