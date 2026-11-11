package mysql

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
)

var ErrInvitationClosed = errors.New("invalid_invitation_state")

type InvitationDetail struct {
	InvitationSummary
	Revision int64 `json:"revision"`
}

type InvitationReissued struct {
	ID        string `json:"id"`
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
	Revision  int64  `json:"revision"`
}

func newInvitationProof() (string, [32]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", [32]byte{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	return token, sha256.Sum256([]byte(token)), nil
}

// Call only after locking and checking the parent invitation and tenant.
// Existing invitations without metadata start at revision 1; first mutation is 2.
func bumpInvitationRevision(ctx context.Context, tx *sql.Tx, id ID) (int64, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO invitation_revisions(invitation_id,revision) VALUES(?,2) ON DUPLICATE KEY UPDATE revision=revision+1`, id); err != nil {
		return 0, err
	}
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM invitation_revisions WHERE invitation_id=?`, id).Scan(&revision)
	return revision, err
}

func (s TenantStore) APIInvitation(ctx context.Context, id ID) (result InvitationDetail, err error) {
	err = s.publicRead(ctx, "invitations.manage", func(tx *sql.Tx, _, _ int64) error {
		e := tx.QueryRowContext(ctx, `SELECT LOWER(HEX(i.id)),i.target_email,i.expires_at,IF(i.consumed_at IS NOT NULL,'closed',IF(i.expires_at<=?,'expired','pending')),COALESCE(v.revision,1) FROM invitations i LEFT JOIN invitation_revisions v ON v.invitation_id=i.id WHERE i.tenant_id=? AND i.id=?`, time.Now().Unix(), s.tenant, id).Scan(&result.ID, &result.Email, &result.Expires, &result.State, &result.Revision)
		if e == sql.ErrNoRows {
			return ErrNotFound
		}
		return e
	})
	return
}

// Reissue replaces only the proof, expiry and responsible current manager.
// The invitation/email/tenant stay fixed. Closed invitations never reopen.
// No recoverable proof is stored, including when commit outcome is unknown.
func (s TenantStore) APIReissueInvitation(ctx context.Context, id ID, expected int64) (InvitationReissued, error) {
	if id.empty() || expected < 1 {
		return InvitationReissued{}, ErrInvalid
	}
	s = s.withRecentMFA()
	token, digest, err := newInvitationProof()
	if err != nil {
		return InvitationReissued{}, err
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return InvitationReissued{}, err
	}
	defer tx.Rollback()
	if _, _, err = s.lockPermission(ctx, tx, "invitations.manage"); err != nil {
		return InvitationReissued{}, err
	}
	var consumed sql.NullTime
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT i.consumed_at,COALESCE(v.revision,1) FROM invitations i LEFT JOIN invitation_revisions v ON v.invitation_id=i.id WHERE i.tenant_id=? AND i.id=? FOR UPDATE`, s.tenant, id).Scan(&consumed, &revision)
	if err == sql.ErrNoRows {
		return InvitationReissued{}, ErrNotFound
	}
	if err != nil {
		return InvitationReissued{}, err
	}
	if revision != expected {
		return InvitationReissued{}, ErrConflict
	}
	if consumed.Valid {
		return InvitationReissued{}, ErrInvitationClosed
	}
	if _, err = tx.ExecContext(ctx, `UPDATE invitations SET creator_membership_id=?,token_digest=?,expires_at=? WHERE tenant_id=? AND id=?`, s.actor, digest[:], time.Now().Add(24*time.Hour).Unix(), s.tenant, id); err != nil {
		return InvitationReissued{}, err
	}
	revision, err = bumpInvitationRevision(ctx, tx, id)
	if err != nil {
		return InvitationReissued{}, err
	}
	if err = record(ctx, tx, s.tenant, s.actor, "invitation.reissued", id, revision); err != nil {
		return InvitationReissued{}, err
	}
	if err = tx.Commit(); err != nil {
		return InvitationReissued{}, err
	}
	return InvitationReissued{ID: id.String(), Token: token, ExpiresIn: 86400, Revision: revision}, nil
}
