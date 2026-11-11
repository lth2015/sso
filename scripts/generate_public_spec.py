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
 'Member':obj({'id':idtype,'user_id':idtype,'email':{'type':['string','null'],'format':'email','description':'Verified alias requires members.read on the token tenant.'},'status':{'enum':['active','invited','suspended','left']},'owner':{'type':'boolean'}}),
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
 'SecurityEvent':obj({'id':idtype,'action':{'type':'string','pattern':'^[a-z][a-z0-9_.]{0,63}$','description':'Reviewed security event code; never includes a private target/challenge ID or arbitrary payload.'},'occurred_at':{'type':'string','format':'date-time'}}),
 'Session':obj({'id':idtype,'current':{'type':'boolean'},'active':{'type':'boolean'},'authenticated_at':{'type':'integer'},'second_factor':{'enum':['otp','recovery']},'idle_expires_at':{'type':'integer'},'absolute_expires_at':{'type':'integer'}}),
 'Empty':obj({}),
}
for item in ['Tenant','Member','Organization','Invitation','AuditEvent','Session','SecurityEvent']:
 schemas[item+'Page']=obj({'items':{'type':'array','maxItems':100,'items':ref('schemas/'+item)},'next_cursor':nullableid})
permissions=['audit.read','invitations.manage','members.read','members.write','organizations.read','organizations.write']
schemas['RolePolicy']=obj({'permission':{'enum':permissions},'effect':{'enum':['allow','deny']}})
schemas['RoleInput']=obj({'code':{'type':'string','pattern':'^[a-z][a-z0-9_-]{0,62}$','not':{'const':'owner'}},'policies':{'type':'array','maxItems':12,'uniqueItems':True,'items':ref('schemas/RolePolicy')}})
schemas['RoleUpdate']=obj({'policies':schemas['RoleInput']['properties']['policies']})
schemas['Role']=obj({'id':idtype,'code':string,'built_in':{'type':'boolean'},'scope':{'const':'tenant'},'policies':{'type':'array','items':ref('schemas/RolePolicy')},'binding_count':{'type':'integer','minimum':0}})
schemas['RolePage']=obj({'items':{'type':'array','maxItems':100,'items':ref('schemas/Role')},'next_cursor':nullableid})
schemas['RoleCreated']=obj({'id':idtype,'authz_version':version})
schemas['MemberRoles']=obj({'items':{'type':'array','maxItems':100,'items':ref('schemas/Role')}})
schemas['PermissionSnapshot']=obj({'permissions':{'type':'array','uniqueItems':True,'items':{'enum':permissions+['roles.manage']}},'denied':{'type':'array','uniqueItems':True,'items':{'enum':permissions}},'owner':{'type':'boolean'},'authz_version':version,'catalog':{'type':'array','items':{'enum':permissions}}})
for name in ['role_id','membership_id']:schemas['AuditEvent']['properties'][name]=idtype
schemas['AuditEvent']['properties']['role_code']=string
schemas['SecuritySummary']=obj({'mfa_enabled':{'type':'boolean'},'recovery_codes_remaining':{'type':'integer','minimum':0,'maximum':10},'active_sessions':{'type':'integer','minimum':0},'authenticated_at':{'type':'integer'},'second_factor':{'enum':['otp','recovery']}})
schemas['SessionsRevoked']=obj({'revoked_sessions':{'type':'integer','minimum':0}})
schemas['Application']=obj({'id':idtype,'code':string,'name':string,'enabled':{'type':'boolean','description':'Enabled for this token-bound tenant.'},'available':{'type':'boolean','description':'Global application registry is active; tenant owners cannot override global suspension.'},'protected':{'type':'boolean','description':'Platform management entry cannot be disabled through this API.'},'version':version,'client_count':{'type':'integer','minimum':0}})
schemas['ApplicationPage']=obj({'items':{'type':'array','maxItems':100,'items':ref('schemas/Application')},'next_cursor':nullableid})
schemas['ApplicationClient']=obj({'client_id':{'type':'string','minLength':1,'maxLength':128},'redirect_uri':{'type':'string','format':'uri'},'scopes':{'type':'array','items':string},'audience':{'type':'string','format':'uri'},'active':{'type':'boolean'},'version':version})
schemas['ApplicationClientPage']=obj({'issuer':{'type':'string','format':'uri'},'items':{'type':'array','maxItems':100,'items':ref('schemas/ApplicationClient')},'next_cursor':{'type':['string','null'],'minLength':1,'maxLength':128,'pattern':'^[!-~]{1,128}$'}})

