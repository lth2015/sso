"""Inject a bounded profile drift only into a newly generated browser fixture client."""
import json,re,subprocess,sys,urllib.request,urllib.error
from pathlib import Path
root=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(root/'tests/support'))
from fixture_protection import protect_demo
action,client=sys.argv[1:3]
if action not in {'create-drift','restore','compare'} or not re.fullmatch('[a-f0-9]{32}',client):raise SystemExit('Invalid owned managed fixture')
query=f"SELECT JSON_OBJECT('email',la.normalized_email,'tenant',LOWER(HEX(mc.tenant_id)),'application',LOWER(HEX(mc.application_id)),'name',mc.name,'redirect',c.redirect_uri) FROM managed_oauth_clients mc JOIN oauth_clients c ON c.client_id=mc.client_id JOIN memberships m ON m.tenant_id=mc.tenant_id AND m.id=mc.creator_membership_id JOIN login_aliases la ON la.user_id=m.user_id WHERE mc.client_id='{client}' AND mc.state='pending' AND NOT c.active AND la.normalized_email REGEXP '^browser-[a-f0-9]{{32}}@example[.]invalid$';"
p=subprocess.run(['docker','compose','--env-file',str(root/'.local/m0.env'),'-f',str(root/'deploy/local/compose.yml'),'exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --default-character-set=utf8mb4 -uroot -N -B sso_business'],input=query,text=True,capture_output=True)
if p.returncode:raise SystemExit('Owned managed fixture lookup failed')
try:
 row=json.loads(p.stdout.strip())
 for key in ['tenant','application']:
  if not re.fullmatch('[a-f0-9]{32}',row[key]):raise ValueError()
except (ValueError,KeyError):raise SystemExit('Refusing non-fixture managed client')
protect_demo(root,[row['email']],[row['tenant']])
metadata={'managed_by':'sso-platform','kind':'public-pkce-v1','tenant_id':row['tenant'],'application_id':row['application'],'registration_id':client}
body={'client_id':client,'client_name':row['name'],'redirect_uris':[row['redirect']],'grant_types':['authorization_code','refresh_token'],'response_types':['code'],'scope':'openid offline_access platform:read','audience':['https://api.sso.local'],'token_endpoint_auth_method':'none','metadata':metadata}
url='http://127.0.0.1:4465/admin/clients/'+client
try:
 with urllib.request.urlopen(url,timeout=5) as response:existing=json.load(response)
except urllib.error.HTTPError as error:
 if error.code!=404:raise SystemExit('Managed fixture engine lookup failed')
 existing=None
if action=='compare':
 if not existing:raise SystemExit('Owned fixture engine object unavailable')
 diff=[key for key,value in body.items() if existing.get(key)!=value]
 print(json.dumps(diff));raise SystemExit(0)
if action=='create-drift':
 if existing:raise SystemExit('Refusing to replace an existing protocol fixture')
 body['scope']+=' platform:write';method='POST';url='http://127.0.0.1:4465/admin/clients'
else:
 if not existing or existing.get('metadata')!=metadata or existing.get('scope')!='openid offline_access platform:read platform:write':raise SystemExit('Refusing unexpected protocol fixture restoration')
 method='PUT'
try:
 with urllib.request.urlopen(urllib.request.Request(url,data=json.dumps(body).encode(),headers={'Content-Type':'application/json'},method=method),timeout=5) as response:
  if response.status not in {200,201}:raise ValueError()
except (urllib.error.URLError,ValueError):raise SystemExit('Managed fixture mutation failed (private details withheld)')
print('Owned managed-client profile '+('drift injected' if action=='create-drift' else 'restored'))
