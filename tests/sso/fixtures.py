"""Controlled mutations of only the real browser fixture's business state."""
import re,subprocess,sys
from pathlib import Path
root=Path(__file__).resolve().parents[2]
action,user,*extra=sys.argv[1:]
if not re.fullmatch('[a-f0-9]{32}',user):raise SystemExit('Invalid fixture user')
command=['docker','compose','--env-file',str(root/'.local/m0.env'),'-f',str(root/'deploy/local/compose.yml'),'exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B sso_business']
def sql(statement):
 r=subprocess.run(command,input=statement.encode(),capture_output=True)
 if r.returncode:raise SystemExit('Fixture mutation failed (SQL withheld)')
 return r.stdout.decode().strip()
count=sql(f"SELECT COUNT(*) FROM login_aliases WHERE user_id=UNHEX('{user}') AND normalized_email REGEXP '^browser-[a-f0-9]{{32}}@example[.]invalid$';")
if count!='1':raise SystemExit('Refusing non-fixture user mutation')
if action in ['suspend','restore']:
 if len(extra)!=1 or not re.fullmatch('[a-f0-9]{32}',extra[0]):raise SystemExit('Invalid fixture tenant')
 status='suspended' if action=='suspend' else 'active'
 sql(f"UPDATE memberships SET status='{status}',security_version=security_version+1 WHERE user_id=UNHEX('{user}') AND tenant_id=UNHEX('{extra[0]}');")
else:raise SystemExit('Unknown fixture mutation')
