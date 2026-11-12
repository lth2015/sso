-- User application admission is separate from machine/application enablement.
-- No ALTER or backfill: missing legacy bindings/policies mean version 1/all.
CREATE TABLE IF NOT EXISTS application_user_access_policies (
 tenant_id BINARY(16) NOT NULL,
 application_id BINARY(16) NOT NULL,
 mode ENUM('all_members','selected_members') NOT NULL DEFAULT 'all_members',
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 PRIMARY KEY(tenant_id,application_id),
 FOREIGN KEY(tenant_id,application_id) REFERENCES managed_applications(tenant_id,application_id) ON DELETE CASCADE
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS application_user_access_members (
 tenant_id BINARY(16) NOT NULL,
 application_id BINARY(16) NOT NULL,
 membership_id BINARY(16) NOT NULL,
 PRIMARY KEY(tenant_id,application_id,membership_id),
 FOREIGN KEY(tenant_id,application_id) REFERENCES application_user_access_policies(tenant_id,application_id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,membership_id) REFERENCES memberships(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS application_user_grant_versions (
 grant_id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 application_id BINARY(16) NOT NULL,
 version BIGINT NOT NULL CHECK(version>0),
 FOREIGN KEY(grant_id) REFERENCES business_grants(id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,application_id) REFERENCES tenant_applications(tenant_id,application_id) ON DELETE CASCADE
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS application_user_access_changes (
 event_id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 application_id BINARY(16) NOT NULL,
 membership_id BINARY(16) NULL,
 mode ENUM('all_members','selected_members') NOT NULL,
 version BIGINT NOT NULL CHECK(version>0),
 FOREIGN KEY(event_id) REFERENCES audit_events(event_id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB;