parameters={
 'TenantID':{'in':'path','name':'tenant','required':True,'description':'Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.','schema':idtype},
 'Limit':{'in':'query','name':'limit','description':'Bounded keyset page size.','schema':{'type':'integer','minimum':1,'maximum':100,'default':50}},
 'After':{'in':'query','name':'after','description':'Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.','schema':idtype},
 'ApplicationMatch':{'in':'header','name':'If-Match','required':True,'description':'Exact application detail ETag, bound to tenant/application/version. Missing: 428; wrong resource or stale: 412.','schema':{'type':'string','pattern':'^"app-[a-f0-9]{32}-[a-f0-9]{32}-[1-9][0-9]*"$'}},
 'ClientAfter':{'in':'query','name':'after','description':'Previous client page next_cursor. Ascending printable client_id, not a UUID or pagination snapshot. URL-encode it.','schema':{'type':'string','minLength':1,'maxLength':128,'pattern':'^[!-~]{1,128}$'}},
 'AuthzMatch':{'in':'header','name':'If-Match','required':True,'description':'Latest tenant/members ETag. Missing: 428; stale: 412.','schema':{'type':'string','pattern':'^"authz-[1-9][0-9]*"$'}},
 'OrgMatch':{'in':'header','name':'If-Match','required':True,'description':'Latest organizations ETag. Missing: 428; stale: 412.','schema':{'type':'string','pattern':'^"org-[1-9][0-9]*"$'}},
}
for name in ['member','org','invitation','session','role','application']:parameters[name+'ID']={'in':'path','name':name,'required':True,'schema':idtype}
headers={'RequestID':{'description':'Server-generated request correlation ID.','schema':string},'ETag':{'description':'Quoted collection version for If-Match.','schema':string},'Authenticate':{'description':'Bearer challenge, including invalid_token or insufficient_scope.','schema':string},'RetryAfter':{'description':'Seconds before retrying.','schema':{'type':'integer'}}}
responses={'Problem':{'description':'RFC 9457 problem. Fixed code only; no input, token or SQL echoed. Codes include protected_application, application_unavailable, invalid_token, forbidden, insufficient_scope, recent_mfa_required, tenant_mismatch, invalid_request, not_found, already_exists, reserved_role, role_in_use, last_active_owner, root_cannot_move, organization_cycle, version_conflict, precondition_required, invalid_precondition, rate_limited, temporarily_unavailable.','headers':{'X-Request-ID':ref('headers/RequestID'),'WWW-Authenticate':ref('headers/Authenticate'),'Retry-After':ref('headers/RetryAfter')},'content':{'application/problem+json':{'schema':ref('schemas/Problem')}}}}
paths={}
def operation(path,method,opid,summary,out=None,body=None,status=200,owner=False,match=None,paged=False,description=''):
 params=[]
 if '{tenant}' in path:params.append(ref('parameters/TenantID'))
 for name in ['member','org','invitation','session','role','application']:
  if '{'+name+'}' in path:params.append(ref('parameters/'+name+'ID'))
 if paged:params += [ref('parameters/Limit'),ref('parameters/'+('ClientAfter' if paged=='client' else 'After'))]
 if match:params.append(ref('parameters/'+match+'Match'))
 success={'description':summary,'headers':{'X-Request-ID':ref('headers/RequestID')}}
 if out:success['content']={'application/json':{'schema':ref('schemas/'+out)}}
 if opid in ['getTenant','listMembers','listOrganizations','createOrganization','getMyPermissions','listRoles','listMemberRoles','createRole','getTenantApplication','enableTenantApplication','disableTenantApplication']:success['headers']['ETag']=ref('headers/ETag')
 if opid=='createTenant':success['headers']['Location']={'description':'Created tenant path; obtain a new tenant-bound authorization before using it.','schema':string}
 op={'operationId':opid,'summary':summary,'description':description or ('Active tenant owner required.' if owner else 'Requires current User, MFA session and tenant-bound grant.'),'tags':['Tenant management' if '{tenant}' in path else 'Identity'],'security':[{'OAuth2':['platform:read' if method=='get' else 'platform:write']}],'parameters':params,'responses':{str(status):success,**{str(code):ref('responses/Problem') for code in [400,401,403,404,409,412,413,415,428,429,503]}}}
 if owner:op['x-authorization']='active_owner'
 if method!='get':op['x-recent-mfa-seconds']=300
 if body:op['requestBody']={'required':True,'content':{'application/json':{'schema':ref('schemas/'+body)}}}
 paths.setdefault(path,{})[method]=op
