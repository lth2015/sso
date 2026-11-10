"""Only fixture-specific auth-time and mutation-race probes, never a login bypass."""
import re,subprocess,sys
from pathlib import Path
root=Path(__file__).resolve().parents[2]
action,user,session=sys.argv[1:]
if not re.fullmatch('[a-f0-9]{32}',user) or not re.fullmatch('[a-f0-9]{32}',session):raise SystemExit('Invalid fixture')
command=['docker','compose','--env-file',str(root/'.local/m0.env'),'-f',str(root/'deploy/local/compose.yml'),'exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B sso_business']
probe=f"SELECT COUNT(*) FROM login_aliases a JOIN sso_sessions s ON s.user_id=a.user_id WHERE a.user_id=UNHEX('{user}') AND s.id=UNHEX('{session}') AND a.normalized_email REGEXP '^browser-[a-f0-9]{{32}}@example[.]invalid$';"
p=subprocess.run(command,input=probe.encode(),capture_output=True)
if p.returncode or p.stdout.decode().strip()!='1':raise SystemExit('Refusing non-fixture mutation')
if action=='age-session':statement=f"UPDATE sso_sessions SET auth_time=UNIX_TIMESTAMP()-301 WHERE id=UNHEX('{session}') AND user_id=UNHEX('{user}');"
else:raise SystemExit('Unknown fixture mutation')
if subprocess.run(command,input=statement.encode(),capture_output=True).returncode:raise SystemExit('Fixture mutation failed')
