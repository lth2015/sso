"""Build the reviewed public API contract; --check rejects stale generated files."""
import json,sys
from pathlib import Path
import yaml
root=Path(__file__).resolve().parents[1]
ref=lambda x:{'$ref':'#/components/'+x}
idtype={'type':'string','pattern':'^[a-f0-9]{32}$'}
nullableid={'type':['string','null'],'pattern':'^[a-f0-9]{32}$'}
def obj(properties,required=None):return {'type':'object','additionalProperties':False,'required':list(properties) if required is None else required,'properties':properties}
string={'type':'string'}
version={'type':'integer','minimum':1}
schemas={
 'Problem':obj({'type':{'type':'string','format':'uri'},'title':string,'status':{'type':'integer','minimum':400,'maximum':599},'code':string,'request_id':string}),
 'Me':obj({'user_id':idtype,'tenant_id':idtype,'membership_id':idtype,'session_id':idtype}),
 'Tenant':obj({'id':idtype,'name':string,'slug':{'type':'string','pattern':'^[a-z0-9][a-z0-9-]{0,62}$'},'membership_id':idtype,'owner':{'type':'boolean'},'authz_version':version,'org_version':version,'root_id':idtype}),
 'TenantInput':obj({'slug':{'type':'string','pattern':'^[a-z0-9][a-z0-9-]{0,62}$'},'name':{'type':'string','minLength':1,'maxLength':128,'description':'Nonblank, trimmed, at most 128 UTF-8 bytes.'}}),
 'TenantCreated':obj({'id':idtype,'membership_id':idtype,'root_id':idtype}),
 'Member':obj({'id':idtype,'user_id':idtype,'email':{'type':['string','null'],'format':'email','description':'Verified alias visible only to authorized tenant owners.'},'status':{'enum':['active','invited','suspended','left']},'owner':{'type':'boolean'}}),
 'Organization':obj({'id':idtype,'parent_id':nullableid,'name':string}),
 'OrganizationInput':obj({'parent_id':idtype,'name':{'type':'string','minLength':1,'maxLength':128,'description':'Nonblank, trimmed, at most 128 UTF-8 bytes.'}}),
 'OrganizationCreated':obj({'id':idtype,'org_version':version}),
 'MoveInput':obj({'parent_id':idtype,'dry_run':{'type':'boolean','description':'Required; false executes the move and invalidates old tenant grants.'}}),
 'MoveResult':obj({'subtree_nodes':{'type':'integer','minimum':1},'resulting_depth':{'type':'integer','minimum':0,'maximum':16},'org_version':version,'dry_run':{'type':'boolean'}}),
 'Invitation':obj({'id':idtype,'email':{'type':'string','format':'email'},'expires_at':{'type':'integer'},'state':{'enum':['pending','expired','closed'],'description':'Closed covers accepted or revoked; audit events distinguish the transition.'}}),
 'InvitationInput':obj({'email':{'type':'string','format':'email','maxLength':254}}),
 'InvitationCreated':obj({'id':idtype,'token':{'type':'string','pattern':'^[A-Za-z0-9_-]{43}$','readOnly':True,'description':'One-time bearer invitation returned only here; keep out of logs. No automatic email transport.'},'expires_in':{'type':'integer','const':86400}}),
 'InvitationProof':obj({'token':{'type':'string','pattern':'^[A-Za-z0-9_-]{43}$','writeOnly':True}}),
 'MembershipCreated':obj({'membership_id':idtype}),
 'AuditEvent':obj({'id':idtype,'actor_membership_id':idtype,'action':string,'target_id':idtype,'occurred_at':{'type':'string','format':'date-time'}}),
 'Session':obj({'id':idtype,'current':{'type':'boolean'},'active':{'type':'boolean'},'authenticated_at':{'type':'integer'},'second_factor':{'enum':['otp','recovery']},'idle_expires_at':{'type':'integer'},'absolute_expires_at':{'type':'integer'}}),
 'Empty':obj({}),
}
for item in ['Tenant','Member','Organization','Invitation','AuditEvent','Session']:
 schemas[item+'Page']=obj({'items':{'type':'array','maxItems':100,'items':ref('schemas/'+item)},'next_cursor':nullableid})
