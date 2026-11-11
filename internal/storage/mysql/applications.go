package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	driver "github.com/go-sql-driver/mysql"
)

var ErrProtectedApplication = errors.New("protected_application")
var ErrApplicationUnavailable = errors.New("application_unavailable")

type ApplicationSummary struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Available   bool   `json:"available"`
	Protected   bool   `json:"protected"`
	Version     int64  `json:"version"`
	ClientCount int    `json:"client_count"`
	Managed     bool   `json:"managed"`
}
type ApplicationClientSummary struct {
	Client   string   `json:"client_id"`
	Redirect string   `json:"redirect_uri"`
	Scopes   []string `json:"scopes"`
	Audience string   `json:"audience"`
	Active   bool     `json:"active"`
	Version  int64    `json:"version"`
	Managed  bool     `json:"managed"`
	State    string   `json:"state"`
}

const applicationSelect = `SELECT LOWER(HEX(a.id)),a.code,a.name,ta.active,a.active,a.code='local-platform',ta.security_version,(SELECT COUNT(*) FROM oauth_clients c WHERE c.application_id=a.id) ,EXISTS(SELECT 1 FROM managed_applications ma WHERE ma.tenant_id=ta.tenant_id AND ma.application_id=a.id) FROM tenant_applications ta JOIN applications a ON a.id=ta.application_id WHERE ta.tenant_id=?`

func scanApplication(row interface{ Scan(...any) error }) (v ApplicationSummary, err error) {
	err = row.Scan(&v.ID, &v.Code, &v.Name, &v.Enabled, &v.Available, &v.Protected, &v.Version, &v.ClientCount, &v.Managed)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	return
}
func (s TenantStore) APIApplications(ctx context.Context, limit int, after ID) (items []ApplicationSummary, err error) {
	items = []ApplicationSummary{}
	if limit < 1 || limit > 101 {
		return items, ErrInvalid
	}
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		rows, e := tx.QueryContext(ctx, applicationSelect+` AND a.id>? ORDER BY a.id LIMIT ?`, s.tenant, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanApplication(rows)
			if e != nil {
				return e
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	return
}
func (s TenantStore) APIApplication(ctx context.Context, app ID) (value ApplicationSummary, err error) {
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		var e error
		value, e = scanApplication(tx.QueryRowContext(ctx, applicationSelect+` AND a.id=?`, s.tenant, app))
		return e
	})
	return
}
func (s TenantStore) APIApplicationClients(ctx context.Context, app ID, limit int, after string) (items []ApplicationClientSummary, err error) {
	items = []ApplicationClientSummary{}
	if limit < 1 || limit > 101 || len(after) > 128 {
		return items, ErrInvalid
	}
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if _, e := scanApplication(tx.QueryRowContext(ctx, applicationSelect+` AND a.id=?`, s.tenant, app)); e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, `SELECT c.client_id,c.redirect_uri,c.scopes,c.audience,c.active,c.security_version,mc.client_id IS NOT NULL,COALESCE(mc.state,'external') FROM oauth_clients c LEFT JOIN managed_oauth_clients mc ON mc.client_id=c.client_id AND mc.tenant_id=? AND mc.application_id=c.application_id WHERE c.application_id=? AND c.client_id>? ORDER BY c.client_id LIMIT ?`, s.tenant, app, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v ApplicationClientSummary
			var scopes string
			if e = rows.Scan(&v.Client, &v.Redirect, &scopes, &v.Audience, &v.Active, &v.Version, &v.Managed, &v.State); e != nil {
				return e
			}
			v.Scopes = strings.Fields(scopes)
			items = append(items, v)
		}
		return rows.Err()
	})
	return
}
func (s TenantStore) SetApplicationEnabled(ctx context.Context, app ID, enabled bool, expected int64) (value ApplicationSummary, err error) {
	for attempt := 0; attempt < 2; attempt++ {
		value, err = s.setApplicationEnabledOnce(ctx, app, enabled, expected)
		var failure *driver.MySQLError
		if !errors.As(err, &failure) || failure.Number != 1213 || ctx.Err() != nil {
			return
		}
		// MySQL 1213 confirms the transaction rolled back. Retry only that
		// known outcome, never transport errors or an ambiguous commit.
	}
	return
}
func (s TenantStore) setApplicationEnabledOnce(ctx context.Context, app ID, enabled bool, expected int64) (value ApplicationSummary, err error) {
	if expected < 1 {
		return value, ErrInvalid
	}
	if s.authority != nil {
		copy := *s.authority
		copy.Recent = true
		s.authority = &copy
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	if _, _, err = s.lockOwner(ctx, tx); err != nil {
		return value, err
	}
	var locked ID
	err = tx.QueryRowContext(ctx, `SELECT application_id FROM tenant_applications WHERE tenant_id=? AND application_id=? FOR UPDATE`, s.tenant, app).Scan(&locked)
	if err == sql.ErrNoRows {
		return value, ErrNotFound
	}
	if err != nil {
		return value, err
	}
	value, err = scanApplication(tx.QueryRowContext(ctx, applicationSelect+` AND a.id=?`, s.tenant, app))
	if err != nil {
		return value, err
	}
	if value.Version != expected {
		return value, ErrConflict
	}
	if value.Protected && !enabled {
		return value, ErrProtectedApplication
	}
	if enabled && !value.Available {
		return value, ErrApplicationUnavailable
	}
	if value.Enabled == enabled {
		return value, tx.Commit()
	}
	value.Enabled = enabled
	value.Version++
	if _, err = tx.ExecContext(ctx, `UPDATE tenant_applications SET active=?,security_version=? WHERE tenant_id=? AND application_id=?`, enabled, value.Version, s.tenant, app); err != nil {
		return value, err
	}
	// The association version is the revocation authority. Every resource and
	// token-hook check compares it with the grant's bound enabled_version.
	// Do not mass-lock grant/BFF rows here: token issuance locks grants before
	// tenant checks, and broad updates can deadlock with those readers. Retained
	// encrypted rows expire normally; no old grant can survive or resurrect.

	action := "application.disabled"
	if enabled {
		action = "application.enabled"
	}
	if err = record(ctx, tx, s.tenant, s.actor, action, app, value.Version); err != nil {
		return value, err
	}
	return value, tx.Commit()
}
