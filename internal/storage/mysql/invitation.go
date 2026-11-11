package mysql

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/mail"
	"strings"
	"time"
)

// NormalizeEmail defines this slice's alias policy. It does not infer tenant
// membership from domains or perform provider-specific dot/plus normalization.
func NormalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || len(normalized) > 254 {
		return "", ErrInvalid
	}
	for _, r := range normalized {
		if r > 127 {
			return "", ErrInvalid
		}
	}
	return normalized, nil
}

// CreateInvitation returns a one-time bearer secret to the trusted caller;
// neither audit nor outbox stores that secret. Email delivery is not implemented.
func (s TenantStore) CreateInvitation(ctx context.Context, email string) (ID, string, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return ID{}, "", err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return ID{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	digest := sha256.Sum256([]byte(token))
	id, err := NewID()
	if err != nil {
		return ID{}, "", err
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ID{}, "", err
	}
	defer tx.Rollback()
	if _, _, err = s.lockPermission(ctx, tx, "invitations.manage"); err != nil {
		return ID{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO invitations(id,tenant_id,creator_membership_id,target_email,token_digest,expires_at) VALUES(?,?,?,?,?,?)`, id, s.tenant, s.actor, email, digest[:], time.Now().Add(24*time.Hour).Unix()); err != nil {
		return ID{}, "", err
	}
	if err = record(ctx, tx, s.tenant, s.actor, "member.invited", id, 1); err != nil {
		return ID{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return ID{}, "", err
	}
	return id, token, nil
}

// AcceptInvitation requires an existing verified login alias and active User.
// The opaque token selects its tenant from the DB; callers cannot override it.
func (s Store) AcceptInvitation(ctx context.Context, user ID, token string) (ID, error) {
	return s.acceptInvitation(ctx, user, token, nil)
}
func (s Store) APIAcceptInvitation(ctx context.Context, a Authorization, token string) (ID, error) {
	return s.acceptInvitation(ctx, a.User, token, &a)
}
func (s Store) acceptInvitation(ctx context.Context, user ID, token string, a *Authorization) (ID, error) {
	if user.empty() || len(token) != 43 {
		return ID{}, ErrInvalid
	}
	if raw, err := base64.RawURLEncoding.DecodeString(token); err != nil || len(raw) != 32 {
		return ID{}, ErrInvalid
	}
	digest := sha256.Sum256([]byte(token))
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ID{}, err
	}
	defer tx.Rollback()
	if a != nil {
		if err = lockAuthorization(ctx, tx, *a); err != nil {
			return ID{}, err
		}
	}
	var tenant, creator ID
	if err = tx.QueryRowContext(ctx, `SELECT tenant_id,creator_membership_id FROM invitations WHERE token_digest=?`, digest[:]).Scan(&tenant, &creator); err == sql.ErrNoRows {
		return ID{}, ErrInvalid
	}
	if err != nil {
		return ID{}, err
	}
	repo := s.ForTenant(tenant, creator)
	version, _, err := repo.lockPermission(ctx, tx, "invitations.manage")
	if err != nil {
		return ID{}, err
	}
	var invite ID
	var email string
	var expiry int64
	var consumed sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT id,target_email,expires_at,consumed_at FROM invitations WHERE tenant_id=? AND token_digest=? FOR UPDATE`, tenant, digest[:]).Scan(&invite, &email, &expiry, &consumed); err != nil {
		return ID{}, err
	}
	if consumed.Valid || expiry <= time.Now().Unix() {
		return ID{}, ErrInvalid
	}
	var matches int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u JOIN login_aliases a ON a.user_id=u.id WHERE u.id=? AND u.status='active' AND a.normalized_email=? AND a.verified_at IS NOT NULL`, user, email).Scan(&matches)
	if err != nil {
		return ID{}, err
	}
	if matches != 1 {
		return ID{}, ErrDenied
	}
	var member ID
	err = tx.QueryRowContext(ctx, `SELECT id FROM memberships WHERE tenant_id=? AND user_id=?`, tenant, user).Scan(&member)
	if err == nil {
		return ID{}, ErrConflict
	}
	if err != sql.ErrNoRows {
		return ID{}, err
	}
	member, err = NewID()
	if err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memberships(id,tenant_id,user_id,status) VALUES(?,?,?,'active')`, member, tenant, user); err != nil {
		return ID{}, err
	}
	// Invitation acceptance grants membership only, never owner/admin privilege.
	if _, err = tx.ExecContext(ctx, `UPDATE invitations SET consumed_at=UTC_TIMESTAMP(6) WHERE id=? AND consumed_at IS NULL`, invite); err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenants SET authz_version=authz_version+1 WHERE id=?`, tenant); err != nil {
		return ID{}, err
	}
	if err = record(ctx, tx, tenant, member, "member.joined", member, version+1); err != nil {
		return ID{}, err
	}
	return member, tx.Commit()
}

// Organization membership is an association, not a role grant.
func (s TenantStore) AssignOrganization(ctx context.Context, member, org ID, expectedVersion int64) error {
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	version, _, err := s.lockPermission(ctx, tx, "organizations.write")
	if err != nil {
		return err
	}
	if version != expectedVersion {
		return ErrConflict
	}
	var count int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.id=? AND m.status='active' AND u.status='active'`, s.tenant, member).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM org_nodes WHERE tenant_id=? AND id=?`, s.tenant, org).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO org_memberships VALUES(?,?,?)`, s.tenant, org, member); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenants SET authz_version=authz_version+1 WHERE id=?`, s.tenant); err != nil {
		return err
	}
	if err = record(ctx, tx, s.tenant, s.actor, "org.member_assigned", member, version+1); err != nil {
		return err
	}
	return tx.Commit()
}