parameters={
 'TenantID':{'in':'path','name':'tenant','required':True,'description':'Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.','schema':idtype},
 'Limit':{'in':'query','name':'limit','description':'Bounded keyset page size.','schema':{'type':'integer','minimum':1,'maximum':100,'default':50}},
 'After':{'in':'query','name':'after','description':'Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.','schema':idtype},
 'AuthzMatch':{'in':'header','name':'If-Match','required':True,'description':'Latest tenant/members ETag. Missing: 428; stale: 412.','schema':{'type':'string','pattern':'^"authz-[1-9][0-9]*"$'}},
 'OrgMatch':{'in':'header','name':'If-Match','required':True,'description':'Latest organizations ETag. Missing: 428; stale: 412.','schema':{'type':'string','pattern':'^"org-[1-9][0-9]*"$'}},
}
for name in ['member','org','invitation','session']:parameters[name+'ID']={'in':'path','name':name,'required':True,'schema':idtype}
headers={'RequestID':{'description':'Server-generated request correlation ID.','schema':string},'ETag':{'description':'Quoted collection version for If-Match.','schema':string},'Authenticate':{'description':'Bearer challenge, including invalid_token or insufficient_scope.','schema':string},'RetryAfter':{'description':'Seconds before retrying.','schema':{'type':'integer'}}}
responses={'Problem':{'description':'RFC 9457 problem. Fixed code only; no input, token or SQL echoed. Codes include invalid_token, forbidden, insufficient_scope, recent_mfa_required, tenant_mismatch, invalid_request, not_found, already_exists, last_active_owner, root_cannot_move, organization_cycle, version_conflict, precondition_required, invalid_precondition, rate_limited, temporarily_unavailable.','headers':{'X-Request-ID':ref('headers/RequestID'),'WWW-Authenticate':ref('headers/Authenticate'),'Retry-After':ref('headers/RetryAfter')},'content':{'application/problem+json':{'schema':ref('schemas/Problem')}}}}
paths={}
def operation(path,method,opid,summary,out=None,body=None,status=200,owner=False,match=None,paged=False,description=''):
 params=[]
 if '{tenant}' in path:params.append(ref('parameters/TenantID'))
 for name in ['member','org','invitation','session']:
  if '{'+name+'}' in path:params.append(ref('parameters/'+name+'ID'))
 if paged:params += [ref('parameters/Limit'),ref('parameters/After')]
 if match:params.append(ref('parameters/'+match+'Match'))
 success={'description':summary,'headers':{'X-Request-ID':ref('headers/RequestID')}}
 if out:success['content']={'application/json':{'schema':ref('schemas/'+out)}}
 if opid in ['getTenant','listMembers','listOrganizations','createOrganization']:success['headers']['ETag']=ref('headers/ETag')
 if opid=='createTenant':success['headers']['Location']={'description':'Created tenant path; obtain a new tenant-bound authorization before using it.','schema':string}
 op={'operationId':opid,'summary':summary,'description':description or ('Active tenant owner required.' if owner else 'Requires current User, MFA session and tenant-bound grant.'),'tags':['Tenant management' if '{tenant}' in path else 'Identity'],'security':[{'OAuth2':['platform:read' if method=='get' else 'platform:write']}],'parameters':params,'responses':{str(status):success,**{str(code):ref('responses/Problem') for code in [400,401,403,404,409,412,413,415,428,429,503]}}}
 if owner:op['x-authorization']='active_owner'
 if method!='get':op['x-recent-mfa-seconds']=300
 if body:op['requestBody']={'required':True,'content':{'application/json':{'schema':ref('schemas/'+body)}}}
 paths.setdefault(path,{})[method]=op
