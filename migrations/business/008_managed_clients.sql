-- Tenant ownership is separate from global shared Application enablement.
-- No secret material is stored here. Existing built-in/shared apps stay unmanaged.
CREATE TABLE IF NOT EXISTS managed_applications (
 tenant_id BINARY(16) NOT NULL,
 application_id BINARY(16) NOT NULL,
 creator_membership_id BINARY(16) NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(tenant_id,application_id),
 UNIQUE(application_id),
 FOREIGN KEY(tenant_id,application_id) REFERENCES tenant_applications(tenant_id,application_id),
 FOREIGN KEY(tenant_id,creator_membership_id) REFERENCES memberships(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS managed_oauth_clients (
 tenant_id BINARY(16) NOT NULL,
 application_id BINARY(16) NOT NULL,
 client_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 creator_membership_id BINARY(16) NOT NULL,
 name VARCHAR(128) NOT NULL,
 state ENUM('pending','active','revoked') NOT NULL DEFAULT 'pending',
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(tenant_id,application_id,client_id),
 UNIQUE(client_id),
 FOREIGN KEY(tenant_id,application_id) REFERENCES managed_applications(tenant_id,application_id),
 FOREIGN KEY(client_id) REFERENCES oauth_clients(client_id),
 FOREIGN KEY(tenant_id,creator_membership_id) REFERENCES memberships(tenant_id,id)
) ENGINE=InnoDB;
