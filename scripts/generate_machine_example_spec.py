"""Contract of the independent loopback reference service, not a platform route."""
import json,sys
from pathlib import Path
import yaml
root=Path(__file__).resolve().parents[1]
base=json.loads((root/'api/machine.openapi.json').read_text())
ref=lambda name:{'$ref':'#/components/schemas/'+name}
error={'type':'object','additionalProperties':False,'required':['error'],'properties':{'error':{'enum':['unauthenticated','forbidden','identity_unavailable','invalid_request','not_found','method_not_allowed']}}}
headers={'Cache-Control':{'schema':{'const':'no-store'},'description':'The example never caches authority or identity responses.'}}
responses={str(code):{'description':description,'headers':headers,'content':{'application/json':{'schema':ref('ExampleError')}}} for code,description in {400:'Query/body is not accepted; tenant/application are trusted service configuration.',401:'Missing, malformed, User-realm, expired or revoked machine Bearer.',403:'Authority refused access.',404:'Unknown reference route.',405:'Only GET is accepted.',503:'The online authority could not prove identity. No cached/offline fallback or raw upstream detail.'}.items()}
paths={'/whoami':{'get':{'operationId':'getReferenceMachineBusinessIdentity','summary':'Verify and read the inbound machine identity through the Go SDK','description':'Independent reference business service on loopback, not a User platform API or SSO deployment route. Expected tenant/application come only from trusted service configuration; inbound Cookie and tenant hints grant no identity. The SDK calls the SSO primary-bound identity resource on every request and checks exact namespace/scope. This endpoint returns identity metadata only; actual business permissions remain the business service responsibility. No query or request body. No AK/SK, token cache or automatic business retry.','security':[{'MachineBearer':[]}],'responses':{'200':{'description':'Online verified identity metadata; never the inbound Bearer or a secret.','headers':headers,'content':{'application/json':{'schema':ref('MachineIdentity')}}},**responses}}}}
for suffix,media in [('yaml','application/yaml'),('json','application/json')]:
 paths['/openapi.'+suffix]={'get':{'operationId':'getReferenceMachineContract'+suffix.upper(),'summary':'Read the anonymous reference-service contract','security':[],'responses':{'200':{'description':'Static contract; no live identity data.','headers':headers,'content':{media:{'schema':{'type':'string'}if suffix=='yaml'else{'type':'object'}}}}}}}
doc={'openapi':'3.1.0','info':{'title':'Independent Machine Business Resource Example','version':'0.15.0','description':'One reference business operation, plus anonymous contract metadata. Go SDK integration example; no User BFF or platform-management authority.'},'servers':[{'url':'http://127.0.0.1:8092'}],'paths':paths,'components':{'schemas':{'MachineIdentity':base['components']['schemas']['MachineIdentity'],'ExampleError':error},'securitySchemes':{'MachineBearer':{'type':'http','scheme':'bearer','bearerFormat':'JWT','description':'Obtain the application machine:invoke Bearer using the separate machine credential protocol; this service does not handle AK/SK or User cookies.'}}}}
for name,data in [('api/machine-resource-example.openapi.yaml',yaml.safe_dump(doc,allow_unicode=True,sort_keys=False)),('api/machine-resource-example.openapi.json',json.dumps(doc,ensure_ascii=False,indent=2)+'\n')]:
 path=root/name
 if '--check'in sys.argv:
  if path.read_text()!=data:raise SystemExit('Reference machine service contract is stale')
 else:path.write_text(data)
print('Reference service contract synchronized: one business operation and two anonymous metadata routes')
