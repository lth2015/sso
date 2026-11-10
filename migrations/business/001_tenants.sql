-- Business database only; never reads or modifies Hydra tables.
CREATE TABLE IF NOT EXISTS users (
 id BINARY(16) PRIMARY KEY,
 status ENUM('active','suspended','deleted') NOT NULL DEFAULT 'active',
 security_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS tenants (
 id BINARY(16) PRIMARY KEY,
 slug VARCHAR(63) COLLATE utf8mb4_bin NOT NULL UNIQUE,
 status ENUM('active','suspended') NOT NULL DEFAULT 'active',
 authz_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 org_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS memberships (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 user_id BINARY(16) NOT NULL,
 status ENUM('invited','active','suspended','left') NOT NULL DEFAULT 'active',
 security_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,user_id),
 FOREIGN KEY(tenant_id) REFERENCES tenants(id),
 FOREIGN KEY(user_id) REFERENCES users(id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS org_nodes (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 parent_id BINARY(16) NULL,
 name VARCHAR(128) NOT NULL,
 root_marker TINYINT GENERATED ALWAYS AS (IF(parent_id IS NULL,1,NULL)) STORED,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,root_marker), INDEX(tenant_id,parent_id),
 CHECK(parent_id IS NULL OR parent_id<>id),
 FOREIGN KEY(tenant_id) REFERENCES tenants(id),
 FOREIGN KEY(tenant_id,parent_id) REFERENCES org_nodes(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS tenant_roots (
 tenant_id BINARY(16) PRIMARY KEY,
 org_id BINARY(16) NOT NULL,
 FOREIGN KEY(tenant_id) REFERENCES tenants(id),
 FOREIGN KEY(tenant_id,org_id) REFERENCES org_nodes(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS org_closure (
 tenant_id BINARY(16) NOT NULL,
 ancestor_id BINARY(16) NOT NULL,
 descendant_id BINARY(16) NOT NULL,
 depth SMALLINT UNSIGNED NOT NULL,
 PRIMARY KEY(tenant_id,ancestor_id,descendant_id),
 INDEX(tenant_id,descendant_id,depth),
 CHECK((ancestor_id=descendant_id AND depth=0) OR (ancestor_id<>descendant_id AND depth>0)),
 FOREIGN KEY(tenant_id,ancestor_id) REFERENCES org_nodes(tenant_id,id),
 FOREIGN KEY(tenant_id,descendant_id) REFERENCES org_nodes(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS roles (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 code VARCHAR(63) COLLATE utf8mb4_bin NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,code),
 FOREIGN KEY(tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS role_bindings (
 tenant_id BINARY(16) NOT NULL,
 membership_id BINARY(16) NOT NULL,
 role_id BINARY(16) NOT NULL,
 PRIMARY KEY(tenant_id,membership_id,role_id),
 FOREIGN KEY(tenant_id,membership_id) REFERENCES memberships(tenant_id,id),
 FOREIGN KEY(tenant_id,role_id) REFERENCES roles(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS audit_events (
 event_id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 actor_membership_id BINARY(16) NOT NULL,
 action VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
 target_id BINARY(16) NOT NULL,
 occurred_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX(tenant_id,occurred_at,event_id),
 FOREIGN KEY(tenant_id,actor_membership_id) REFERENCES memberships(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS outbox_events (
 event_id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 aggregate_id BINARY(16) NOT NULL,
 aggregate_version BIGINT UNSIGNED NOT NULL,
 type VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
 delivered_at DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX(delivered_at,created_at),
 FOREIGN KEY(event_id) REFERENCES audit_events(event_id),
 FOREIGN KEY(tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB;
