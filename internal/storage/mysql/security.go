package mysql

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"time"
)

type SecuritySummary struct {
	MFAEnabled        bool   `json:"mfa_enabled"`
	RecoveryRemaining int    `json:"recovery_codes_remaining"`
	ActiveSessions    int    `json:"active_sessions"`
	AuthenticatedAt   int64  `json:"authenticated_at"`
	SecondFactor      string `json:"second_factor"`
}

func (s Store) APISecurity(ctx context.Context, a Authorization) (v SecuritySummary, err error) {
	err = s.APITransaction(ctx, a, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT f.active,(SELECT COUNT(*) FROM mfa_recovery_codes r WHERE r.user_id=u.id AND r.used_at IS NULL),(SELECT COUNT(*) FROM sso_sessions x WHERE x.user_id=u.id AND x.active AND x.user_version=u.security_version AND x.mfa_version=f.security_version AND x.idle_expires_at>? AND x.absolute_expires_at>?),ss.auth_time,ss.amr FROM users u JOIN mfa_factors f ON f.user_id=u.id JOIN sso_sessions ss ON ss.user_id=u.id WHERE u.id=? AND ss.id=?`, time.Now().Unix(), time.Now().Unix(), a.User, a.Session).Scan(&v.MFAEnabled, &v.RecoveryRemaining, &v.ActiveSessions, &v.AuthenticatedAt, &v.SecondFactor)
	})
	return
}
func (s Store) APIRevokeOtherSessions(ctx context.Context, a Authorization) (count int64, err error) {
	a.Recent = true
	err = s.APITransaction(ctx, a, func(tx *sql.Tx) error {
		var user ID
		if e := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=? FOR UPDATE`, a.User).Scan(&user); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO protocol_logout_jobs(sid,user_id) SELECT DISTINCT g.hydra_sid,g.user_id FROM business_grants g JOIN sso_sessions live ON live.id=g.session_id AND live.active=TRUE WHERE g.user_id=? AND g.session_id<>? AND g.hydra_sid<>'' AND NOT EXISTS(SELECT 1 FROM business_grants kept WHERE kept.user_id=g.user_id AND kept.session_id=? AND kept.active=TRUE AND kept.hydra_sid=g.hydra_sid) ON DUPLICATE KEY UPDATE state='pending',attempts=0,next_attempt_at=0`, a.User, a.Session, a.Session); e != nil {
			return e
		}
		result, e := tx.ExecContext(ctx, `UPDATE sso_sessions SET active=FALSE WHERE user_id=? AND id<>? AND active=TRUE`, a.User, a.Session)
		if e != nil {
			return e
		}
		count, e = result.RowsAffected()
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bff_sessions b JOIN business_grants g ON g.id=b.grant_id SET b.active=FALSE WHERE g.user_id=? AND g.session_id<>?`, a.User, a.Session); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE business_grants SET active=FALSE WHERE user_id=? AND session_id<>?`, a.User, a.Session); e != nil {
			return e
		}
		if count == 0 {
			return nil
		}
		return identityEvent(ctx, tx, a.User, "session.others_revoked", a.Session)
	})
	return
}

type SecurityAction struct {
	ID, User, Session       ID
	UserVersion, MFAVersion uint64
	Browser, CSRF           []byte
	State                   string
	Attempts                int
	Expires                 int64
}

func (s Store) BeginSecurityAction(ctx context.Context, sessionDigest, browser, csrf []byte) (f SecurityAction, err error) {
	if len(sessionDigest) != 32 || len(browser) != 32 || len(csrf) != 32 {
		return f, ErrDenied
	}
	session, err := s.SSOSession(ctx, sessionDigest)
	if err != nil {
		return f, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return f, err
	}
	defer tx.Rollback()
	var version uint64
	if err = tx.QueryRowContext(ctx, `SELECT security_version FROM users WHERE id=? AND status='active' FOR UPDATE`, session.User).Scan(&version); err != nil {
		return f, err
	}
	now := time.Now().Unix()
	current, e := scanSession(tx.QueryRowContext(ctx, sessionSelect+` AND s.id=? AND s.token_digest=? FOR UPDATE`, now, now, session.ID, sessionDigest))
	if e == sql.ErrNoRows {
		return f, ErrDenied
	}
	if e != nil {
		return f, e
	}
	if current.User != session.User || version != session.UserVersion {
		return f, ErrDenied
	}
	f.ID, err = NewID()
	if err != nil {
		return f, err
	}
	f.User = current.User
	f.Session = current.ID
	f.UserVersion = current.UserVersion
	f.MFAVersion = current.MFAVersion
	f.Browser = browser
	f.CSRF = csrf
	f.State = "pending"
	f.Expires = now + 300
	if _, err = tx.ExecContext(ctx, `DELETE FROM security_actions WHERE session_id=?`, f.Session); err != nil {
		return f, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO security_actions(id,user_id,session_id,user_version,mfa_version,browser_digest,csrf_digest,expires_at) VALUES(?,?,?,?,?,?,?,?)`, f.ID, f.User, f.Session, f.UserVersion, f.MFAVersion, browser, csrf, f.Expires)
	if err != nil {
		return f, err
	}
	return f, tx.Commit()
}
func lockSecurityAction(ctx context.Context, tx *sql.Tx, id ID, browser, sessionDigest, csrf []byte) (f SecurityAction, err error) {
	err = tx.QueryRowContext(ctx, `SELECT id,user_id,session_id,user_version,mfa_version,browser_digest,csrf_digest,state,attempts,expires_at FROM security_actions WHERE id=? FOR UPDATE`, id).Scan(&f.ID, &f.User, &f.Session, &f.UserVersion, &f.MFAVersion, &f.Browser, &f.CSRF, &f.State, &f.Attempts, &f.Expires)
	if err == sql.ErrNoRows {
		return f, ErrDenied
	}
	if err != nil {
		return f, err
	}
	if len(browser) != 32 || len(csrf) != 32 || len(sessionDigest) != 32 || subtle.ConstantTimeCompare(f.Browser, browser) != 1 || subtle.ConstantTimeCompare(f.CSRF, csrf) != 1 || f.State != "pending" || f.Attempts >= 5 || f.Expires <= time.Now().Unix() {
		return f, ErrDenied
	}
	var status string
	var uv uint64
	if err = tx.QueryRowContext(ctx, `SELECT status,security_version FROM users WHERE id=? FOR UPDATE`, f.User).Scan(&status, &uv); err != nil {
		return f, err
	}
	now := time.Now().Unix()
	current, e := scanSession(tx.QueryRowContext(ctx, sessionSelect+` AND s.id=? AND s.token_digest=? FOR UPDATE`, now, now, f.Session, sessionDigest))
	if e == sql.ErrNoRows {
		return f, ErrDenied
	}
	if e != nil {
		return f, e
	}
	if status != "active" || uv != f.UserVersion || current.User != f.User || current.UserVersion != f.UserVersion || current.MFAVersion != f.MFAVersion {
		return f, ErrDenied
	}
	return f, nil
}
func (s Store) SecurityAction(ctx context.Context, id ID, browser, sessionDigest, csrf []byte) (SecurityAction, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return SecurityAction{}, err
	}
	defer tx.Rollback()
	f, err := lockSecurityAction(ctx, tx, id, browser, sessionDigest, csrf)
	if err != nil {
		return f, err
	}
	return f, tx.Commit()
}
func securityFailure(ctx context.Context, tx *sql.Tx, id ID) error {
	if _, err := tx.ExecContext(ctx, `UPDATE security_actions SET state=IF(attempts+1>=5,'failed',state),attempts=attempts+1 WHERE id=?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return ErrDenied
}
func (s Store) FailSecurityAction(ctx context.Context, id ID, browser, sessionDigest, csrf []byte) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = lockSecurityAction(ctx, tx, id, browser, sessionDigest, csrf); err != nil {
		return err
	}
	return securityFailure(ctx, tx, id)
}
func (s Store) RegenerateRecovery(ctx context.Context, id ID, browser, sessionDigest, csrf []byte, proof PasswordCredential, factorVersion uint64, step int64, codes [][]byte) error {
	if len(codes) != 10 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, d := range codes {
		if len(d) != 32 || seen[string(d)] {
			return ErrInvalid
		}
		seen[string(d)] = true
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	f, err := lockSecurityAction(ctx, tx, id, browser, sessionDigest, csrf)
	if err != nil {
		return err
	}
	if proof.User != f.User || proof.Version != f.UserVersion {
		return securityFailure(ctx, tx, id)
	}
	var last int64
	var version uint64
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT last_step,security_version,active FROM mfa_factors WHERE user_id=? FOR UPDATE`, f.User).Scan(&last, &version, &active); err != nil {
		return err
	}
	if !active || version != factorVersion || version != f.MFAVersion || step <= last || step < 0 {
		return securityFailure(ctx, tx, id)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id=?`, f.User); err != nil {
		return err
	}
	for _, d := range codes {
		if _, err = tx.ExecContext(ctx, `INSERT INTO mfa_recovery_codes(user_id,digest) VALUES(?,?)`, f.User, d); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mfa_factors SET last_step=?,security_version=security_version+1 WHERE user_id=?`, step, f.User); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET security_version=security_version+1 WHERE id=?`, f.User); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO protocol_logout_jobs(sid,user_id) SELECT DISTINCT hydra_sid,user_id FROM business_grants WHERE user_id=? AND hydra_sid<>'' ON DUPLICATE KEY UPDATE state='pending',attempts=0,next_attempt_at=0`, f.User); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sso_sessions SET active=FALSE WHERE user_id=?`, f.User); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE bff_sessions SET active=FALSE WHERE user_id=?`, f.User); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE business_grants SET active=FALSE WHERE user_id=?`, f.User); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE security_actions SET state='complete' WHERE id=?`, id); err != nil {
		return err
	}
	if err = identityEvent(ctx, tx, f.User, "mfa.recovery_regenerated", id); err != nil {
		return err
	}
	return tx.Commit()
}
