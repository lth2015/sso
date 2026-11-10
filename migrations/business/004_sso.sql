CREATE TABLE IF NOT EXISTS applications (
 id BINARY(16) PRIMARY KEY,
 code VARCHAR(64) COLLATE utf8mb4_bin NOT NULL UNIQUE,
 name VARCHAR(128) NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 security_version BIGINT UNSIGNED NOT NULL DEFAULT 1
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS tenant_applications (
 tenant_id BINARY(16) NOT NULL,
 application_id BINARY(16) NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 security_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,application_id),
 FOREIGN KEY(tenant_id) REFERENCES tenants(id),
 FOREIGN KEY(application_id) REFERENCES applications(id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS oauth_clients (
 client_id VARCHAR(128) COLLATE utf8mb4_bin PRIMARY KEY,
 application_id BINARY(16) NOT NULL,
 redirect_uri VARCHAR(255) NOT NULL,
 scopes VARCHAR(1024) NOT NULL,
 audience VARCHAR(255) NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 security_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 FOREIGN KEY(application_id) REFERENCES applications(id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS mfa_factors (
 user_id BINARY(16) PRIMARY KEY,
 encrypted_secret BLOB NOT NULL,
 last_step BIGINT NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 security_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 FOREIGN KEY(user_id) REFERENCES users(id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS mfa_recovery_codes (
 user_id BINARY(16) NOT NULL,
 digest BINARY(32) NOT NULL,
 used_at DATETIME(6) NULL,
 PRIMARY KEY(user_id,digest),
 FOREIGN KEY(user_id) REFERENCES mfa_factors(user_id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS sso_sessions (
 id BINARY(16) PRIMARY KEY,
 token_digest BINARY(32) NOT NULL UNIQUE,
 logout_csrf_digest BINARY(32) NULL,
 user_id BINARY(16) NOT NULL,
 user_version BIGINT UNSIGNED NOT NULL,
 mfa_version BIGINT UNSIGNED NOT NULL,
 auth_time BIGINT NOT NULL,
 amr VARCHAR(32) NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 idle_expires_at BIGINT NOT NULL,
 absolute_expires_at BIGINT NOT NULL,
 FOREIGN KEY(user_id) REFERENCES users(id),
 INDEX(user_id,active)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS login_interactions (
 id BINARY(16) PRIMARY KEY,
 kind ENUM('login','consent') NOT NULL,
 challenge_digest BINARY(32) NOT NULL,
 encrypted_challenge BLOB NOT NULL,
 browser_digest BINARY(32) NOT NULL,
 csrf_digest BINARY(32) NOT NULL,
 client_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 user_id BINARY(16) NULL,
 user_version BIGINT UNSIGNED NULL,
 session_id BINARY(16) NULL,
 hydra_sid VARCHAR(128) NOT NULL,
 requested_scopes VARCHAR(1024) NOT NULL,
 audience VARCHAR(255) NOT NULL,
 amr VARCHAR(32) NOT NULL DEFAULT '',
 mfa_version BIGINT UNSIGNED NULL,
 authenticated_at BIGINT NULL,
 state ENUM('password','enroll','mfa','passed','consent','submitting','complete','failed') NOT NULL,
 pending_secret BLOB NULL,
 attempts INT UNSIGNED NOT NULL DEFAULT 0,
 expires_at BIGINT NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE(kind,challenge_digest),
 FOREIGN KEY(client_id) REFERENCES oauth_clients(client_id),
 FOREIGN KEY(user_id) REFERENCES users(id),
 FOREIGN KEY(session_id) REFERENCES sso_sessions(id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS business_grants (
 id BINARY(16) PRIMARY KEY,
 interaction_id BINARY(16) NOT NULL UNIQUE,
 tenant_id BINARY(16) NOT NULL,
 membership_id BINARY(16) NOT NULL,
 user_id BINARY(16) NOT NULL,
 session_id BINARY(16) NOT NULL,
 client_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 scopes VARCHAR(1024) NOT NULL,
 audience VARCHAR(255) NOT NULL,
 hydra_sid VARCHAR(128) NOT NULL,
 user_version BIGINT UNSIGNED NOT NULL,
 membership_version BIGINT UNSIGNED NOT NULL,
 tenant_version BIGINT UNSIGNED NOT NULL,
 client_version BIGINT UNSIGNED NOT NULL,
 application_version BIGINT UNSIGNED NOT NULL,
 enabled_version BIGINT UNSIGNED NOT NULL,
 protocol_binding BINARY(32) NULL UNIQUE,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 absolute_expires_at BIGINT NOT NULL,
 FOREIGN KEY(interaction_id) REFERENCES login_interactions(id),
 FOREIGN KEY(tenant_id,membership_id) REFERENCES memberships(tenant_id,id),
 FOREIGN KEY(user_id) REFERENCES users(id),
 FOREIGN KEY(session_id) REFERENCES sso_sessions(id),
 FOREIGN KEY(client_id) REFERENCES oauth_clients(client_id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS bff_transactions (
 state_digest BINARY(32) PRIMARY KEY,
 client_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 browser_digest BINARY(32) NOT NULL,
 encrypted_payload BLOB NOT NULL,
 consumed BOOLEAN NOT NULL DEFAULT FALSE,
 expires_at BIGINT NOT NULL,
 FOREIGN KEY(client_id) REFERENCES oauth_clients(client_id)
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
 FOREIGN KEY(client_id) REFERENCES oauth_clients(client_id),
 FOREIGN KEY(user_id) REFERENCES users(id),
 FOREIGN KEY(grant_id) REFERENCES business_grants(id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS protocol_logout_jobs (
 sid VARCHAR(128) COLLATE utf8mb4_bin PRIMARY KEY,
 user_id BINARY(16) NOT NULL,
 state ENUM('pending','done') NOT NULL DEFAULT 'pending',
 attempts INT UNSIGNED NOT NULL DEFAULT 0,
 next_attempt_at BIGINT NOT NULL DEFAULT 0,
 FOREIGN KEY(user_id) REFERENCES users(id)
) ENGINE=InnoDB;
