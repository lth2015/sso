"""Bounded local machine fixtures; no secret arguments, output or production mutation."""
import json,re,subprocess,sys,urllib.request,urllib.error
from pathlib import Path
root=Path(__file__).resolve().parents[2]
action,app,principal,credential=sys.argv[1:]
if action not in {'expire','pending','orphan-create','orphan-delete'} or any(not re.fullmatch('[a-f0-9]{32}',v) for v in [app,principal,credential]):raise SystemExit('Invalid machine fixture')
command=['docker','compose','--env-file',str(root/'.local/m0.env'),'-f',str(root/'deploy/local/compose.yml'),'exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --default-character-set=utf8mb4 -uroot -N -B sso_business']
def sql(query):
 p=subprocess.run(command,input=query,text=True,capture_output=True)
 if p.returncode:raise SystemExit('Machine fixture query failed (details withheld)')
 return p.stdout.strip()
tenant=sql(f"SELECT LOWER(HEX(sp.tenant_id)) FROM service_principals sp JOIN memberships m ON m.tenant_id=sp.tenant_id AND m.id=sp.creator_membership_id JOIN login_aliases la ON la.user_id=m.user_id WHERE sp.application_id=UNHEX('{app}') AND sp.id=UNHEX('{principal}') AND la.normalized_email REGEXP '^browser-[a-f0-9]{{32}}@example[.]invalid$';")
if not re.fullmatch('[a-f0-9]{32}',tenant):raise SystemExit('Refusing non-fixture machine mutation')
where=f"tenant_id=UNHEX('{tenant}') AND application_id=UNHEX('{app}') AND service_principal_id=UNHEX('{principal}') AND id=UNHEX('{credential}')"
if action in {'expire','pending'}:
 if sql('SELECT COUNT(*) FROM machine_credentials WHERE '+where+" AND state='active';")!='1':raise SystemExit('Refusing unexpected machine fixture state')
 update="expires_at=UNIX_TIMESTAMP()-1" if action=='expire' else "state='pending',version=version+1,security_version=security_version+1"
 sql('UPDATE machine_credentials SET '+update+' WHERE '+where+" AND state='active';")
else:
 if sql('SELECT COUNT(*) FROM machine_credentials WHERE '+where)!='0':raise SystemExit('Refusing registered machine orphan fixture')
 client='ak_'+credential
 metadata={'managed_by':'sso-platform','kind':'machine-credential-v1','tenant_id':tenant,'application_id':app,'service_principal_id':principal,'credential_id':credential}
 url='http://127.0.0.1:4465/admin/clients/'+client
 try:
  with urllib.request.urlopen(url,timeout=5) as response:existing=json.load(response)
 except urllib.error.HTTPError as e:
  if e.code!=404:raise SystemExit('Machine fixture engine lookup failed')
  existing=None
 try:
  if action=='orphan-create':
   secret=json.load(sys.stdin).get('secret')
   if not isinstance(secret,str) or not re.fullmatch('[A-Za-z0-9_-]{43}',secret) or existing:raise ValueError()
   profile={'client_id':client,'client_secret':secret,'client_name':'Bounded orphan fixture','grant_types':['client_credentials'],'response_types':[],'redirect_uris':[],'scope':'machine:invoke','audience':['urn:sso:application:'+app],'token_endpoint_auth_method':'client_secret_basic','metadata':metadata}
   request=urllib.request.Request('http://127.0.0.1:4465/admin/clients',data=json.dumps(profile).encode(),headers={'Content-Type':'application/json'},method='POST')
  else:
   if not existing:raise SystemExit(0)
   if existing.get('metadata')!=metadata:raise ValueError()
   request=urllib.request.Request(url,method='DELETE')
  with urllib.request.urlopen(request,timeout=5) as response:
   if response.status not in {200,201,204}:raise ValueError()
 except (urllib.error.URLError,ValueError):raise SystemExit('Machine fixture mutation refused (private details withheld)')
print('Bounded machine fixture completed')
