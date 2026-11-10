CREATE TABLE IF NOT EXISTS account_profiles (
 user_id BINARY(16) PRIMARY KEY,
 original_email VARCHAR(254) NOT NULL,
 FOREIGN KEY(user_id) REFERENCES users(id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS password_credentials (
 user_id BINARY(16) PRIMARY KEY,
 password_hash VARBINARY(255) NOT NULL,
 changed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 FOREIGN KEY(user_id) REFERENCES users(id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS account_challenges (
 id BINARY(16) PRIMARY KEY,
 token_digest BINARY(32) NOT NULL UNIQUE,
 purpose ENUM('register','reset') NOT NULL,
 normalized_email VARCHAR(254) COLLATE utf8mb4_bin NOT NULL,
 original_email VARCHAR(254) NOT NULL,
 user_id BINARY(16) NULL,
 user_version BIGINT UNSIGNED NULL,
 expires_at BIGINT NOT NULL,
 consumed_at DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 FOREIGN KEY(user_id) REFERENCES users(id),
 CHECK((purpose='register' AND user_id IS NULL AND user_version IS NULL) OR (purpose='reset' AND user_id IS NOT NULL AND user_version IS NOT NULL)),
 INDEX(normalized_email,purpose,created_at)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS account_notifications (
 id BINARY(16) PRIMARY KEY,
 challenge_id BINARY(16) NOT NULL UNIQUE,
 recipient VARCHAR(254) COLLATE utf8mb4_bin NOT NULL,
 purpose ENUM('register','reset') NOT NULL,
 encrypted_payload BLOB NOT NULL,
 expires_at BIGINT NOT NULL,
 state ENUM('pending','delivered','expired') NOT NULL DEFAULT 'pending',
 attempts INT UNSIGNED NOT NULL DEFAULT 0,
 next_attempt_at BIGINT NOT NULL DEFAULT 0,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 FOREIGN KEY(challenge_id) REFERENCES account_challenges(id),
 INDEX(state,created_at)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS identity_events (
 id BINARY(16) PRIMARY KEY,
 user_id BINARY(16) NULL,
 action VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
 target_id BINARY(16) NOT NULL,
 occurred_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 FOREIGN KEY(user_id) REFERENCES users(id),
 INDEX(user_id,occurred_at,id)
) ENGINE=InnoDB;
