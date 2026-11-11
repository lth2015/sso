"""Expire one caller-owned synthetic invitation; never select broad fixtures."""
import re
import subprocess
import sys
from pathlib import Path

root = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(root / 'tests/support'))
from fixture_protection import protect_demo

if len(sys.argv) != 4 or sys.argv[1] != 'expire':
    raise SystemExit('Expected bounded invitation fixture action')
user, invitation = sys.argv[2:]
if not all(re.fullmatch('[a-f0-9]{32}', v) for v in [user, invitation]):
    raise SystemExit('Invalid invitation fixture identifiers')
command = ['docker', 'compose', '--env-file', str(root / '.local/m0.env'), '-f',
           str(root / 'deploy/local/compose.yml'), 'exec', '-T', 'mysql', 'sh', '-c',
           'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B sso_business']
query = f"SELECT a.normalized_email,LOWER(HEX(i.tenant_id)) FROM invitations i JOIN memberships m ON m.tenant_id=i.tenant_id AND m.id=i.creator_membership_id JOIN login_aliases a ON a.user_id=m.user_id WHERE i.id=UNHEX('{invitation}') AND m.user_id=UNHEX('{user}') AND i.consumed_at IS NULL AND a.verified_at IS NOT NULL;"
r = subprocess.run(command, input=query.encode(), capture_output=True)
if r.returncode:
    raise SystemExit('Fixture ownership inspection failed; private output withheld')
rows = r.stdout.decode().splitlines()
if len(rows) != 1 or len(rows[0].split('\t')) != 2:
    raise SystemExit('Refusing unknown or closed invitation fixture')
email, tenant = rows[0].split('\t')
if not re.fullmatch(r'browser-[a-f0-9]{32}@example\.invalid', email) or not re.fullmatch('[a-f0-9]{32}', tenant):
    raise SystemExit('Refusing non-synthetic invitation fixture')
protect_demo(root, [email], [tenant])
statement = f"UPDATE invitations i JOIN memberships m ON m.tenant_id=i.tenant_id AND m.id=i.creator_membership_id SET i.expires_at=UNIX_TIMESTAMP()-1 WHERE i.id=UNHEX('{invitation}') AND i.tenant_id=UNHEX('{tenant}') AND m.user_id=UNHEX('{user}') AND i.consumed_at IS NULL; SELECT ROW_COUNT();"
r = subprocess.run(command, input=statement.encode(), capture_output=True)
if r.returncode or r.stdout.decode().strip() != '1':
    raise SystemExit('Bounded invitation expiry failed; private output withheld')
print('Caller-owned synthetic invitation expiry applied')
