-- Separate management purpose: old recovery endpoints cannot consume these IDs.
CREATE TABLE IF NOT EXISTS authenticator_changes (
 id BINARY(16) PRIMARY KEY,
 user_id BINARY(16) NOT NULL,
 session_id BINARY(16) NOT NULL UNIQUE,
 user_version BIGINT UNSIGNED NOT NULL,
 mfa_version BIGINT UNSIGNED NOT NULL,
 browser_digest BINARY(32) NOT NULL,
 csrf_digest BINARY(32) NOT NULL,
 state ENUM('pending','enrolling','complete','failed','cancelled') NOT NULL DEFAULT 'pending',
 attempts INT UNSIGNED NOT NULL DEFAULT 0,
 expires_at BIGINT NOT NULL,
 pending_secret BLOB NULL,
 INDEX(expires_at),
 FOREIGN KEY(user_id) REFERENCES users(id),
 FOREIGN KEY(session_id) REFERENCES sso_sessions(id)
) ENGINE=InnoDB;
