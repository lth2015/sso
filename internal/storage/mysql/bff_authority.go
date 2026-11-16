package mysql

import (
	"context"
	"database/sql"
	"time"
)

// APIIssueBFFRevocation checks the current authority in the same transaction as issuance.
func (s Store) APIIssueBFFRevocation(ctx context.Context, a Authorization, digest []byte) (int64, error) {
	var expiry int64
	if len(digest) != 32 {
		return 0, ErrInvalid
	}
	err := s.APITransaction(ctx, a, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT absolute_expires_at FROM business_grants WHERE id=? AND client_id=?`, a.Grant, a.Client).Scan(&expiry); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO bff_revocation_credentials(token_digest,grant_id,client_id,expires_at) VALUES(?,?,?,?)`, digest, a.Grant, a.Client, expiry)
		return err
	})
	return expiry, err
}

// APIRevokeBFFGrant never accepts identifiers from the caller. A hashed capability
// selects one client-bound grant and can only deactivate it, with atomic audit/outbox.
func (s Store) APIRevokeBFFGrant(ctx context.Context, digest []byte, client, reason string) error {
	if len(digest) != 32 || (reason != "authorization.logout" && reason != "authorization.refresh_failed" && reason != "authorization.session_expired") {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var grant, tenant, member ID
	var expiry int64
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT g.id,g.tenant_id,g.membership_id,c.expires_at,g.active FROM bff_revocation_credentials c JOIN business_grants g ON g.id=c.grant_id AND g.client_id=c.client_id WHERE c.token_digest=? AND c.client_id=? FOR UPDATE`, digest, client).Scan(&grant, &tenant, &member, &expiry, &active)
	if err == sql.ErrNoRows {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if expiry <= time.Now().Unix() {
		return ErrBFFRevocationExpired
	}
	if !active {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE business_grants SET active=FALSE WHERE id=? AND client_id=?`, grant, client); err != nil {
		return err
	}
	if err = record(ctx, tx, tenant, member, reason, grant, 2); err != nil {
		return err
	}
	return tx.Commit()
}

func (s Store) BFFAuthorityReady(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT token_digest FROM bff_revocation_credentials LIMIT 0`)
	if err != nil {
		return err
	}
	return rows.Close()
}
