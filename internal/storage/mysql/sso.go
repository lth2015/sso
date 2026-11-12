package mysql

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type OAuthClient struct {
	ID                               string
	Application                      ID
	Name, Redirect, Scopes, Audience string
	Version                          uint64
}

func (s Store) OAuthClient(ctx context.Context, id string) (OAuthClient, error) {
	var c OAuthClient
	err := s.db.QueryRowContext(ctx, `SELECT c.client_id,c.application_id,a.name,c.redirect_uri,c.scopes,c.audience,c.security_version FROM oauth_clients c JOIN applications a ON a.id=c.application_id WHERE c.client_id=? AND c.active=TRUE AND a.active=TRUE`, id).Scan(&c.ID, &c.Application, &c.Name, &c.Redirect, &c.Scopes, &c.Audience, &c.Version)
	if err == sql.ErrNoRows {
		return c, ErrDenied
	}
	return c, err
}

type Flow struct {
	ID                                                       ID
	Kind, Client, State, HydraSID, Scopes, Audience, AMR     string
	Challenge, ChallengeDigest, BrowserDigest, CSRF, Pending []byte
	User, Session                                            ID
	UserVersion, MFAVersion                                  uint64
	AuthenticatedAt, Expires                                 int64
	Attempts                                                 int
}
type scanner interface{ Scan(...any) error }

func scanFlow(row scanner) (Flow, error) {
	var f Flow
	var user, session []byte
	var uv, mv, at sql.NullInt64
	err := row.Scan(&f.ID, &f.Kind, &f.Client, &f.State, &f.HydraSID, &f.Scopes, &f.Audience, &f.AMR, &f.Challenge, &f.BrowserDigest, &f.CSRF, &f.Pending, &user, &session, &uv, &mv, &at, &f.Expires, &f.Attempts)
	if err != nil {
		return f, err
	}
	if user != nil {
		_ = f.User.Scan(user)
	}
	if session != nil {
		_ = f.Session.Scan(session)
	}
	f.UserVersion = uint64(uv.Int64)
	f.MFAVersion = uint64(mv.Int64)
	f.AuthenticatedAt = at.Int64
	return f, nil
}

const flowSelect = `SELECT id,kind,client_id,state,hydra_sid,requested_scopes,audience,amr,encrypted_challenge,browser_digest,csrf_digest,pending_secret,user_id,session_id,user_version,mfa_version,authenticated_at,expires_at,attempts FROM login_interactions`

