package mysql

import (
	"context"
	"database/sql"
	"errors"
	driver "github.com/go-sql-driver/mysql"
	"time"
)

// ErrChallenge intentionally conflates unknown, expired, consumed and inapplicable proofs.
var ErrChallenge = ErrInvalid

type AccountChallenge struct {
	ID, NotificationID       ID
	Digest                   []byte
	Purpose, Email, Original string
	Expires                  int64
	Ciphertext               []byte
}

func (s Store) EnqueueAccountChallenge(ctx context.Context, c AccountChallenge) error {
	if len(c.Digest) != 32 || c.ID.empty() || c.NotificationID.empty() || (c.Purpose != "register" && c.Purpose != "reset") {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var user ID
	var version uint64
	var status string
	var verified sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT u.id,u.security_version,u.status,a.verified_at FROM login_aliases a JOIN users u ON u.id=a.user_id WHERE a.normalized_email=? FOR SHARE`, c.Email).Scan(&user, &version, &status, &verified)
	exists := err == nil
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if c.Purpose == "register" && exists {
		return nil
	}
	if c.Purpose == "reset" && (!exists || !verified.Valid || status != "active") {
		return nil
	}
	var userArg, versionArg any
	if c.Purpose == "reset" {
		userArg = user
		versionArg = version
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO account_challenges(id,token_digest,purpose,normalized_email,original_email,user_id,user_version,expires_at) VALUES(?,?,?,?,?,?,?,?)`, c.ID, c.Digest, c.Purpose, c.Email, c.Original, userArg, versionArg, c.Expires)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_notifications(id,challenge_id,recipient,purpose,encrypted_payload,expires_at) VALUES(?,?,?,?,?,?)`, c.NotificationID, c.ID, c.Email, c.Purpose, c.Ciphertext, c.Expires); err != nil {
		return err
	}
	event, err := NewID()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO identity_events(id,user_id,action,target_id) VALUES(?,?,?,?)`, event, userArg, "account."+c.Purpose+"_requested", c.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s Store) ChallengeUsable(ctx context.Context, digest []byte, purpose string, now int64) error {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_challenges WHERE token_digest=? AND purpose=? AND consumed_at IS NULL AND expires_at>?`, digest, purpose, now).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrChallenge
	}
	return nil
}

func (s Store) CompleteRegistration(ctx context.Context, digest []byte, hash string, now int64) (ID, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ID{}, err
	}
	defer tx.Rollback()
	var challenge ID
	var email, original string
	err = tx.QueryRowContext(ctx, `SELECT id,normalized_email,original_email FROM account_challenges WHERE token_digest=? AND purpose='register' AND consumed_at IS NULL AND expires_at>? FOR UPDATE`, digest, now).Scan(&challenge, &email, &original)
	if err == sql.ErrNoRows {
		return ID{}, ErrChallenge
	}
	if err != nil {
		return ID{}, err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM login_aliases WHERE normalized_email=?`, email).Scan(&exists); err != nil {
		return ID{}, err
	}
	if exists != 0 {
		return ID{}, ErrChallenge
	}
	user, err := NewID()
	if err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO users(id) VALUES(?)`, user); err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO login_aliases(normalized_email,user_id,verified_at) VALUES(?,?,UTC_TIMESTAMP(6))`, email, user); err != nil {
		var duplicate *driver.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			return ID{}, ErrChallenge
		}
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_profiles VALUES(?,?)`, user, original); err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO password_credentials(user_id,password_hash) VALUES(?,?)`, user, hash); err != nil {
		return ID{}, err
	}
	if _, err = bootstrapTenantTx(ctx, tx, user, "personal-"+user.String(), "个人空间"); err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE account_challenges SET consumed_at=UTC_TIMESTAMP(6) WHERE id=?`, challenge); err != nil {
		return ID{}, err
	}
	if err = identityEvent(ctx, tx, user, "account.registered", user); err != nil {
		return ID{}, err
	}
	if err = tx.Commit(); err != nil {
		return ID{}, err
	}
	return user, nil
}

func (s Store) CompletePasswordReset(ctx context.Context, digest []byte, hash string, now int64) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var challenge, user ID
	var boundVersion uint64
	err = tx.QueryRowContext(ctx, `SELECT id,user_id,user_version FROM account_challenges WHERE token_digest=? AND purpose='reset' AND consumed_at IS NULL AND expires_at>? FOR UPDATE`, digest, now).Scan(&challenge, &user, &boundVersion)
	if err == sql.ErrNoRows {
		return ErrChallenge
	}
	if err != nil {
		return err
	}
	var status string
	var version uint64
	if err = tx.QueryRowContext(ctx, `SELECT status,security_version FROM users WHERE id=? FOR UPDATE`, user).Scan(&status, &version); err != nil {
		return err
	}
	if status != "active" || version != boundVersion {
		return ErrChallenge
	}
	result, err := tx.ExecContext(ctx, `UPDATE password_credentials SET password_hash=?,changed_at=UTC_TIMESTAMP(6) WHERE user_id=?`, hash, user)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return ErrChallenge
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET security_version=security_version+1 WHERE id=?`, user); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE account_challenges SET consumed_at=UTC_TIMESTAMP(6) WHERE id=?`, challenge); err != nil {
		return err
	}
	if err = identityEvent(ctx, tx, user, "account.password_reset", user); err != nil {
		return err
	}
	return tx.Commit()
}
func identityEvent(ctx context.Context, tx *sql.Tx, user ID, action string, target ID) error {
	id, err := NewID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO identity_events(id,user_id,action,target_id) VALUES(?,?,?,?)`, id, user, action, target)
	return err
}

