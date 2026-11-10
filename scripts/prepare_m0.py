"""Generate local-only credentials once, preserving the persistent DB identity."""
import os
import secrets
from pathlib import Path

root = Path(__file__).resolve().parents[1]
local = root / '.local'
local.mkdir(exist_ok=True, mode=0o700)
local.chmod(0o700)
env = local / 'm0.env'
init = local / 'init.sql'
if env.exists() != init.exists():
    raise SystemExit('Incomplete local configuration; restore both files, do not replace persistent credentials')
if not env.exists():
    values = {name: secrets.token_hex(32) for name in (
        'MYSQL_ROOT_PASSWORD', 'HYDRA_DB_PASSWORD', 'HYDRA_SYSTEM_SECRET', 'HOOK_KEY', 'HOOK_DB_PASSWORD')}
    def private_write(path, text):
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'w') as f:
            f.write(text)
    private_write(env, ''.join(f'{k}={v}\n' for k, v in values.items()))
    private_write(init, f"""
CREATE DATABASE hydra CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE USER 'hydra'@'%' IDENTIFIED BY '{values['HYDRA_DB_PASSWORD']}';
GRANT ALL ON hydra.* TO 'hydra'@'%';
CREATE DATABASE sso_m0 CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE USER 'm0_hook'@'%' IDENTIFIED BY '{values['HOOK_DB_PASSWORD']}';
USE sso_m0;
CREATE TABLE policies (
 client_id VARCHAR(128) PRIMARY KEY,
 tenant_id VARCHAR(128) NOT NULL,
 principal_id VARCHAR(128) NOT NULL,
 principal_kind ENUM('user','service') NOT NULL,
 subject_id VARCHAR(128) NOT NULL,
 context_id VARCHAR(128) NOT NULL,
 audience VARCHAR(255) NOT NULL,
 scopes VARCHAR(1024) NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 security_version BIGINT NOT NULL DEFAULT 1,
 expires_at BIGINT NOT NULL
);
CREATE TABLE hook_observations (
 id BIGINT AUTO_INCREMENT PRIMARY KEY,
 client_id VARCHAR(128) NOT NULL,
 grant_type VARCHAR(32) NOT NULL,
 allowed BOOLEAN NOT NULL,
 occurred_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);
GRANT SELECT ON sso_m0.policies TO 'm0_hook'@'%';
GRANT INSERT ON sso_m0.hook_observations TO 'm0_hook'@'%';
""")
# MySQL's unprivileged entrypoint must read this bind-mounted file. The host
# parent stays private (0700); the file is only mounted inside this lab DB.
init.chmod(0o644)
print('Local M0 credentials prepared (values hidden)')