operation('/v1/tenants/{tenant}/applications','get','listTenantApplications','List applications explicitly associated with this space','ApplicationPage',owner=True,paged=True,description='Active owner only. Includes disabled associations for recovery; no global application discovery, implicit enablement, client creation or secret access.')
operation('/v1/tenants/{tenant}/applications/{application}','get','getTenantApplication','Read an application association and resource-bound version','Application',owner=True,description='Active owner only. ETag binds tenant ID, application ID and association version. Global availability and per-tenant enablement are separate. Only existing associations may be managed.')
operation('/v1/tenants/{tenant}/applications/{application}/clients','get','listApplicationClients','Read registered client metadata for an associated application','ApplicationClientPage',owner=True,paged='client',description='Active owner only, including disabled associations. Exact registered client_id, redirect URI, scopes, audience, active flag and version; never client secrets, cookies or token material. Cursor is printable client_id (not UUID), sorted ascending within this application. This registry view does not create clients or change protocol configuration/authentication methods.')
for action in ['enable','disable']:
 operation('/v1/tenants/{tenant}/applications/{application}/'+action,'post',action+'TenantApplication',action.title()+' an existing application association','Application','Empty',owner=True,match='Application',description='Active owner and recent MFA required. Exact tenant/application/version ETag. Atomically changes only this association version, records audit/outbox; disabling revokes this tenant/application grants and BFF sessions. Other spaces, applications and central SSO remain valid; no shared Hydra SID logout. Re-enabling requires new authorization and never resurrects old grants. Same-state requests with current ETag are no-ops. Management platform disable is 409 protected_application; globally suspended application enable is 409 application_unavailable. No registration or global registry mutation.')
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
operation('/v1/me/security-events','get','listMySecurityEvents','List own security events newest first','SecurityEventPage',paged=True,description='Current User only, across spaces; platform:read and a valid current tenant grant required, no tenant audit permission. Returns only reviewed event ID, action and timestamp; excludes internal targets, email, password, MFA proofs, IP and payloads. Order: occurred_at DESC, id DESC. after is the last event ID from this same user; unknown, hidden or another user cursor returns 404. Same-timestamp events use descending ID as a stable tie-breaker. No pagination snapshot, immutable-archive or complete security-monitoring guarantee. Reset-request events record receipt of a request, not proof that the user initiated it.')
operation('/v1/me/security','get','getMySecurity','Read own MFA/recovery/session metadata','SecuritySummary',description='Current verified user only, across their spaces. Counts and authentication metadata only; no factor secret, code digest, cookie or recovery code. Requires an active current tenant grant. Recovery-code regeneration is a separate trusted-origin password+TOTP interaction, never a Bearer API.')
operation('/v1/sessions/revoke-others','post','revokeOtherSessions','Revoke other central sessions, retaining the current session','SessionsRevoked','Empty',description='Recent MFA required. No user ID input: only current user sessions, across their spaces and applications. Revokes other committed session records and their BFF/grant authority, queuing protocol cleanup in the same transaction. Current session/grants remain valid; authentication completing later may create a new session. No-op returns zero. Does not invalidate the current grant or update auth_time.')
operation('/v1/tenants/{tenant}/permissions','get','getMyPermissions','Get the current membership effective permissions','PermissionSnapshot',description='Any current member may inspect their own effective set. Explicit deny wins across tenant-scoped roles. Reserved owner roles.manage is retained for recovery and cannot be expressed in custom policies.')
operation('/v1/tenants/{tenant}/roles','get','listRoles','List tenant roles and rules','RolePage',owner=True,paged=True)
operation('/v1/tenants/{tenant}/roles','post','createRole','Create a tenant-scoped custom role','RoleCreated','RoleInput',201,True,'Authz')
operation('/v1/tenants/{tenant}/roles/{role}','put','replaceRole','Replace all custom role rules',body='RoleUpdate',status=204,owner=True,match='Authz',description='Owner and recent MFA required. Reserved owner is immutable. Duplicate rules and reserved permissions are invalid. Changed policies increment tenant authorization version and invalidate prior tenant grants; identical rules are a no-op.')
operation('/v1/tenants/{tenant}/roles/{role}','delete','deleteRole','Delete an unbound custom role',body='Empty',status=204,owner=True,match='Authz',description='Owner and recent MFA required. Built-in owner cannot be deleted; roles with bindings return role_in_use. Changed state invalidates tenant grants.')
operation('/v1/tenants/{tenant}/members/{member}/roles','get','listMemberRoles','List one tenant membership role bindings','MemberRoles',owner=True)
for method,action in [('put','bind'),('delete','unbind')]:
 operation('/v1/tenants/{tenant}/members/{member}/roles/{role}',method,action+'Role',action.title()+' a tenant role to a membership',body='Empty',status=204,owner=True,match='Authz',description='Owner and recent MFA required. Both objects must belong to the token tenant. Owner grants/revocations are explicit reserved-role operations; last active owner cannot be removed. Repeating the same binding state with the current ETag is a no-op. A real change increments tenant authz_version; old grants must reauthorize. Audit preserves member and role metadata even after role deletion.')
