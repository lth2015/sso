"""Remove only a closed set of generated accounts and their isolated test tenants."""
import re,subprocess,sys,json,urllib.request,urllib.error
from pathlib import Path
root=Path(__file__).resolve().parents[2]
emails=sys.argv[1:]
if not emails or any(not re.fullmatch(r'browser-[a-f0-9]{32}@example\.invalid',e) for e in emails):raise SystemExit('Refusing non-fixture cleanup')
command=['docker','compose','--env-file',str(root/'.local/m0.env'),'-f',str(root/'deploy/local/compose.yml'),'exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B sso_business']
def sql(statement):
 p=subprocess.run(command,input=statement.encode(),capture_output=True)
 if p.returncode:
  code=re.search(rb'ERROR (\d+)',p.stderr)
  table=re.search(r'DELETE FROM ([a-z_]+)',statement)
  raise SystemExit('Shared fixture cleanup failed: '+(table[1] if table else 'query')+' / '+(code[1].decode() if code else 'unknown')+' (SQL withheld)')
 return p.stdout.decode().strip()
users=[]
for email in emails:
 user=sql(f"SELECT HEX(user_id) FROM login_aliases WHERE normalized_email='{email}';")
 if user:
  if not re.fullmatch('[0-9A-F]{32}',user):raise SystemExit('Invalid fixture identity')
  users.append(user)
if users:
 ids=','.join("UNHEX('"+u+"')" for u in users)
 tenants=sql(f'SELECT DISTINCT HEX(tenant_id) FROM memberships WHERE user_id IN ({ids});').splitlines()
 for tenant in tenants:
  if not re.fullmatch('[0-9A-F]{32}',tenant):raise SystemExit('Invalid fixture tenant')
  if sql(f"SELECT COUNT(*) FROM memberships WHERE tenant_id=UNHEX('{tenant}') AND user_id NOT IN ({ids});")!='0':raise SystemExit('Refusing cleanup of a tenant containing unrelated accounts')
 for table in ['authenticator_changes','security_actions','protocol_logout_jobs','bff_sessions','business_grants','login_interactions','sso_sessions','mfa_recovery_codes','mfa_factors']:
  sql(f'DELETE FROM {table} WHERE user_id IN ({ids});')
 # Protocol cleanup is limited to clients owned by this closed fixture set.
 managed_apps=[]
 for tenant in tenants:
  managed_apps.extend(sql(f"SELECT HEX(application_id) FROM managed_applications WHERE tenant_id=UNHEX('{tenant}');").splitlines())
 for app in managed_apps:
  if not re.fullmatch('[0-9A-F]{32}',app):raise SystemExit('Invalid managed fixture application')
  linked=sql(f"SELECT HEX(tenant_id) FROM tenant_applications WHERE application_id=UNHEX('{app}');").splitlines()
  if any(t not in tenants for t in linked):raise SystemExit('Refusing application cleanup with unrelated spaces')
 for tenant in tenants:
  for row in sql(f"SELECT LOWER(HEX(application_id)),client_id FROM managed_oauth_clients WHERE tenant_id=UNHEX('{tenant}');").splitlines():
   app,client=row.split('\t')
   if not re.fullmatch('[a-f0-9]{32}',app) or not re.fullmatch('[a-f0-9]{32}',client):raise SystemExit('Invalid managed fixture client')
   url='http://127.0.0.1:4465/admin/clients/'+client
   try:
    with urllib.request.urlopen(url,timeout=5) as response:metadata=json.load(response).get('metadata')
   except urllib.error.HTTPError as error:
    if error.code==404:continue
    raise SystemExit('Managed fixture protocol lookup failed')
   expected={'managed_by':'sso-platform','kind':'public-pkce-v1','tenant_id':tenant.lower(),'application_id':app,'registration_id':client}
   if metadata!=expected:raise SystemExit('Refusing foreign managed fixture protocol cleanup')
   try:
    with urllib.request.urlopen(urllib.request.Request(url,method='DELETE'),timeout=5) as response:assert response.status==204
   except (urllib.error.URLError,AssertionError):raise SystemExit('Managed fixture protocol cleanup failed (details withheld)')
  owned_clients=sql(f"SELECT client_id FROM managed_oauth_clients WHERE tenant_id=UNHEX('{tenant}');").splitlines()
  sql(f"DELETE FROM managed_oauth_clients WHERE tenant_id=UNHEX('{tenant}');")
  for client in owned_clients:sql(f"DELETE FROM oauth_clients WHERE client_id='{client}';")
  sql(f"DELETE FROM managed_applications WHERE tenant_id=UNHEX('{tenant}');")
 for tenant in tenants:
  for table in ['role_binding_changes','role_policies','org_memberships','invitations','outbox_events','audit_events','tenant_applications','role_bindings','roles','org_closure','tenant_roots']:
   sql(f"DELETE FROM {table} WHERE tenant_id=UNHEX('{tenant}');")
  for _ in range(17):
      sql(f"DELETE leaf FROM org_nodes leaf LEFT JOIN org_nodes child ON child.tenant_id=leaf.tenant_id AND child.parent_id=leaf.id WHERE leaf.tenant_id=UNHEX('{tenant}') AND child.id IS NULL;")
  sql(f"DELETE FROM memberships WHERE tenant_id=UNHEX('{tenant}');")
  sql(f"DELETE FROM tenants WHERE id=UNHEX('{tenant}');")
 for app in managed_apps:sql(f"DELETE FROM applications WHERE id=UNHEX('{app}');")
for email in emails:
 subprocess.run([sys.executable,str(root/'tests/accounts/cleanup.py'),email],check=True,capture_output=True)

# Also remove only this closed set's private notification files, even after a
# prior browser failure that lost its in-memory file list.
for file in (root/'.local/mailbox').glob('*.json'):
 if not re.fullmatch('[a-f0-9]{32}.json',file.name) or not file.is_file():continue
 try:data=json.loads(file.read_text())
 except (ValueError,OSError):continue
 if data.get('recipient') in emails:file.unlink()
