CREATE TABLE IF NOT EXISTS login_aliases (
 normalized_email VARCHAR(254) COLLATE utf8mb4_bin PRIMARY KEY,
 user_id BINARY(16) NOT NULL,
 verified_at DATETIME(6) NULL,
 FOREIGN KEY(user_id) REFERENCES users(id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS invitations (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 creator_membership_id BINARY(16) NOT NULL,
 target_email VARCHAR(254) COLLATE utf8mb4_bin NOT NULL,
 token_digest BINARY(32) NOT NULL UNIQUE,
 expires_at BIGINT NOT NULL,
 consumed_at DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 FOREIGN KEY(tenant_id,creator_membership_id) REFERENCES memberships(tenant_id,id),
 INDEX(tenant_id,target_email)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS org_memberships (
 tenant_id BINARY(16) NOT NULL,
 org_id BINARY(16) NOT NULL,
 membership_id BINARY(16) NOT NULL,
 PRIMARY KEY(tenant_id,org_id,membership_id),
 FOREIGN KEY(tenant_id,org_id) REFERENCES org_nodes(tenant_id,id),
 FOREIGN KEY(tenant_id,membership_id) REFERENCES memberships(tenant_id,id)
) ENGINE=InnoDB;
