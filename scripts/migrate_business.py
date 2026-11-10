"""Checksummed, serialized local migration runner. No schema privilege in the app."""
from pathlib import Path
import hashlib
import subprocess
root=Path(__file__).resolve().parents[1]
command=['docker','compose','--env-file',str(root/'.local/m0.env'),'-f',str(root/'deploy/local/compose.yml'),'exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B sso_business']
# All checks and mutations share one MySQL session and a named advisory lock.
statements=["SET @locked=GET_LOCK('sso_business_migrations',10);", "CREATE TEMPORARY TABLE migration_guard (locked INT NOT NULL CHECK(locked=1)); INSERT INTO migration_guard VALUES(@locked);", "CREATE TABLE IF NOT EXISTS schema_migrations (version VARCHAR(64) PRIMARY KEY, checksum CHAR(64) NOT NULL);"]
for path in sorted((root/'migrations/business').glob('*.sql')):
    version=path.stem
    checksum=hashlib.sha256(path.read_bytes()).hexdigest()
    # Execute idempotent DDL only if no recorded checksum conflicts. DDL can
    # commit individually; an interrupted run resumes IF NOT EXISTS statements.
    statements += [f"INSERT INTO migration_guard SELECT 0 FROM schema_migrations WHERE version='{version}' AND checksum<>'{checksum}';",path.read_text(),f"INSERT IGNORE INTO schema_migrations VALUES('{version}','{checksum}');"]
statements.append("DO RELEASE_LOCK('sso_business_migrations');")
result=subprocess.run(command,input='\n'.join(statements).encode(),capture_output=True)
if result.returncode:
    # Do not echo SQL input or connection configuration.
    raise SystemExit('Business migration failed; inspect schema/constraints locally (SQL and credentials withheld)')
print('Business migrations applied with checksum guard')