operation('/v1/me','get','getMe','Get the verified current principal','Me')
operation('/v1/tenants','get','listMyTenants','List active spaces of the current user','TenantPage',paged=True)
operation('/v1/tenants','post','createTenant','Create a space and initial owner atomically','TenantCreated','TenantInput',201,description='Recent MFA required. Maximum 20 active owned spaces. The first-party platform is enabled in the new space; unrelated applications are not enabled. Duplicate slug is 409; no automatic retry or idempotency-key guarantee.')
operation('/v1/tenants/{tenant}','get','getTenant','Get the token-bound space and versions','Tenant')
operation('/v1/tenants/{tenant}/members','get','listMembers','List space memberships','MemberPage',owner=True,paged=True)
for action in ['suspend','restore']:operation('/v1/tenants/{tenant}/members/{member}/'+action,'post',action+'Member',action.title()+' a membership',body='Empty',status=204,owner=True,match='Authz',description='Owner and recent MFA required. Last active owner cannot be suspended. Changes increment membership and tenant versions, invalidating existing tenant grants; restoration never revives old grants.')
operation('/v1/tenants/{tenant}/organizations','get','listOrganizations','List space organization nodes','OrganizationPage',owner=True,paged=True)
operation('/v1/tenants/{tenant}/organizations','post','createOrganization','Create an organization under an existing parent','OrganizationCreated','OrganizationInput',201,True,'Org')
operation('/v1/tenants/{tenant}/organizations/{org}/move','post','moveOrganization','Preview or execute an organization move','MoveResult','MoveInput',owner=True,match='Org',description='Owner and recent MFA required, including preview. Root/cycle/depth checks apply. Preview changes no state. Executing changes organization and tenant authorization versions; obtain a new authorization afterwards.')
operation('/v1/tenants/{tenant}/organizations/{org}/members/{member}','put','assignOrganization','Associate a membership with an organization',body='Empty',status=204,owner=True,match='Authz',description='Association does not grant owner or other roles. Changes tenant authorization version; obtain a new authorization afterwards.')
operation('/v1/tenants/{tenant}/invitations','get','listInvitations','List invitation metadata without bearer secrets','InvitationPage',owner=True,paged=True)
operation('/v1/tenants/{tenant}/invitations','post','createInvitation','Create an email-bound one-time invitation','InvitationCreated','InvitationInput',201,True,description='Owner and recent MFA required. Token is returned once, stored only as a digest and never in audit/outbox. The caller delivers it securely; no external email is sent. Invitations grant membership only.')
operation('/v1/tenants/{tenant}/invitations/{invitation}/revoke','post','revokeInvitation','Close an invitation so its proof cannot be accepted',body='Empty',status=204,owner=True,description='Owner and recent MFA required. Idempotently closes the proof; already closed proofs stay closed. Audit records the first revocation; closed includes accepted or revoked.')
operation('/v1/invitations/accept','post','acceptInvitation','Consume an invitation for the verified user','MembershipCreated','InvitationProof',201,description='Recent MFA required. The proof selects the destination tenant. Verified email must match; membership only, no implicit role. Consumed/expired/revoked proofs are invalid. Acceptance increments destination tenant authorization version.')
operation('/v1/tenants/{tenant}/audit-events','get','listAuditEvents','List controlled space audit metadata','AuditEventPage',owner=True,paged=True)
operation('/v1/sessions','get','listMySessions','List central sessions of the current user','SessionPage',paged=True)
operation('/v1/sessions/{session}/revoke','post','revokeMySession','Revoke one central session owned by the current user',body='Empty',status=204,description='Recent MFA required. Revokes business authority before queued Hydra cleanup. Revoking the current session invalidates this access token. Session cookie digests, CSRF values, factor secrets and tokens are never returned.')
for path,ctype in [('/openapi.yaml','application/yaml'),('/openapi.json','application/json'),('/docs','text/html')]:paths[path]={'get':{'operationId':'get'+path.replace('/','').replace('.','').title(),'summary':'Read public API documentation','security':[],'responses':{'200':{'description':'Public reference; no credentials','content':{ctype:{}}}}}}
paths['/health/ready']={'get':{'operationId':'getReadiness','summary':'Check the business schema','security':[],'responses':{'204':{'description':'Business schema ready'},'503':ref('responses/Problem')}}}
doc={'openapi':'3.1.0','info':{'title':'SSO Public Platform API','version':'0.5.0','description':'External server/native-client contract. Local reference server only; production requires TLS and configured public origins. OAuth code+S256 PKCE, platform audience https://api.sso.local. No cookie authentication or wildcard CORS; browser integrations use a BFF. All writes require MFA authenticated within 300 seconds; refresh does not update auth_time. Active owners manage tenant resources; this slice does not implement arbitrary scoped RBAC. Authorization is checked against the business primary inside each operation transaction. Tenant-version mutations invalidate prior tenant grants, requiring new authorization. JSON objects reject duplicates, unknown fields, nested objects/arrays, invalid UTF-8 and extra documents. Request body limit 4096 bytes. UUIDs are lowercase 32-hex strings. List limit 1–100, default 50; after is previous next_cursor. No pagination snapshot guarantee. Error format follows RFC 9457. All responses no-store. Invitation proofs never appear in lists/audit.'},'servers':[{'url':'http://api.localhost:8083','description':'Local acceptance only; SRE owns production deployment'}],'tags':[{'name':'Identity'},{'name':'Tenant management'}],'paths':paths,'components':{'securitySchemes':{'OAuth2':{'type':'oauth2','flows':{'authorizationCode':{'authorizationUrl':'http://auth.localhost:4464/oauth2/auth','tokenUrl':'http://auth.localhost:4464/oauth2/token','refreshUrl':'http://auth.localhost:4464/oauth2/token','scopes':{'platform:read':'Read identity and authorized space metadata','platform:write':'Modify owned spaces and own sessions; recent MFA required'}}}}},'schemas':schemas,'parameters':parameters,'responses':responses,'headers':headers}}
for path,contents in [(root/'api/openapi.yaml',yaml.safe_dump(doc,allow_unicode=True,sort_keys=False)),(root/'api/openapi.json',json.dumps(doc,ensure_ascii=False,indent=2)+'\n')]:
 if '--check' in sys.argv:
  if path.read_text()!=contents:raise SystemExit('Public API contract is stale')
 else:path.write_text(contents)
print('Public OpenAPI: 18 business operations, reference routes, YAML/JSON synchronized')
