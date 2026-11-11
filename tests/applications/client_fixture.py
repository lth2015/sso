"""Owned public protocol client for real application-disable/code/refresh probes."""
import json,re,subprocess,sys,urllib.request,urllib.error
from pathlib import Path
root=Path(__file__).resolve().parents[2]
action,client=sys.argv[1:3]
if action not in {'create','delete'} or not re.fullmatch(r'application-test-[a-f0-9]{32}',client):raise SystemExit('Invalid owned application test client')
command=['docker','compose','--env-file',str(root/'.local/m0.env'),'-f',str(root/'deploy/local/compose.yml'),'exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B sso_business']
def sql(statement):
 result=subprocess.run(command,input=statement.encode(),capture_output=True)
 if result.returncode:raise SystemExit('Application client fixture SQL failed (details withheld)')
 return result.stdout.decode().strip()
url='http://127.0.0.1:4465/admin/clients/'+client
try:
 with urllib.request.urlopen(url,timeout=5) as response:existing=json.load(response)
except urllib.error.HTTPError as error:
 if error.code!=404:raise SystemExit('Application client fixture protocol lookup failed')
 existing=None
except urllib.error.URLError:raise SystemExit('Application client fixture protocol unavailable')
metadata={'fixture_owner':'tests/applications','fixture_id':client}
if existing and existing.get('metadata')!=metadata:raise SystemExit('Refusing unowned protocol client')
if action=='create':
 app=sys.argv[3]
 if not re.fullmatch('[a-f0-9]{32}',app) or sql(f"SELECT code FROM applications WHERE id=UNHEX('{app}');")!='local-demo':raise SystemExit('Owned local-demo application required')
 body={'client_id':client,'grant_types':['authorization_code','refresh_token'],'response_types':['code'],'redirect_uris':['http://127.0.0.1:9876/callback'],'scope':'openid offline_access platform:read','audience':['https://api.sso.local'],'token_endpoint_auth_method':'none','metadata':metadata}
 if existing and any(existing.get(k)!=v for k,v in body.items()):raise SystemExit('Refusing customized fixture client')
 if not existing:
  request=urllib.request.Request('http://127.0.0.1:4465/admin/clients',data=json.dumps(body).encode(),headers={'Content-Type':'application/json'},method='POST')
  try:
   with urllib.request.urlopen(request,timeout=5) as response:
    if response.status!=201:raise SystemExit('Application fixture client registration failed')
  except urllib.error.URLError:raise SystemExit('Application fixture client registration failed (details withheld)')
 sql(f"INSERT INTO oauth_clients(client_id,application_id,redirect_uri,scopes,audience) VALUES('{client}',UNHEX('{app}'),'http://127.0.0.1:9876/callback','openid offline_access platform:read','https://api.sso.local');")
else:
 sql(f"UPDATE oauth_clients SET security_version=IF(active,security_version+1,security_version),active=FALSE WHERE client_id='{client}';")
 if existing:
  try:
   with urllib.request.urlopen(urllib.request.Request(url,method='DELETE'),timeout=5) as response:
    if response.status!=204:raise SystemExit('Application fixture protocol cleanup failed')
  except urllib.error.URLError:raise SystemExit('Application fixture protocol cleanup failed (details withheld)')
 if sql(f"SELECT COUNT(*) FROM login_interactions WHERE client_id='{client}';")!='0':raise SystemExit('Refusing registry deletion while fixture interactions remain; client disabled')
 sql(f"DELETE FROM oauth_clients WHERE client_id='{client}';")
print('Owned application protocol fixture '+('registered' if action=='create' else 'removed'))
