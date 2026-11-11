-- Tenant-scoped role rules; reserved owner administration is not delegatable.
CREATE TABLE IF NOT EXISTS role_policies (
 tenant_id BINARY(16) NOT NULL,
 role_id BINARY(16) NOT NULL,
 permission VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
 effect ENUM('allow','deny') NOT NULL,
 PRIMARY KEY(tenant_id,role_id,permission,effect),
 CHECK(permission IN ('members.read','members.write','organizations.read','organizations.write','invitations.manage','audit.read')),
 FOREIGN KEY(tenant_id,role_id) REFERENCES roles(tenant_id,id)
) ENGINE=InnoDB;
-- Historical binding metadata survives role deletion. role_id is a historical
-- reference; composite membership FK still enforces the tenant boundary.
CREATE TABLE IF NOT EXISTS role_binding_changes (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 membership_id BINARY(16) NOT NULL,
 role_id BINARY(16) NOT NULL,
 role_code VARCHAR(63) COLLATE utf8mb4_bin NOT NULL,
 operation ENUM('bind','unbind') NOT NULL,
 FOREIGN KEY(tenant_id,membership_id) REFERENCES memberships(tenant_id,id)
) ENGINE=InnoDB;
