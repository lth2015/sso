package mysql

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"time"
)

// FactorChange uses a separate table and ID space from recovery management.
// A recovery endpoint can never consume a replacement authorization.
type FactorChange struct {
	SecurityAction
	Pending []byte
}

func (s Store) BeginFactorChange(ctx context.Context, sessionDigest, browser, csrf []byte) (f FactorChange, err error) {
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
	if err = tx.QueryRowContext(ctx, `SELECT security_version FROM users WHERE id=? AND status='active' FOR UPDATE`, session.User).Scan(&version); err == sql.ErrNoRows {
		return f, ErrDenied
	}
	if err != nil {
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
	if current.User != session.User || version != current.UserVersion {
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
	if _, err = tx.ExecContext(ctx, `DELETE FROM authenticator_changes WHERE session_id=?`, f.Session); err != nil {
		return f, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO authenticator_changes(id,user_id,session_id,user_version,mfa_version,browser_digest,csrf_digest,expires_at) VALUES(?,?,?,?,?,?,?,?)`, f.ID, f.User, f.Session, f.UserVersion, f.MFAVersion, browser, csrf, f.Expires)
	if err != nil {
		return f, err
	}
	return f, tx.Commit()
}
func lockFactorChange(ctx context.Context, tx *sql.Tx, id ID, browser, sessionDigest, csrf []byte) (f FactorChange, err error) {
	err = tx.QueryRowContext(ctx, `SELECT id,user_id,session_id,user_version,mfa_version,browser_digest,csrf_digest,state,attempts,expires_at,pending_secret FROM authenticator_changes WHERE id=? FOR UPDATE`, id).Scan(&f.ID, &f.User, &f.Session, &f.UserVersion, &f.MFAVersion, &f.Browser, &f.CSRF, &f.State, &f.Attempts, &f.Expires, &f.Pending)
	if err == sql.ErrNoRows {
		return f, ErrDenied
	}
	if err != nil {
		return f, err
	}
	if len(browser) != 32 || len(csrf) != 32 || len(sessionDigest) != 32 || subtle.ConstantTimeCompare(f.Browser, browser) != 1 || subtle.ConstantTimeCompare(f.CSRF, csrf) != 1 || f.Expires <= time.Now().Unix() || f.Attempts >= 5 || (f.State != "pending" && f.State != "enrolling") {
		return f, ErrDenied
	}
	if (f.State == "pending" && len(f.Pending) != 0) || (f.State == "enrolling" && len(f.Pending) == 0) {
		return f, ErrDenied
	}
	var status string
	var version uint64
	if err = tx.QueryRowContext(ctx, `SELECT status,security_version FROM users WHERE id=? FOR UPDATE`, f.User).Scan(&status, &version); err != nil {
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
	if status != "active" || version != f.UserVersion || current.User != f.User || current.UserVersion != f.UserVersion || current.MFAVersion != f.MFAVersion {
		return f, ErrDenied
	}
	return f, nil
}
func (s Store) FactorChange(ctx context.Context, id ID, browser, sessionDigest, csrf []byte) (FactorChange, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return FactorChange{}, err
	}
	defer tx.Rollback()
	f, err := lockFactorChange(ctx, tx, id, browser, sessionDigest, csrf)
	if err != nil {
		return f, err
	}
	return f, tx.Commit()
}
func factorChangeFailure(ctx context.Context, tx *sql.Tx, id ID) error {
	if _, err := tx.ExecContext(ctx, `UPDATE authenticator_changes SET state=IF(attempts+1>=5,'failed',state),pending_secret=IF(attempts+1>=5,NULL,pending_secret),attempts=attempts+1 WHERE id=?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return ErrDenied
}
func (s Store) FailFactorChange(ctx context.Context, id ID, browser, sessionDigest, csrf []byte) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = lockFactorChange(ctx, tx, id, browser, sessionDigest, csrf); err != nil {
		return err
	}
	return factorChangeFailure(ctx, tx, id)
}
func (s Store) StageFactorChange(ctx context.Context, id ID, browser, sessionDigest, csrf []byte, proof PasswordCredential, factorVersion uint64, step int64, pending []byte) error {
	if len(pending) == 0 {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	f, err := lockFactorChange(ctx, tx, id, browser, sessionDigest, csrf)
	if err != nil {
		return err
	}
	if f.State != "pending" {
		return ErrDenied
	}
	if proof.User != f.User || proof.Version != f.UserVersion {
		return factorChangeFailure(ctx, tx, id)
	}
	var last int64
	var version uint64
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT last_step,security_version,active FROM mfa_factors WHERE user_id=? FOR UPDATE`, f.User).Scan(&last, &version, &active); err != nil {
		return err
	}
	if !active || version != factorVersion || version != f.MFAVersion || step <= last || step < 0 {
		return factorChangeFailure(ctx, tx, id)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mfa_factors SET last_step=? WHERE user_id=?`, step, f.User); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE authenticator_changes SET state='enrolling',pending_secret=? WHERE id=?`, pending, id); err != nil {
		return err
	}
	if err = identityEvent(ctx, tx, f.User, "mfa.replacement_started", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s Store) CancelFactorChange(ctx context.Context, id ID, browser, sessionDigest, csrf []byte) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	f, err := lockFactorChange(ctx, tx, id, browser, sessionDigest, csrf)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE authenticator_changes SET state='cancelled',pending_secret=NULL WHERE id=?`, id); err != nil {
		return err
	}
	if err = identityEvent(ctx, tx, f.User, "mfa.replacement_cancelled", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s Store) CommitFactorChange(ctx context.Context, id ID, browser, sessionDigest, csrf, pending, newCipher []byte, step int64, codes [][]byte) error {
	if len(newCipher) == 0 || step < 0 || len(codes) != 10 {
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
	f, err := lockFactorChange(ctx, tx, id, browser, sessionDigest, csrf)
	if err != nil {
		return err
	}
	if f.State != "enrolling" || len(pending) == 0 || subtle.ConstantTimeCompare(f.Pending, pending) != 1 {
		return ErrDenied
	}
	var version uint64
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT security_version,active FROM mfa_factors WHERE user_id=? FOR UPDATE`, f.User).Scan(&version, &active); err != nil {
		return err
	}
	if !active || version != f.MFAVersion {
		return ErrDenied
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id=?`, f.User); err != nil {
		return err
	}
	for _, d := range codes {
		if _, err = tx.ExecContext(ctx, `INSERT INTO mfa_recovery_codes(user_id,digest) VALUES(?,?)`, f.User, d); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mfa_factors SET encrypted_secret=?,last_step=?,security_version=security_version+1 WHERE user_id=?`, newCipher, step, f.User); err != nil {
		return err
	}
	if err = revokeSecurityAuthority(ctx, tx, f.User); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE authenticator_changes SET state='complete',pending_secret=NULL WHERE id=?`, id); err != nil {
		return err
	}
	if err = identityEvent(ctx, tx, f.User, "mfa.authenticator_replaced", id); err != nil {
		return err
	}
	return tx.Commit()
}
