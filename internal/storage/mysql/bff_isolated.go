package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// IsolatedBFFStore deliberately executes SQL only against three BFF-owned tables.
// User/tenant/grant identifiers are opaque references, never SQL foreign keys.
type IsolatedBFFStore struct{ store Store }

func NewIsolatedBFFStore(s Store) IsolatedBFFStore { return IsolatedBFFStore{s} }
func (s IsolatedBFFStore) CreateBFFTransaction(ctx context.Context, state, browser []byte, client string, payload []byte) error {
	return s.store.CreateBFFTransaction(ctx, state, browser, client, payload)
}
func (s IsolatedBFFStore) ConsumeBFFTransaction(ctx context.Context, state, browser []byte, client string) ([]byte, error) {
	return s.store.ConsumeBFFTransaction(ctx, state, browser, client)
}
func (s IsolatedBFFStore) CreateBFFSession(ctx context.Context, token []byte, client string, user, grant ID, cipher []byte) error {
	return s.store.CreateBFFSession(ctx, token, client, user, grant, cipher)
}
func (s IsolatedBFFStore) BFFSession(ctx context.Context, token []byte, client string) (BFFSession, error) {
	return s.store.BFFSession(ctx, token, client)
}
func (s IsolatedBFFStore) BFFStorageReady(ctx context.Context, client string) error {
	for _, q := range []string{`SELECT state_digest FROM bff_transactions LIMIT 0`, `SELECT token_digest FROM bff_sessions LIMIT 0`, `SELECT token_digest FROM bff_revocation_outbox LIMIT 0`} {
		rows, err := s.store.db.QueryContext(ctx, q)
		if err != nil {
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	return nil
}
func (s Store) BFFStorageReady(ctx context.Context, client string) error {
	return s.UserGrantStorageReady(ctx, client)
}
func queueBFFRevocation(ctx context.Context, tx *sql.Tx, token []byte, client string, payload []byte, reason string) error {
	if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO bff_revocation_outbox(token_digest,client_id,encrypted_tokens,reason) VALUES(?,?,?,?)`, token, client, payload, reason); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE bff_sessions SET active=FALSE,encrypted_tokens='' WHERE token_digest=? AND client_id=?`, token, client)
	return err
}
func (s IsolatedBFFStore) RefreshBFF(ctx context.Context, token []byte, client string, refresh func(BFFSession) ([]byte, error)) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var b BFFSession
	now := time.Now().Unix()
	err = tx.QueryRowContext(ctx, `SELECT user_id,grant_id,encrypted_tokens,idle_expires_at,absolute_expires_at FROM bff_sessions WHERE token_digest=? AND client_id=? AND active=TRUE AND idle_expires_at>? AND absolute_expires_at>? FOR UPDATE`, token, client, now, now).Scan(&b.User, &b.Grant, &b.Tokens, &b.Idle, &b.Absolute)
	if err == sql.ErrNoRows {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	payload, refreshErr := refresh(b)
	if refreshErr != nil {
		if err = queueBFFRevocation(ctx, tx, token, client, b.Tokens, "authorization.refresh_failed"); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return refreshErr
	}
	if _, err = tx.ExecContext(ctx, `UPDATE bff_sessions SET encrypted_tokens=?,idle_expires_at=LEAST(absolute_expires_at,?) WHERE token_digest=? AND client_id=?`, payload, now+1800, token, client); err != nil {
		return err
	}
	return tx.Commit()
}
func (s IsolatedBFFStore) RevokeBFF(ctx context.Context, token []byte, client string) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var payload []byte
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT encrypted_tokens,active FROM bff_sessions WHERE token_digest=? AND client_id=? FOR UPDATE`, token, client).Scan(&payload, &active)
	if err == sql.ErrNoRows {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if active {
		if err = queueBFFRevocation(ctx, tx, token, client, payload, "authorization.logout"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

var ErrBFFRevocationExpired = errors.New("revocation_credential_expired")

// DeliverOneRevocation serializes workers and retains encrypted credentials on failure.
func (s IsolatedBFFStore) DeliverOneRevocation(ctx context.Context, client string, send func([]byte, []byte, string) error) (bool, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var token, payload []byte
	var reason string
	var attempts uint
	err = tx.QueryRowContext(ctx, `SELECT token_digest,encrypted_tokens,reason,attempts FROM bff_revocation_outbox WHERE client_id=? AND state='pending' AND next_attempt_at<=? ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED`, client, time.Now().Unix()).Scan(&token, &payload, &reason, &attempts)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sendErr := send(token, payload, reason)
	if sendErr == nil || errors.Is(sendErr, ErrBFFRevocationExpired) {
		_, err = tx.ExecContext(ctx, `UPDATE bff_revocation_outbox SET state='done',encrypted_tokens='' WHERE token_digest=? AND client_id=?`, token, client)
	} else {
		shift := min(attempts, 7)
		delay := min(2<<shift, 300)
		_, err = tx.ExecContext(ctx, `UPDATE bff_revocation_outbox SET attempts=attempts+1,next_attempt_at=? WHERE token_digest=? AND client_id=?`, time.Now().Unix()+int64(delay), token, client)
	}
	if err != nil {
		return true, err
	}
	if err = tx.Commit(); err != nil {
		return true, err
	}
	return true, sendErr
}

// ExpireOneBFFSession schedules central cleanup when a local idle/absolute deadline passes.
func (s IsolatedBFFStore) ExpireOneBFFSession(ctx context.Context, client string) (bool, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var token, payload []byte
	now := time.Now().Unix()
	err = tx.QueryRowContext(ctx, `SELECT token_digest,encrypted_tokens FROM bff_sessions WHERE client_id=? AND active=TRUE AND (idle_expires_at<=? OR absolute_expires_at<=?) ORDER BY absolute_expires_at LIMIT 1 FOR UPDATE SKIP LOCKED`, client, now, now).Scan(&token, &payload)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = queueBFFRevocation(ctx, tx, token, client, payload, "authorization.session_expired"); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
