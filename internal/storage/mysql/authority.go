package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Authorization is created only from a verified platform access token. Its
// identifiers are rechecked against the primary within each API transaction.
type Authorization struct {
	Grant, User, Tenant, Member, Session ID
	Client                               string
	Recent                               bool
	Expires                              int64
}

var ErrStepUp = errors.New("recent_mfa_required")

func (s Store) ForAuthorization(a Authorization) TenantStore {
	return TenantStore{store: s, tenant: a.Tenant, actor: a.Member, authority: &a}
}

func lockAuthorization(ctx context.Context, tx *sql.Tx, a Authorization) error {
	now := time.Now().Unix()
	if a.Expires > 0 && now >= a.Expires {
		return ErrDenied
	}
	var id ID
	var authTime int64
	err := tx.QueryRowContext(ctx, `SELECT g.id,s.auth_time FROM business_grants g
 JOIN login_interactions i ON i.id=g.interaction_id JOIN sso_sessions s ON s.id=g.session_id
 JOIN users u ON u.id=g.user_id JOIN mfa_factors f ON f.user_id=u.id
 JOIN memberships m ON m.tenant_id=g.tenant_id AND m.id=g.membership_id
 JOIN tenants t ON t.id=g.tenant_id JOIN oauth_clients c ON c.client_id=g.client_id
 JOIN applications app ON app.id=c.application_id JOIN tenant_applications ta ON ta.tenant_id=t.id AND ta.application_id=app.id
 WHERE g.id=? AND g.user_id=? AND g.tenant_id=? AND g.membership_id=? AND g.session_id=? AND g.client_id=?
 AND g.protocol_binding IS NOT NULL AND i.state='complete' AND g.active AND s.active
 AND u.status='active' AND m.status='active' AND t.status='active' AND f.active AND c.active AND app.active AND ta.active
 AND g.user_version=u.security_version AND s.user_version=u.security_version AND s.mfa_version=f.security_version
 AND g.membership_version=m.security_version AND g.tenant_version=t.authz_version AND g.client_version=c.security_version
 AND g.application_version=app.security_version AND g.enabled_version=ta.security_version
 AND m.user_id=g.user_id AND s.user_id=g.user_id AND s.auth_time>0 AND s.auth_time<=?
 AND s.amr IN ('otp','recovery') AND s.idle_expires_at>? AND s.absolute_expires_at>? AND g.absolute_expires_at>?
 FOR SHARE`, a.Grant, a.User, a.Tenant, a.Member, a.Session, a.Client, now, now, now, now).Scan(&id, &authTime)
	if err == sql.ErrNoRows {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if a.Recent && now-authTime > 300 {
		return ErrStepUp
	}
	return nil
}
func (s Store) APITransaction(ctx context.Context, a Authorization, run func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockAuthorization(ctx, tx, a); err != nil {
		return err
	}
	if err = run(tx); err != nil {
		return err
	}
	return tx.Commit()
}
