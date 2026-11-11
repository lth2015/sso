"""Derive the same-origin browser boundary from the public API contract."""
import copy,json,sys
from pathlib import Path
import yaml
root=Path(__file__).resolve().parents[1]
public=json.loads((root/'api/openapi.json').read_text())
ref=lambda path:{'$ref':'#/components/'+path}
components=copy.deepcopy(public['components'])
components['securitySchemes']={'ConsoleCookie':{'type':'apiKey','in':'header','name':'Cookie','description':'Browser-managed host-only HttpOnly cookie local_console_session_<X-Console-Context>. The context identifier is not a credential. The corresponding cookie is required; no Authorization header is accepted.'}}
components['parameters']['Context']={'in':'header','name':'X-Console-Context','required':True,'description':'Non-secret per-tab authorization context from the callback URL fragment. Requires its corresponding HttpOnly cookie.','schema':{'type':'string','pattern':'^[a-f0-9]{32}$'}}
components['parameters']['CSRF']={'in':'header','name':'X-CSRF-Token','required':True,'description':'Read from GET /session into page memory; bound to this context session.','schema':{'type':'string','pattern':'^[A-Za-z0-9_-]{43}$'}}
components['parameters']['Origin']={'in':'header','name':'Origin','required':True,'schema':{'const':'http://console.localhost:3003'}}
components['schemas']['BFFError']={'type':'object','additionalProperties':False,'required':['code'],'properties':{'code':{'const':'session_unavailable'}}}
components['responses']['Problem']['content']['application/json']={'schema':ref('schemas/BFFError')}
paths={}
for path,operations in public['paths'].items():
 if not path.startswith('/v1/'):continue
 operations=copy.deepcopy(operations)
 for method,operation in operations.items():
  operation['operationId']='console'+operation['operationId'][0].upper()+operation['operationId'][1:]
  operation['security']=[{'ConsoleCookie':[]}]
  operation['parameters'].append(ref('parameters/Context'))
  if method!='get':operation['parameters'] += [ref('parameters/CSRF'),ref('parameters/Origin')]
 paths['/bff'+path]=operations
session={'type':'object','additionalProperties':False,'required':['csrf','application','expires_at'],'properties':{'csrf':{'type':'string','pattern':'^[A-Za-z0-9_-]{43}$'},'application':{'type':'string'},'expires_at':{'type':'string','format':'date-time'}}}
errors={str(n):ref('responses/Problem') for n in [400,401,403,413,415,429,502,503]}
paths['/session']={'get':{'operationId':'getConsoleSession','summary':'Read safe session metadata and CSRF proof; never OAuth tokens','security':[{'ConsoleCookie':[]}],'parameters':[ref('parameters/Context')],'responses':{'200':{'description':'No-store browser session metadata','content':{'application/json':{'schema':session}}},**errors}}}
for action in ['refresh','logout']:
 paths['/session/'+action]={'post':{'operationId':action+'ConsoleSession','summary':('Serialize refresh without changing MFA authentication time' if action=='refresh' else 'Revoke only this context grant/session; other tabs keep their grants'),'security':[{'ConsoleCookie':[]}],'parameters':[ref('parameters/Context'),ref('parameters/CSRF'),ref('parameters/Origin')],'responses':{'204':{'description':'Completed; mutation is not automatically replayed'},**errors}}}
paths['/login']={'get':{'operationId':'startConsoleLogin','summary':'Begin real code+S256 PKCE with state/nonce bound to browser cookie','security':[],'parameters':[{'in':'query','name':'force','schema':{'type':'string','const':'1'},'description':'Request max_age=0 for fresh password/MFA.'},{'in':'query','name':'silent','schema':{'type':'string','const':'1'}}],'responses':{'303':{'description':'Fixed Hydra authorization endpoint'},**errors}}}
paths['/oidc/callback']={'get':{'operationId':'finishConsoleLogin','summary':'Consume state once, exchange code and verify nonce/signature/MFA','security':[],'parameters':[{'in':'query','name':name,'required':name=='state','schema':{'type':'string'}} for name in ['state','code','error']],'responses':{'303':{'description':'Sets context-specific HttpOnly cookie, redirects to /#workspace=<non-secret context ID>. No bearer tokens enter URL or HTML.'},**errors}}}
paths['/bff-openapi.yaml']={'get':{'operationId':'getConsoleContract','security':[],'responses':{'200':{'description':'This same-origin browser API contract','content':{'application/yaml':{}}}}}}
paths['/platform-openapi.json']={'get':{'operationId':'getPlaygroundContract','security':[],'responses':{'200':{'description':'Public platform OpenAPI, same origin for the authenticated Playground; no credentials','content':{'application/json':{}}}}}}
doc={'openapi':'3.1.0','info':{'title':'Identity Console Same-Origin BFF','version':'0.15.0','description':'Browser cookie+context+CSRF boundary. Proxy paths are restricted to the public platform contract and one fixed API destination; caller Authorization is rejected. OAuth tokens and verifier remain encrypted server-side. Every tab uses an independent context cookie and grant. Automatic refresh may occur before a business request; a business mutation is forwarded at most once. API version changes require reauthorization. Cookie limit 20 contexts, oldest context cookie expires when adding another. Local HTTP reference only; production needs TLS/Secure/__Host- policy.'},'servers':[{'url':'http://console.localhost:3003'}],'paths':paths,'components':components}
text=yaml.safe_dump(doc,allow_unicode=True,sort_keys=False)
p=root/'api/console.openapi.yaml'
if '--check' in sys.argv:
 if p.read_text()!=text:raise SystemExit('Console OpenAPI is stale')
else:p.write_text(text)
print('Console OpenAPI synchronized with public operations')
