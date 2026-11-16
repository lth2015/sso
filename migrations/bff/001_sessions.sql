-- Apply only to the dedicated BFF database. No cross-database views or foreign keys.
CREATE TABLE IF NOT EXISTS bff_transactions (
 state_digest BINARY(32) PRIMARY KEY,
 client_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 browser_digest BINARY(32) NOT NULL,
 encrypted_payload BLOB NOT NULL,
 consumed BOOLEAN NOT NULL DEFAULT FALSE,
 expires_at BIGINT NOT NULL,
 INDEX(client_id,expires_at)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS bff_sessions (
 token_digest BINARY(32) PRIMARY KEY,
 client_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 user_id BINARY(16) NOT NULL,
 grant_id BINARY(16) NOT NULL,
 encrypted_tokens BLOB NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 idle_expires_at BIGINT NOT NULL,
 absolute_expires_at BIGINT NOT NULL,
 INDEX(client_id,active,absolute_expires_at)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS bff_revocation_outbox (
 token_digest BINARY(32) PRIMARY KEY,
 client_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 encrypted_tokens BLOB NOT NULL,
 reason ENUM('authorization.logout','authorization.refresh_failed','authorization.session_expired') NOT NULL,
 state ENUM('pending','done') NOT NULL DEFAULT 'pending',
 attempts INT UNSIGNED NOT NULL DEFAULT 0,
 next_attempt_at BIGINT NOT NULL DEFAULT 0,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX(client_id,state,next_attempt_at)
) ENGINE=InnoDB;
