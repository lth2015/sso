"""Separate machine protocol/resource realm; never a Console User proxy contract."""
import json,sys
from pathlib import Path
import yaml
root=Path(__file__).resolve().parents[1]
public=json.loads((root/'api/openapi.json').read_text())
idtype={'type':'string','pattern':'^[a-f0-9]{32}$'}
def obj(p):return {'type':'object','additionalProperties':False,'required':list(p),'properties':p}
ref=lambda path:{'$ref':'#/components/'+path}
audience={'type':'string','pattern':'^urn:sso:application:[a-f0-9]{32}$'}
schemas={
 'Problem':public['components']['schemas']['Problem'],
 'MachineIdentity':obj({'principal_type':{'const':'service_principal'},'tenant_id':idtype,'application_id':idtype,'service_principal_id':idtype,'credential_id':idtype,'scope':{'const':'machine:invoke'},'audience':audience}),
 'MachineTokenRequest':obj({'grant_type':{'const':'client_credentials'},'scope':{'const':'machine:invoke'},'audience':audience}),
 'MachineTokenResponse':{'type':'object','required':['access_token','expires_in','token_type','scope'],'properties':{'access_token':{'type':'string','description':'Bearer secret; never log/store in browser. RS256 JWT with machine principal and primary-bound versions, not a User/MFA session.'},'expires_in':{'type':'integer','minimum':0,'maximum':300},'token_type':{'type':'string'},'scope':{'const':'machine:invoke'}}},
 'EngineProblem':{'type':'object','properties':{'error':{'type':'string'},'error_description':{'type':'string'}}},
}
schemas['MachineTokenRequest']['additionalProperties']=True
responses={str(code):{'description':'Machine resource RFC 9457 problem; no upstream token/secret/body echo.','content':{'application/problem+json':{'schema':ref('schemas/Problem')}}} for code in [400,401,403,404,429,503]}
paths={
 '/v1/machine/applications/{application}/identity':{'get':{
  'operationId':'getMachineBusinessIdentity','summary':'Read the verified machine identity for this application resource',
  'description':'Business resource reference, separate from User platform management. Expected audience is constructed from this application path. RS256 issuer/signature/expiry, exactly machine:invoke, service_principal claims and key/principal/application/tenant versions are checked. Primary credential expiry/state and SP/application/tenant enablement are checked for every request; fail closed. An old JWT never revives after SP/application restoration. User tokens and machine tokens for another app are rejected. This route is absent from the Console BFF whitelist and User Playground. This sample does not prove that external consumers doing signature-only JWT checks revoke instantly.',
  'parameters':[{'in':'path','name':'application','required':True,'schema':idtype}],
  'security':[{'MachineOAuth2':['machine:invoke']}],
  'responses':{'200':{'description':'No-store identity metadata; not the token or SK.','content':{'application/json':{'schema':ref('schemas/MachineIdentity')}}},**responses}
 }},
 '/oauth2/token':{'post':{
  'operationId':'exchangeMachineCredential','summary':'Exchange AK/SK for a short machine access token',
  'servers':[{'url':'http://auth.localhost:4464'}],
  'description':'Ory Hydra protocol endpoint, not an RFC 9457 platform operation. HTTP Basic username=access_key and password=one-time secret_key. Client credentials only; no refresh/ID token. Explicit fixed scope and the registered application audience. Tenant/principal come exclusively from primary registry. The private hook refuses pending/orphan/expired/revoked/disabled credentials. Source SK cannot be recovered from GET/reconcile; owner revokes/recreates after creation response loss. Local HTTP acceptance only; production TLS is required.',
  'security':[{'AccessKeyBasic':[]}],
  'requestBody':{'required':True,'content':{'application/x-www-form-urlencoded':{'schema':ref('schemas/MachineTokenRequest')}}},
  'responses':{'200':{'description':'Short bearer response; keep in process memory only.','content':{'application/json':{'schema':ref('schemas/MachineTokenResponse')}}},**{str(code):{'description':'Engine OAuth problem; no platform RFC 9457 guarantee.','content':{'application/json':{'schema':ref('schemas/EngineProblem')}}} for code in [400,401,403,429,503]}}
 }}
}
doc={'openapi':'3.1.0','info':{'title':'SSO Machine Credential Protocol and Business Resource Reference','version':'0.25.0','description':'Separate machine realm. No User Cookie/MFA or platform management permission is inferred. Local acceptance; SRE owns deployment.'},'servers':[{'url':'http://api.localhost:8083'}],'paths':paths,'components':{'schemas':schemas,'securitySchemes':{'AccessKeyBasic':{'type':'http','scheme':'basic'},'MachineOAuth2':{'type':'oauth2','flows':{'clientCredentials':{'tokenUrl':'http://auth.localhost:4464/oauth2/token','scopes':{'machine:invoke':'Invoke the registered application resource only'}}}}}}}
for file,text in [(root/'api/machine.openapi.yaml',yaml.safe_dump(doc,allow_unicode=True,sort_keys=False)),(root/'api/machine.openapi.json',json.dumps(doc,ensure_ascii=False,indent=2)+'\n')]:
 if '--check' in sys.argv:
  if file.read_text()!=text:raise SystemExit('Machine resource contract is stale')
 else:file.write_text(text)
print('Machine contract: one separate business resource plus the engine token endpoint')
