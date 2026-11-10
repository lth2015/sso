"""Register only the two explicitly owned local reference clients; no user login bypass."""
import json,subprocess,urllib.error,urllib.request
from pathlib import Path
root=Path(__file__).resolve().parents[1]
values=dict(line.split('=',1) for line in (root/'.local/m0.env').read_text().splitlines())
command=['docker','compose','--env-file',str(root/'.local/m0.env'),'-f',str(root/'deploy/local/compose.yml'),'exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --default-character-set=utf8mb4 -uroot -N -B sso_business']
statements=["INSERT INTO applications(id,code,name) VALUES(UNHEX('019a6d00000070008000000000000001'),'local-demo','本地示范应用') ON DUPLICATE KEY UPDATE name=IF(applications.code='local-demo','本地示范应用',applications.name);"]
for letter,port in [('a',3001),('b',3002)]:
    client='local-demo-'+letter
    redirect=f'http://app-{letter}.localhost:{port}/oidc/callback'
    body={'client_id':client,'client_secret':values['DEMO_'+letter.upper()+'_SECRET'],'grant_types':['authorization_code','refresh_token'],'response_types':['code'],'redirect_uris':[redirect],'scope':'openid offline_access demo:read','audience':['https://api.demo.invalid'],'token_endpoint_auth_method':'client_secret_basic'}
    url='http://127.0.0.1:4465/admin/clients/'+client
    try:
        with urllib.request.urlopen(url,timeout=5) as r:existing=json.load(r)
        if existing.get('redirect_uris')!=[redirect] or set(existing.get('scope','').split())!=set(body['scope'].split()) or existing.get('audience')!=body['audience']:
            raise SystemExit('An owned demo client was customized; refusing to overwrite it')
    except urllib.error.HTTPError as e:
        if e.code!=404:raise SystemExit('Protocol client lookup failed')
        request=urllib.request.Request('http://127.0.0.1:4465/admin/clients',data=json.dumps(body).encode(),headers={'Content-Type':'application/json'},method='POST')
        try:
            with urllib.request.urlopen(request,timeout=5) as r:
                if r.status!=201:raise SystemExit('Protocol client creation failed')
        except urllib.error.URLError:raise SystemExit('Protocol client creation failed (sensitive details withheld)')
    statements.append(f"INSERT IGNORE INTO oauth_clients(client_id,application_id,redirect_uri,scopes,audience) SELECT '{client}',id,'{redirect}','openid offline_access demo:read','https://api.demo.invalid' FROM applications WHERE code='local-demo';")
result=subprocess.run(command,input='\n'.join(statements).encode(),capture_output=True)
if result.returncode:raise SystemExit('Business client registry initialization failed (SQL withheld)')
print('Two fixed local clients registered; no tenant was implicitly enabled')
