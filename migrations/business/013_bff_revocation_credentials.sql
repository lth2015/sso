-- Opaque, one-purpose credentials authorize revocation of one immutable grant.
-- Only the digest is stored here; plaintext is encrypted in the BFF session store.
CREATE TABLE IF NOT EXISTS bff_revocation_credentials (
 token_digest BINARY(32) PRIMARY KEY,
 grant_id BINARY(16) NOT NULL,
 client_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 expires_at BIGINT NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX(grant_id),
 INDEX(expires_at),
 FOREIGN KEY(grant_id) REFERENCES business_grants(id)
) ENGINE=InnoDB;
