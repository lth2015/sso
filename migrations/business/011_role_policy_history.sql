-- Capture only observed future mutations; no inferred legacy backfill.
-- The audit event owns lifecycle; role_id/code are historical references.
CREATE TABLE IF NOT EXISTS role_policy_changes (
 event_id BINARY(16) NOT NULL PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 role_id BINARY(16) NOT NULL,
 role_code VARCHAR(63) COLLATE utf8mb4_bin NOT NULL,
 authz_version BIGINT NOT NULL CHECK(authz_version > 0),
 before_policies JSON NOT NULL,
 after_policies JSON NOT NULL,
 INDEX(tenant_id,role_id,event_id),
 FOREIGN KEY(event_id) REFERENCES audit_events(event_id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id) REFERENCES tenants(id),
 CHECK(JSON_TYPE(before_policies)='ARRAY' AND JSON_LENGTH(before_policies)<=12),
 CHECK(JSON_TYPE(after_policies)='ARRAY' AND JSON_LENGTH(after_policies)<=12)
) ENGINE=InnoDB;
