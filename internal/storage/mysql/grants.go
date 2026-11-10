package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"strings"
	"time"
)

type TokenCheck struct {
	GrantID                                                      ID
	Client, Subject, HydraSID, Binding, Grant                    string
	Requested, Granted, Audience, PayloadScopes, PayloadAudience []string
}
type GrantClaims struct {
	Grant, Tenant, Member, User, Session      ID
	UserVersion, MemberVersion, TenantVersion uint64
	AMR                                       string
	HydraSID                                  string
}

// CheckBusinessToken runs against the business primary for every code/refresh issuance.
func (s Store) CheckBusinessToken(ctx context.Context, input TokenCheck) (GrantClaims, error) {
	var claims GrantClaims
	if input.Grant != "authorization_code" && input.Grant != "refresh_token" {
		return claims, ErrDenied
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return claims, err
	}
	defer tx.Rollback()
	var binding []byte
	var scopes, audience, subject, sid string
	var valid bool
	now := time.Now().Unix()
	err = tx.QueryRowContext(ctx, `SELECT g.id,g.tenant_id,g.membership_id,g.user_id,g.session_id,g.user_version,g.membership_version,g.tenant_version,s.amr,g.hydra_sid,g.protocol_binding,g.scopes,g.audience,LOWER(HEX(g.user_id)),g.hydra_sid,
 (i.state='complete' AND g.active AND s.active AND u.status='active' AND m.status='active' AND t.status='active' AND f.active AND c.active AND a.active AND ta.active AND g.user_version=u.security_version AND s.user_version=u.security_version AND s.mfa_version=f.security_version AND g.membership_version=m.security_version AND g.tenant_version=t.authz_version AND g.client_version=c.security_version AND g.application_version=a.security_version AND g.enabled_version=ta.security_version AND m.user_id=g.user_id AND s.user_id=g.user_id AND s.auth_time>0 AND s.auth_time<=? AND s.idle_expires_at>? AND s.absolute_expires_at>? AND g.absolute_expires_at>?)
 FROM business_grants g JOIN login_interactions i ON i.id=g.interaction_id JOIN sso_sessions s ON s.id=g.session_id JOIN users u ON u.id=g.user_id JOIN mfa_factors f ON f.user_id=u.id JOIN memberships m ON m.tenant_id=g.tenant_id AND m.id=g.membership_id JOIN tenants t ON t.id=g.tenant_id JOIN oauth_clients c ON c.client_id=g.client_id JOIN applications a ON a.id=c.application_id JOIN tenant_applications ta ON ta.tenant_id=t.id AND ta.application_id=a.id
 WHERE g.id=? AND g.client_id=? FOR UPDATE OF g FOR SHARE OF i,s,u,f,m,t,c,a,ta`, now, now, now, now, input.GrantID, input.Client).Scan(&claims.Grant, &claims.Tenant, &claims.Member, &claims.User, &claims.Session, &claims.UserVersion, &claims.MemberVersion, &claims.TenantVersion, &claims.AMR, &claims.HydraSID, &binding, &scopes, &audience, &subject, &sid, &valid)
	if err == sql.ErrNoRows {
		return claims, ErrDenied
	}
	if err != nil {
		return claims, err
	}
	if !valid || subject != input.Subject || sid == "" || sid != input.HydraSID || input.Binding == "" {
		return claims, ErrDenied
	}
	if len(input.Requested) > 0 && !scopesWithin(input.Requested, scopes) {
		return claims, ErrDenied
	}
	for _, value := range input.PayloadScopes {
		if !scopesWithin(strings.Fields(value), scopes) {
			return claims, ErrDenied
		}
	}
	for _, value := range input.PayloadAudience {
		if value != audience {
			return claims, ErrDenied
		}
	}
	granted, aud := input.Granted, input.Audience
	if input.Grant == "authorization_code" && len(granted) == 0 && len(aud) == 0 {
		granted = strings.Fields(scopes)
		aud = []string{audience}
	}
	if !scopesWithin(granted, scopes) || len(aud) != 1 || aud[0] != audience {
		return claims, ErrDenied
	}
	digest := sha256.Sum256([]byte(input.Binding))
	if binding == nil {
		if input.Grant != "authorization_code" {
			return claims, ErrDenied
		}
		if _, err = tx.ExecContext(ctx, `UPDATE business_grants SET protocol_binding=? WHERE id=? AND protocol_binding IS NULL`, digest[:], input.GrantID); err != nil {
			return claims, err
		}
	} else if !bytes.Equal(binding, digest[:]) {
		return claims, ErrDenied
	}
	if err = record(ctx, tx, claims.Tenant, claims.Member, "token."+input.Grant, claims.Grant, 1); err != nil {
		return claims, err
	}
	return claims, tx.Commit()
}

// GrantCurrent is the authoritative check used by the demo resource/BFF. It does
// not treat a valid JWT signature as live authorization.
func (s Store) GrantCurrent(ctx context.Context, id, user, tenant ID, client string) error {
	now := time.Now().Unix()
	var valid int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM business_grants g JOIN login_interactions i ON i.id=g.interaction_id JOIN sso_sessions s ON s.id=g.session_id JOIN users u ON u.id=g.user_id JOIN mfa_factors f ON f.user_id=u.id JOIN memberships m ON m.tenant_id=g.tenant_id AND m.id=g.membership_id JOIN tenants t ON t.id=g.tenant_id JOIN oauth_clients c ON c.client_id=g.client_id JOIN applications a ON a.id=c.application_id JOIN tenant_applications ta ON ta.tenant_id=t.id AND ta.application_id=a.id WHERE g.id=? AND g.user_id=? AND g.tenant_id=? AND g.client_id=? AND g.protocol_binding IS NOT NULL AND i.state='complete' AND g.active AND s.active AND u.status='active' AND m.status='active' AND t.status='active' AND f.active AND c.active AND a.active AND ta.active AND g.user_version=u.security_version AND s.user_version=u.security_version AND s.mfa_version=f.security_version AND g.membership_version=m.security_version AND g.tenant_version=t.authz_version AND g.client_version=c.security_version AND g.application_version=a.security_version AND g.enabled_version=ta.security_version AND m.user_id=g.user_id AND s.user_id=g.user_id AND s.idle_expires_at>? AND s.absolute_expires_at>? AND g.absolute_expires_at>?`, id, user, tenant, client, now, now, now).Scan(&valid)
	if err != nil {
		return err
	}
	if valid != 1 {
		return ErrDenied
	}
	return nil
}
