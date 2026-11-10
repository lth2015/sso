"""Add a private local business DB credential without replacing existing values."""
import os
from pathlib import Path
import secrets
root=Path(__file__).resolve().parents[1]
local=root/'.local'
env=local/'m0.env'
values=dict(line.split('=',1) for line in env.read_text().splitlines())
if 'M1_DB_PASSWORD' not in values:
    values['M1_DB_PASSWORD']=secrets.token_hex(32)
    with env.open('a') as f: f.write('M1_DB_PASSWORD='+values['M1_DB_PASSWORD']+'\n')
password=values['M1_DB_PASSWORD']
if len(password)!=64 or any(c not in '0123456789abcdef' for c in password):
    raise SystemExit('Unsupported local credential encoding; preserve existing configuration')
path=local/'business-init.sql'
# This helper owns only this generated file; values are never emitted to stdout.
fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
with os.fdopen(fd,'w') as f:
    f.write(f"""CREATE DATABASE IF NOT EXISTS sso_business CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE USER IF NOT EXISTS 'business_app'@'%' IDENTIFIED BY '{password}';
GRANT SELECT,INSERT,UPDATE,DELETE ON sso_business.* TO 'business_app'@'%';
""")
print('M1 local credential prepared (value hidden)')
