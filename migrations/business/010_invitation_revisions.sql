-- Revision metadata is keyed by the globally unique invitation resource ID.
-- Tenant access always joins the parent invitation under its tenant lock.
-- A missing row means revision 1, preserving all existing invitation rows.
CREATE TABLE IF NOT EXISTS invitation_revisions (
 invitation_id BINARY(16) NOT NULL PRIMARY KEY,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision > 0),
 FOREIGN KEY(invitation_id) REFERENCES invitations(id) ON DELETE CASCADE
) ENGINE=InnoDB;