type PasswordCredential struct {
	User    ID
	Hash    string
	Version uint64
}

func (s Store) PasswordCredential(ctx context.Context, email string) (PasswordCredential, error) {
	var c PasswordCredential
	err := s.db.QueryRowContext(ctx, `SELECT u.id,p.password_hash,u.security_version FROM users u JOIN login_aliases a ON a.user_id=u.id JOIN password_credentials p ON p.user_id=u.id WHERE a.normalized_email=? AND a.verified_at IS NOT NULL AND u.status='active'`, email).Scan(&c.User, &c.Hash, &c.Version)
	if err == sql.ErrNoRows {
		return c, ErrDenied
	}
	return c, err
}
func (s Store) CredentialCurrent(ctx context.Context, user ID, version uint64) error {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id=? AND status='active' AND security_version=?`, user, version).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrDenied
	}
	return nil
}

type Notification struct {
	ID                 ID
	Recipient, Purpose string
	Ciphertext         []byte
	Expires            int64
}

// DeliverOne holds a row lock while the local idempotent sink writes. Failure
// rolls back its acknowledgement; SKIP LOCKED permits independent workers.
func (s Store) DeliverOne(ctx context.Context, deliver func(Notification) error) (bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var n Notification
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT id,recipient,purpose,encrypted_payload,expires_at,attempts FROM account_notifications WHERE state='pending' AND next_attempt_at<=? ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`, time.Now().Unix()).Scan(&n.ID, &n.Recipient, &n.Purpose, &n.Ciphertext, &n.Expires, &attempts)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	state := "delivered"
	if n.Expires <= time.Now().Unix() {
		state = "expired"
	} else if err = deliver(n); err != nil {
		delay := int64(1) << min(attempts+1, 6)
		if _, updateErr := tx.ExecContext(ctx, `UPDATE account_notifications SET attempts=attempts+1,next_attempt_at=? WHERE id=?`, time.Now().Unix()+delay, n.ID); updateErr != nil {
			return false, updateErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return false, commitErr
		}
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE account_notifications SET state=?,encrypted_payload='' WHERE id=?`, state, n.ID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s Store) AccountReady(ctx context.Context) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='003_accounts'`).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return errors.New("account schema unavailable")
	}
	return nil
}
