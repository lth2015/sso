package mysql

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"time"
)

func (s Store) CreateBFFTransaction(ctx context.Context, state, browser []byte, client string, payload []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO bff_transactions VALUES(?,?,?,?,FALSE,?)`, state, client, browser, payload, time.Now().Unix()+300)
	return err
}
func (s Store) ConsumeBFFTransaction(ctx context.Context, state, browser []byte, client string) ([]byte, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var binding, payload []byte
	var consumed bool
	var expiry int64
	err = tx.QueryRowContext(ctx, `SELECT browser_digest,encrypted_payload,consumed,expires_at FROM bff_transactions WHERE state_digest=? AND client_id=? FOR UPDATE`, state, client).Scan(&binding, &payload, &consumed, &expiry)
	if err == sql.ErrNoRows {
		return nil, ErrDenied
	}
	if err != nil {
		return nil, err
	}
	if consumed || expiry <= time.Now().Unix() || len(browser) != 32 || subtle.ConstantTimeCompare(binding, browser) != 1 {
		return nil, ErrDenied
	}
	if _, err = tx.ExecContext(ctx, `UPDATE bff_transactions SET consumed=TRUE,encrypted_payload='' WHERE state_digest=?`, state); err != nil {
		return nil, err
	}
	return payload, tx.Commit()
}

type BFFSession struct {
	User, Grant    ID
	Tokens         []byte
	Idle, Absolute int64
}

func (s Store) CreateBFFSession(ctx context.Context, token []byte, client string, user, grant ID, cipher []byte) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO bff_sessions VALUES(?,?,?,?,?,TRUE,?,?)`, token, client, user, grant, cipher, now+1800, now+8*3600)
	return err
}
func (s Store) BFFSession(ctx context.Context, token []byte, client string) (BFFSession, error) {
	var b BFFSession
	now := time.Now().Unix()
	err := s.db.QueryRowContext(ctx, `SELECT user_id,grant_id,encrypted_tokens,idle_expires_at,absolute_expires_at FROM bff_sessions WHERE token_digest=? AND client_id=? AND active=TRUE AND idle_expires_at>? AND absolute_expires_at>?`, token, client, now, now).Scan(&b.User, &b.Grant, &b.Tokens, &b.Idle, &b.Absolute)
	if err == sql.ErrNoRows {
		return b, ErrDenied
	}
	if err != nil {
		return b, err
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE bff_sessions SET idle_expires_at=LEAST(absolute_expires_at,?) WHERE token_digest=? AND client_id=? AND active=TRUE`, now+1800, token, client); err != nil {
		return b, err
	}
	return b, nil
}
func (s Store) RefreshBFF(ctx context.Context, token []byte, client string, refresh func(BFFSession) ([]byte, error)) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	var b BFFSession
	err = tx.QueryRowContext(ctx, `SELECT user_id,grant_id,encrypted_tokens,idle_expires_at,absolute_expires_at FROM bff_sessions WHERE token_digest=? AND client_id=? AND active=TRUE AND idle_expires_at>? AND absolute_expires_at>? FOR UPDATE`, token, client, now, now).Scan(&b.User, &b.Grant, &b.Tokens, &b.Idle, &b.Absolute)
	if err == sql.ErrNoRows {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	payload, err := refresh(b)
	if err != nil {
		if _, e := tx.ExecContext(ctx, `UPDATE bff_sessions SET active=FALSE,encrypted_tokens='' WHERE token_digest=?`, token); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE business_grants SET active=FALSE WHERE id=? AND client_id=?`, b.Grant, client); e != nil {
			return e
		}
		event, eventErr := NewID()
		if eventErr != nil {
			return eventErr
		}
		if _, e := tx.ExecContext(ctx, `CALL append_bff_event(?,?,'authorization.refresh_failed')`, event, b.Grant); e != nil {
			return e
		}
		if e := tx.Commit(); e != nil {
			return e
		}
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE bff_sessions SET encrypted_tokens=?,idle_expires_at=LEAST(absolute_expires_at,?) WHERE token_digest=?`, payload, now+1800, token); err != nil {
		return err
	}
	return tx.Commit()
}
func (s Store) RevokeBFF(ctx context.Context, token []byte, client string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var grant, tenant, member ID
	err = tx.QueryRowContext(ctx, `SELECT g.id,g.tenant_id,g.membership_id FROM bff_sessions b JOIN business_grants g ON g.id=b.grant_id WHERE b.token_digest=? AND b.client_id=? FOR UPDATE OF b,g`, token, client).Scan(&grant, &tenant, &member)
	if err == sql.ErrNoRows {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE bff_sessions SET active=FALSE,encrypted_tokens='' WHERE token_digest=?`, token); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE business_grants SET active=FALSE WHERE id=?`, grant); err != nil {
		return err
	}
	event, eventErr := NewID()
	if eventErr != nil {
		return eventErr
	}
	if _, err = tx.ExecContext(ctx, `CALL append_bff_event(?,?,'authorization.logout')`, event, grant); err != nil {
		return err
	}
	return tx.Commit()
}
