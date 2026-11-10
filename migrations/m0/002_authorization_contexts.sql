-- Laboratory authorization bindings; not the production identity schema.
-- Additive and repeatable for databases created by the previous M0 release.
USE sso_m0;
CREATE TABLE IF NOT EXISTS tenant_qualifications (
 client_id VARCHAR(128) NOT NULL,
 tenant_id VARCHAR(128) NOT NULL,
 subject_id VARCHAR(128) NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 security_version BIGINT NOT NULL DEFAULT 1,
 PRIMARY KEY (client_id,tenant_id,subject_id),
 FOREIGN KEY (client_id) REFERENCES policies(client_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS authorization_contexts (
 id VARCHAR(128) PRIMARY KEY,
 client_id VARCHAR(128) NOT NULL,
 tenant_id VARCHAR(128) NOT NULL,
 subject_id VARCHAR(128) NOT NULL,
 principal_id VARCHAR(128) NOT NULL,
 consent_digest BINARY(32) NOT NULL UNIQUE,
 protocol_binding BINARY(32) NULL UNIQUE,
 login_session_id VARCHAR(128) NOT NULL,
 audience VARCHAR(255) NOT NULL,
 scopes VARCHAR(1024) NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 qualification_version BIGINT NOT NULL,
 client_version BIGINT NOT NULL,
 absolute_expires_at BIGINT NOT NULL,
 FOREIGN KEY (client_id,tenant_id,subject_id)
  REFERENCES tenant_qualifications(client_id,tenant_id,subject_id) ON DELETE CASCADE,
 INDEX (client_id,login_session_id)
);
GRANT SELECT ON sso_m0.tenant_qualifications TO 'm0_hook'@'%';
GRANT SELECT ON sso_m0.authorization_contexts TO 'm0_hook'@'%';
GRANT UPDATE (protocol_binding) ON sso_m0.authorization_contexts TO 'm0_hook'@'%';