func (s Store) CreateFlow(ctx context.Context, f Flow) error {
	var user, session, uv, mv, at any
	if !f.User.empty() {
		user = f.User
		uv = f.UserVersion
	}
	if !f.Session.empty() {
		session = f.Session
		mv = f.MFAVersion
		at = f.AuthenticatedAt
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO login_interactions(id,kind,challenge_digest,encrypted_challenge,browser_digest,csrf_digest,client_id,user_id,user_version,session_id,hydra_sid,requested_scopes,audience,amr,mfa_version,authenticated_at,state,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, f.ID, f.Kind, f.ChallengeDigest, f.Challenge, f.BrowserDigest, f.CSRF, f.Client, user, uv, session, f.HydraSID, f.Scopes, f.Audience, f.AMR, mv, at, f.State, f.Expires)
	return err
}
func (s Store) Flow(ctx context.Context, id ID, browser, csrf []byte) (Flow, error) {
	f, err := scanFlow(s.db.QueryRowContext(ctx, flowSelect+` WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return f, ErrDenied
	}
	if err != nil {
		return f, err
	}
	if !flowBound(f, browser, csrf) {
		return f, ErrDenied
	}
	return f, nil
}
func flowBound(f Flow, browser, csrf []byte) bool {
	return len(browser) == 32 && subtle.ConstantTimeCompare(f.BrowserDigest, browser) == 1 && (csrf == nil || (len(csrf) == 32 && subtle.ConstantTimeCompare(f.CSRF, csrf) == 1)) && f.Expires > time.Now().Unix() && f.Attempts < 5 && f.State != "failed" && f.State != "complete" && f.State != "submitting"
}
func lockFlow(ctx context.Context, tx *sql.Tx, id ID, browser []byte) (Flow, error) {
	f, err := scanFlow(tx.QueryRowContext(ctx, flowSelect+` WHERE id=? FOR UPDATE`, id))
	if err == sql.ErrNoRows {
		return f, ErrDenied
	}
	if err != nil {
		return f, err
	}
	if !flowBound(f, browser, nil) {
		return f, ErrDenied
	}
	return f, nil
}
func failAttempt(ctx context.Context, tx *sql.Tx, id ID) error {
	_, err := tx.ExecContext(ctx, `UPDATE login_interactions SET state=IF(attempts+1>=5,'failed',state),attempts=attempts+1 WHERE id=?`, id)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return ErrDenied
}
func (s Store) FailedPassword(ctx context.Context, id ID, browser []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	f, err := lockFlow(ctx, tx, id, browser)
	if err != nil {
		return err
	}
	if f.State != "password" {
		return ErrConflict
	}
	return failAttempt(ctx, tx, id)
}

type Factor struct {
	Cipher   []byte
	LastStep int64
	Version  uint64
	Active   bool
}

func (s Store) Factor(ctx context.Context, user ID) (Factor, error) {
	var f Factor
	err := s.db.QueryRowContext(ctx, `SELECT encrypted_secret,last_step,security_version,active FROM mfa_factors WHERE user_id=?`, user).Scan(&f.Cipher, &f.LastStep, &f.Version, &f.Active)
	if err == sql.ErrNoRows {
		return f, ErrNotFound
	}
	return f, err
}
func (s Store) PasswordPassed(ctx context.Context, id ID, browser []byte, proof PasswordCredential, pending []byte) (string, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	f, err := lockFlow(ctx, tx, id, browser)
	if err != nil {
		return "", err
	}
	if f.State != "password" {
		return "", ErrConflict
	}
	var version uint64
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status,security_version FROM users WHERE id=? FOR UPDATE`, proof.User).Scan(&status, &version); err != nil {
		return "", err
	}
	if status != "active" || version != proof.Version {
		return "", ErrDenied
	}
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT active FROM mfa_factors WHERE user_id=? FOR SHARE`, proof.User).Scan(&active)
	state := "enroll"
	if err == nil {
		if !active {
			return "", ErrDenied
		}
		state = "mfa"
		pending = nil
	} else if err != sql.ErrNoRows {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE login_interactions SET user_id=?,user_version=?,state=?,pending_secret=? WHERE id=?`, proof.User, proof.Version, state, pending, id); err != nil {
		return "", err
	}
	return state, tx.Commit()
}

type MFAResult struct {
	Step           int64
	FactorVersion  uint64
	NewCipher      []byte
	Recovery       [][]byte
	RecoveryDigest []byte
}

func (s Store) MFAPassed(ctx context.Context, id ID, browser []byte, result MFAResult) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	f, err := lockFlow(ctx, tx, id, browser)
	if err != nil {
		return err
	}
	if f.State != "enroll" && f.State != "mfa" {
		return ErrConflict
	}
	var status string
	var version uint64
	if err = tx.QueryRowContext(ctx, `SELECT status,security_version FROM users WHERE id=? FOR UPDATE`, f.User).Scan(&status, &version); err != nil {
		return err
	}
	if status != "active" || version != f.UserVersion {
		return ErrDenied
	}
	var factor Factor
	err = tx.QueryRowContext(ctx, `SELECT encrypted_secret,last_step,security_version,active FROM mfa_factors WHERE user_id=? FOR UPDATE`, f.User).Scan(&factor.Cipher, &factor.LastStep, &factor.Version, &factor.Active)
	amr := "otp"
	mfaVersion := uint64(1)
	if f.State == "enroll" {
		if err != sql.ErrNoRows || len(result.NewCipher) == 0 || result.Step < 0 || len(result.Recovery) != 10 {
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			return failAttempt(ctx, tx, id)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO mfa_factors(user_id,encrypted_secret,last_step) VALUES(?,?,?)`, f.User, result.NewCipher, result.Step); err != nil {
			return err
		}
		for _, digest := range result.Recovery {
			if len(digest) != 32 {
				return ErrInvalid
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO mfa_recovery_codes(user_id,digest) VALUES(?,?)`, f.User, digest); err != nil {
				return err
			}
		}
		if err = identityEvent(ctx, tx, f.User, "mfa.enrolled", f.User); err != nil {
			return err
		}
	} else {
		if err == sql.ErrNoRows {
			return failAttempt(ctx, tx, id)
		}
		if err != nil {
			return err
		}
		if !factor.Active || factor.Version != result.FactorVersion {
			return failAttempt(ctx, tx, id)
		}
		mfaVersion = factor.Version
		if len(result.RecoveryDigest) > 0 {
			update, err := tx.ExecContext(ctx, `UPDATE mfa_recovery_codes SET used_at=UTC_TIMESTAMP(6) WHERE user_id=? AND digest=? AND used_at IS NULL`, f.User, result.RecoveryDigest)
			if err != nil {
				return err
			}
			count, err := update.RowsAffected()
			if err != nil {
				return err
			}
			if count != 1 {
				return failAttempt(ctx, tx, id)
			}
			amr = "recovery"
		} else {
			if result.Step <= factor.LastStep || result.Step < 0 {
				return failAttempt(ctx, tx, id)
			}
			if _, err = tx.ExecContext(ctx, `UPDATE mfa_factors SET last_step=? WHERE user_id=?`, result.Step, f.User); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE login_interactions SET state='passed',pending_secret=NULL,mfa_version=?,authenticated_at=?,amr=? WHERE id=?`, mfaVersion, time.Now().Unix(), amr, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Invalid MFA proofs consume an attempt without changing factor state.
func (s Store) FailedMFA(ctx context.Context, id ID, browser []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	f, err := lockFlow(ctx, tx, id, browser)
	if err != nil {
		return err
	}
	if f.State != "mfa" && f.State != "enroll" {
		return ErrConflict
	}
	return failAttempt(ctx, tx, id)
}

type SSOSession struct {
	ID, User                 ID
	UserVersion, MFAVersion  uint64
	AuthTime, Idle, Absolute int64
	AMR                      string
}

func scanSession(row scanner) (SSOSession, error) {
	var s SSOSession
	err := row.Scan(&s.ID, &s.User, &s.UserVersion, &s.MFAVersion, &s.AuthTime, &s.AMR, &s.Idle, &s.Absolute)
	return s, err
}

const sessionSelect = `SELECT s.id,s.user_id,s.user_version,s.mfa_version,s.auth_time,s.amr,s.idle_expires_at,s.absolute_expires_at FROM sso_sessions s JOIN users u ON u.id=s.user_id JOIN mfa_factors f ON f.user_id=u.id WHERE s.active=TRUE AND u.status='active' AND s.user_version=u.security_version AND f.active=TRUE AND s.mfa_version=f.security_version AND s.idle_expires_at>? AND s.absolute_expires_at>?`

func (s Store) SSOSession(ctx context.Context, digest []byte) (SSOSession, error) {
	now := time.Now().Unix()
	session, err := scanSession(s.db.QueryRowContext(ctx, sessionSelect+` AND s.token_digest=?`, now, now, digest))
	if err == sql.ErrNoRows {
		return session, ErrDenied
	}
	if err != nil {
		return session, err
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE sso_sessions SET idle_expires_at=LEAST(absolute_expires_at,?) WHERE id=? AND active=TRUE`, now+8*3600, session.ID); err != nil {
		return session, err
	}
	return session, nil
}
func (s Store) PrepareLogin(ctx context.Context, id ID, browser, tokenDigest []byte) (SSOSession, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return SSOSession{}, false, err
	}
	defer tx.Rollback()
	f, err := lockFlow(ctx, tx, id, browser)
	if err != nil {
		return SSOSession{}, false, err
	}
	if f.State != "passed" {
		return SSOSession{}, false, ErrConflict
	}
	now := time.Now().Unix()
	session := SSOSession{ID: f.Session, User: f.User, UserVersion: f.UserVersion, MFAVersion: f.MFAVersion, AuthTime: f.AuthenticatedAt, AMR: f.AMR, Idle: now + 8*3600, Absolute: now + 24*3600}
	fresh := f.Session.empty()
	if fresh {
		var valid int
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u JOIN mfa_factors m ON m.user_id=u.id WHERE u.id=? AND u.status='active' AND u.security_version=? AND m.active=TRUE AND m.security_version=?`, f.User, f.UserVersion, f.MFAVersion).Scan(&valid)
		if err != nil {
			return session, false, err
		}
		if valid != 1 || f.AuthenticatedAt <= 0 || f.AuthenticatedAt > now {
			return session, false, ErrDenied
		}
		session.ID, err = NewID()
		if err != nil {
			return session, false, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO sso_sessions(id,token_digest,user_id,user_version,mfa_version,auth_time,amr,idle_expires_at,absolute_expires_at) VALUES(?,?,?,?,?,?,?,?,?)`, session.ID, tokenDigest, f.User, f.UserVersion, f.MFAVersion, f.AuthenticatedAt, f.AMR, session.Idle, session.Absolute); err != nil {
			return session, false, err
		}
	} else {
		session, err = scanSession(tx.QueryRowContext(ctx, sessionSelect+` AND s.id=? FOR SHARE`, now, now, f.Session))
		if err == sql.ErrNoRows {
			return session, false, ErrDenied
		}
		if err != nil {
			return session, false, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE login_interactions SET state='submitting',session_id=? WHERE id=?`, session.ID, id); err != nil {
		return session, false, err
	}
	if err = identityEvent(ctx, tx, session.User, "session.authenticated", session.ID); err != nil {
		return session, false, err
	}
	return session, fresh, tx.Commit()
}
func (s Store) FinishFlow(ctx context.Context, id ID, success bool, revoke ID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state := "failed"
	if success {
		state = "complete"
	}
	result, err := tx.ExecContext(ctx, `UPDATE login_interactions SET state=?,encrypted_challenge='',pending_secret=NULL WHERE id=? AND state='submitting'`, state, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	if !revoke.empty() {
		if _, err = tx.ExecContext(ctx, `UPDATE sso_sessions SET active=FALSE WHERE id=?`, revoke); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type TenantOption struct {
	ID   ID
	Name string
}

func (s Store) TenantOptions(ctx context.Context, user, session ID, client string) ([]TenantOption, error) {
	now := time.Now().Unix()
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,root.name FROM memberships m JOIN tenants t ON t.id=m.tenant_id JOIN tenant_roots tr ON tr.tenant_id=t.id JOIN org_nodes root ON root.tenant_id=tr.tenant_id AND root.id=tr.org_id JOIN oauth_clients c ON c.client_id=? AND c.active=TRUE JOIN applications a ON a.id=c.application_id AND a.active=TRUE JOIN tenant_applications ta ON ta.tenant_id=t.id AND ta.application_id=a.id AND ta.active=TRUE `+userAccessJoinSQL+` JOIN sso_sessions s ON s.id=? AND s.user_id=m.user_id JOIN users u ON u.id=m.user_id JOIN mfa_factors f ON f.user_id=u.id WHERE m.user_id=? AND m.status='active' AND t.status='active' AND u.status='active' AND s.active=TRUE AND s.user_version=u.security_version AND f.active=TRUE AND f.security_version=s.mfa_version AND s.idle_expires_at>? AND s.absolute_expires_at>? AND `+userAccessAllowedSQL+` ORDER BY t.id`, client, session, user, now, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []TenantOption{}
	for rows.Next() {
		var o TenantOption
		if err = rows.Scan(&o.ID, &o.Name); err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	return items, rows.Err()
}
func (s Store) PrepareConsent(ctx context.Context, id ID, browser, sessionDigest []byte, tenant ID) (ID, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ID{}, err
	}
	defer tx.Rollback()
	f, err := lockFlow(ctx, tx, id, browser)
	if err != nil {
		return ID{}, err
	}
	if f.State != "consent" {
		return ID{}, ErrConflict
	}
	now := time.Now().Unix()
	session, err := scanSession(tx.QueryRowContext(ctx, sessionSelect+` AND s.token_digest=? AND s.id=? FOR SHARE`, now, now, sessionDigest, f.Session))
	if err == sql.ErrNoRows {
		return ID{}, ErrDenied
	}
	if err != nil {
		return ID{}, err
	}
	if session.User != f.User {
		return ID{}, ErrDenied
	}
	var member ID
	var memberVersion, tenantVersion, clientVersion, appVersion, enabledVersion, accessVersion uint64
	var scopes, audience string
	err = tx.QueryRowContext(ctx, `SELECT m.id,m.security_version,t.authz_version,c.security_version,a.security_version,ta.security_version,COALESCE(uap.version,1),c.scopes,c.audience FROM memberships m JOIN tenants t ON t.id=m.tenant_id JOIN oauth_clients c ON c.client_id=? AND c.active=TRUE JOIN applications a ON a.id=c.application_id AND a.active=TRUE JOIN tenant_applications ta ON ta.tenant_id=t.id AND ta.application_id=a.id AND ta.active=TRUE `+userAccessJoinSQL+` WHERE m.tenant_id=? AND m.user_id=? AND m.status='active' AND t.status='active' AND `+userAccessAllowedSQL+` FOR SHARE`, f.Client, tenant, session.User).Scan(&member, &memberVersion, &tenantVersion, &clientVersion, &appVersion, &enabledVersion, &accessVersion, &scopes, &audience)
	if err == sql.ErrNoRows {
		return ID{}, ErrDenied
	}
	if err != nil {
		return ID{}, err
	}
	if audience != f.Audience || !scopesWithin(strings.Fields(f.Scopes), scopes) || f.HydraSID == "" {
		return ID{}, ErrDenied
	}
	grant, err := NewID()
	if err != nil {
		return ID{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO business_grants(id,interaction_id,tenant_id,membership_id,user_id,session_id,client_id,scopes,audience,hydra_sid,user_version,membership_version,tenant_version,client_version,application_version,enabled_version,absolute_expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, grant, id, tenant, member, session.User, session.ID, f.Client, f.Scopes, f.Audience, f.HydraSID, session.UserVersion, memberVersion, tenantVersion, clientVersion, appVersion, enabledVersion, session.Absolute)
	if err != nil {
		return ID{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO application_user_grant_versions(grant_id,tenant_id,application_id,version) SELECT ?,?,application_id,? FROM oauth_clients WHERE client_id=?`, grant, tenant, accessVersion, f.Client)
	if err != nil {
		return ID{}, err
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return ID{}, ErrDenied
	}
	if _, err = tx.ExecContext(ctx, `UPDATE login_interactions SET state='submitting' WHERE id=?`, id); err != nil {
		return ID{}, err
	}
	if err = record(ctx, tx, tenant, member, "authorization.consented", grant, 1); err != nil {
		return ID{}, err
	}
	return grant, tx.Commit()
}
func scopesWithin(values []string, upper string) bool {
	allowed := map[string]bool{}
	for _, v := range strings.Fields(upper) {
		allowed[v] = true
	}
	if len(values) == 0 {
		return false
	}
	for _, v := range values {
		if !allowed[v] {
			return false
		}
	}
	return true
}
func (s Store) LogoutAll(ctx context.Context, digest, csrf []byte) (ID, error) {
	session, err := s.SSOSession(ctx, digest)
	if err != nil {
		return ID{}, err
	}
	var proof []byte
	if err = s.db.QueryRowContext(ctx, `SELECT logout_csrf_digest FROM sso_sessions WHERE id=?`, session.ID).Scan(&proof); err != nil {
		return ID{}, err
	}
	if len(csrf) != 32 || subtle.ConstantTimeCompare(proof, csrf) != 1 {
		return ID{}, ErrDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ID{}, err
	}
	defer tx.Rollback()
	var version uint64
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status,security_version FROM users WHERE id=? FOR UPDATE`, session.User).Scan(&status, &version); err != nil {
		return ID{}, err
	}
	if status != "active" || version != session.UserVersion {
		return ID{}, ErrDenied
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT active FROM sso_sessions WHERE id=? AND logout_csrf_digest=? FOR UPDATE`, session.ID, csrf).Scan(&active); err != nil {
		return ID{}, err
	}
	if !active {
		return ID{}, ErrDenied
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO protocol_logout_jobs(sid,user_id) SELECT DISTINCT g.hydra_sid,g.user_id FROM business_grants g JOIN sso_sessions s ON s.id=g.session_id WHERE g.user_id=? AND s.active=TRUE AND g.hydra_sid<>'' ON DUPLICATE KEY UPDATE state='pending',attempts=0,next_attempt_at=0`, session.User); err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sso_sessions SET active=FALSE WHERE user_id=?`, session.User); err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET security_version=security_version+1 WHERE id=?`, session.User); err != nil {
		return ID{}, err
	}
	if err = identityEvent(ctx, tx, session.User, "session.logout_all", session.User); err != nil {
		return ID{}, err
	}
	return session.User, tx.Commit()
}
func (s Store) SSOReady(ctx context.Context) error {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version IN ('004_sso','006_account_security','007_authenticator_changes','008_managed_clients','009_machine_credentials','012_application_user_access')`).Scan(&count)
	if err != nil {
		return err
	}
	if count != 6 {
		return errors.New("SSO schema unavailable")
	}
	return nil
}

func (s Store) LogoutCSRF(ctx context.Context, digest, csrf []byte) error {
	session, err := s.SSOSession(ctx, digest)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE sso_sessions SET logout_csrf_digest=? WHERE id=?`, csrf, session.ID)
	return err
}

func (s Store) DeliverProtocolLogout(ctx context.Context, send func(string, string) error) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var sid string
	var user ID
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT sid,user_id,attempts FROM protocol_logout_jobs WHERE state='pending' AND next_attempt_at<=? LIMIT 1 FOR UPDATE SKIP LOCKED`, time.Now().Unix()).Scan(&sid, &user, &attempts)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = send(user.String(), sid); err != nil {
		if _, e := tx.ExecContext(ctx, `UPDATE protocol_logout_jobs SET attempts=attempts+1,next_attempt_at=? WHERE sid=?`, time.Now().Unix()+(1<<min(attempts+1, 6)), sid); e != nil {
			return false, e
		}
		if e := tx.Commit(); e != nil {
			return false, e
		}
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE protocol_logout_jobs SET state='done' WHERE sid=?`, sid); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s Store) PruneAuth(ctx context.Context) error {
	now := time.Now().Unix()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM authenticator_changes WHERE expires_at<=?`, now); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM security_actions WHERE expires_at<=?`, now); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM bff_transactions WHERE expires_at<=?`, now); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM bff_sessions WHERE absolute_expires_at<=? OR idle_expires_at<=?`, now, now); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE login_interactions SET encrypted_challenge='',pending_secret=NULL,state=IF(state='complete','complete','failed') WHERE expires_at<=? AND (LENGTH(encrypted_challenge)>0 OR pending_secret IS NOT NULL)`, now)
	return err
}

func (s Store) BeginReject(ctx context.Context, id ID, browser []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	f, err := lockFlow(ctx, tx, id, browser)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE login_interactions SET state='submitting' WHERE id=?`, id); err != nil {
		return err
	}
	event, err := NewID()
	if err != nil {
		return err
	}
	var user any
	if !f.User.empty() {
		user = f.User
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO identity_events(id,user_id,action,target_id) VALUES(?,?,'authorization.rejected',?)`, event, user, id); err != nil {
		return err
	}
	return tx.Commit()
}
