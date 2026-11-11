-- Machine principals are tenant/application identities, never human sessions.
CREATE TABLE IF NOT EXISTS service_principals (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 application_id BINARY(16) NOT NULL,
 creator_membership_id BINARY(16) NOT NULL,
 name VARCHAR(128) NOT NULL,
 enabled BOOLEAN NOT NULL DEFAULT TRUE,
 security_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE(tenant_id,application_id,id),
 FOREIGN KEY(tenant_id,application_id) REFERENCES managed_applications(tenant_id,application_id),
 FOREIGN KEY(tenant_id,creator_membership_id) REFERENCES memberships(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS machine_credentials (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 application_id BINARY(16) NOT NULL,
 service_principal_id BINARY(16) NOT NULL,
 creator_membership_id BINARY(16) NOT NULL,
 client_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL UNIQUE,
 name VARCHAR(128) NOT NULL,
 state ENUM('pending','active','revoked') NOT NULL DEFAULT 'pending',
 expires_at BIGINT NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 security_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE(tenant_id,application_id,service_principal_id,id),
 INDEX(tenant_id,service_principal_id,state,expires_at),
 FOREIGN KEY(tenant_id,application_id,service_principal_id) REFERENCES service_principals(tenant_id,application_id,id),
 FOREIGN KEY(tenant_id,creator_membership_id) REFERENCES memberships(tenant_id,id)
) ENGINE=InnoDB;
-- No token/proof/SK or secret digest is retained in business tables.
-- Hydra owns its own credential hashing and authentication.
CREATE TABLE IF NOT EXISTS machine_events (
 event_id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 application_id BINARY(16) NOT NULL,
 service_principal_id BINARY(16) NOT NULL,
 credential_id BINARY(16) NOT NULL,
 action VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
 occurred_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX(tenant_id,event_id),
 CHECK(action='token.client_credentials'),
 FOREIGN KEY(tenant_id,application_id,service_principal_id,credential_id) REFERENCES machine_credentials(tenant_id,application_id,service_principal_id,id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS machine_outbox_events (
 event_id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 aggregate_id BINARY(16) NOT NULL,
 aggregate_version BIGINT UNSIGNED NOT NULL,
 type VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
 delivered_at DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 FOREIGN KEY(event_id) REFERENCES machine_events(event_id),
 FOREIGN KEY(tenant_id) REFERENCES tenants(id),
 INDEX(delivered_at,created_at)
) ENGINE=InnoDB;
