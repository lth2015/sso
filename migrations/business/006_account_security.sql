-- Browser-bound, single-use recovery-code management; no password/factor material.
CREATE TABLE IF NOT EXISTS security_actions (
 id BINARY(16) PRIMARY KEY,
 user_id BINARY(16) NOT NULL,
 session_id BINARY(16) NOT NULL UNIQUE,
 user_version BIGINT UNSIGNED NOT NULL,
 mfa_version BIGINT UNSIGNED NOT NULL,
 browser_digest BINARY(32) NOT NULL,
 csrf_digest BINARY(32) NOT NULL,
 state ENUM('pending','complete','failed') NOT NULL DEFAULT 'pending',
 attempts INT UNSIGNED NOT NULL DEFAULT 0,
 expires_at BIGINT NOT NULL,
 INDEX(expires_at),
 FOREIGN KEY(user_id) REFERENCES users(id),
 FOREIGN KEY(session_id) REFERENCES sso_sessions(id)
) ENGINE=InnoDB;
