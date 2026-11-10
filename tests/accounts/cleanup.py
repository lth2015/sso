"""Remove only the browser test's generated identity and local proof fixtures."""
import re
import subprocess
import sys
from pathlib import Path
root=Path(__file__).resolve().parents[2]
email=sys.argv[1]
if not re.fullmatch(r'browser-[a-f0-9]{32}@example\.invalid',email):
    raise SystemExit('Refusing non-fixture cleanup')
command=['docker','compose','--env-file',str(root/'.local/m0.env'),'-f',str(root/'deploy/local/compose.yml'),'exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B sso_business']
def sql(text):
    result=subprocess.run(command,input=text.encode(),capture_output=True)
    if result.returncode:raise SystemExit('Browser fixture cleanup failed (SQL withheld)')
    return result.stdout.decode().strip()
user=sql(f"SELECT HEX(user_id) FROM login_aliases WHERE normalized_email='{email}';")
sql(f"DELETE n FROM account_notifications n JOIN account_challenges c ON c.id=n.challenge_id WHERE c.normalized_email='{email}'; DELETE e FROM identity_events e JOIN account_challenges c ON c.id=e.target_id WHERE c.normalized_email='{email}'; DELETE FROM account_challenges WHERE normalized_email='{email}';")
if user:
    if not re.fullmatch('[0-9A-F]{32}',user):raise SystemExit('Invalid fixture user ID')
    tenants=sql(f"SELECT HEX(tenant_id) FROM memberships WHERE user_id=UNHEX('{user}');").splitlines()
    for tenant in tenants:
        if not re.fullmatch('[0-9A-F]{32}',tenant):raise SystemExit('Invalid fixture tenant ID')
        for table in ['outbox_events','audit_events','role_bindings','roles','org_closure','tenant_roots','org_nodes','memberships']:
            sql(f"DELETE FROM {table} WHERE tenant_id=UNHEX('{tenant}');")
        sql(f"DELETE FROM tenants WHERE id=UNHEX('{tenant}');")
    for table in ['identity_events','password_credentials','account_profiles','login_aliases']:
        sql(f"DELETE FROM {table} WHERE user_id=UNHEX('{user}');")
    sql(f"DELETE FROM users WHERE id=UNHEX('{user}');")