operation_permissions={'listMembers':'members.read','suspendMember':'members.write','restoreMember':'members.write','listOrganizations':'organizations.read','createOrganization':'organizations.write','moveOrganization':'organizations.write','assignOrganization':'organizations.write','listInvitations':'invitations.manage','createInvitation':'invitations.manage','revokeInvitation':'invitations.manage','listAuditEvents':'audit.read'}
for operations in paths.values():
 for op in operations.values():
  permission=operation_permissions.get(op['operationId'])
  if permission:
   op['x-authorization']='permission:'+permission
   op['description']=('Requires '+permission+' on the token tenant. Explicit deny overrides other allows, including owner operational privileges. ')+op['description'].replace('Active tenant owner required.','').replace('Owner and recent MFA required.','Recent MFA required.').replace('Owner and recent MFA required, including preview.','Recent MFA required, including preview.')
  elif op.get('x-authorization')=='active_owner':op['x-authorization']='reserved_owner:roles.manage'
examples={
 'TenantInput':{'slug':'example-team','name':'示例团队'},
 'OrganizationInput':{'parent_id':'0'*32,'name':'示例组织'},
 'MoveInput':{'parent_id':'0'*32,'dry_run':True},
 'InvitationInput':{'email':'teammate@example.invalid'},
 'InvitationProof':{'token':'A'*43},
 'RoleInput':{'code':'auditor','policies':[{'permission':'audit.read','effect':'allow'}]},
 'RoleUpdate':{'policies':[{'permission':'audit.read','effect':'allow'}]},
 'Empty':{},
}
for route in paths.values():
 for op in route.values():
  if 'requestBody' in op:
   content=op['requestBody']['content']['application/json']
   content['example']=examples[content['schema']['$ref'].rsplit('/',1)[1]]
for path,ctype in [('/openapi.yaml','application/yaml'),('/openapi.json','application/json'),('/docs','text/html')]:paths[path]={'get':{'operationId':'get'+path.replace('/','').replace('.','').title(),'summary':'Read public API documentation','security':[],'responses':{'200':{'description':'Public reference; no credentials','content':{ctype:{}}}}}}
paths['/health/ready']={'get':{'operationId':'getReadiness','summary':'Check the business schema','security':[],'responses':{'204':{'description':'Business schema ready'},'503':ref('responses/Problem')}}}
doc={'openapi':'3.1.0','info':{'title':'SSO Public Platform API','version':'0.11.0','description':'External server/native-client contract. Local reference server only; production requires TLS and configured public origins. OAuth code+S256 PKCE, platform audience https://api.sso.local. No cookie authentication or wildcard CORS; browser integrations use a BFF. All writes require MFA authenticated within 300 seconds; refresh does not update auth_time. Tenant-scoped roles grant separate member/organization/invitation/audit permissions; explicit deny wins. Only active owners manage roles and owner bindings; roles.manage cannot be delegated or denied via custom policies. Resource/subtree scopes are not implemented. Delegated member operators cannot suspend/restore owner bindings. Authorization is checked against the business primary inside each operation transaction. Tenant-version mutations invalidate prior tenant grants, requiring new authorization. JSON objects reject duplicate keys, unknown fields, invalid UTF-8 and extra documents. Only role policy requests admit typed nested rules, with depth/work and policy-count limits. Request body limit 4096 bytes. UUIDs are lowercase 32-hex strings. List limit 1–100, default 50; after is previous next_cursor. No pagination snapshot guarantee. Error format follows RFC 9457. All responses no-store. Invitation proofs never appear in lists/audit.'},'servers':[{'url':'http://api.localhost:8083','description':'Local acceptance only; SRE owns production deployment'}],'tags':[{'name':'Identity'},{'name':'Tenant management'}],'paths':paths,'components':{'securitySchemes':{'OAuth2':{'type':'oauth2','flows':{'authorizationCode':{'authorizationUrl':'http://auth.localhost:4464/oauth2/auth','tokenUrl':'http://auth.localhost:4464/oauth2/token','refreshUrl':'http://auth.localhost:4464/oauth2/token','scopes':{'platform:read':'Read identity and authorized space metadata','platform:write':'Modify authorized space resources and own sessions; recent MFA required'}}}}},'schemas':schemas,'parameters':parameters,'responses':responses,'headers':headers}}
for path,contents in [(root/'api/openapi.yaml',yaml.safe_dump(doc,allow_unicode=True,sort_keys=False)),(root/'api/openapi.json',json.dumps(doc,ensure_ascii=False,indent=2)+'\n')]:
 if '--check' in sys.argv:
  if path.read_text()!=contents:raise SystemExit('Public API contract is stale')
 else:path.write_text(contents)
print('Public OpenAPI: 34 business operations, reference routes, YAML/JSON synchronized')
