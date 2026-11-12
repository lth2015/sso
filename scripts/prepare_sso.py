"""Prepare local SSO keys and an isolated Hydra schema, retaining every existing value."""
import hashlib,json,os,secrets,tempfile
from pathlib import Path
root=Path(__file__).resolve().parents[1]
env=root/'.local/m0.env'
values=dict(line.split('=',1) for line in env.read_text().splitlines())
marker=root/'.local/sso-key.id'
previous=json.loads(marker.read_text()) if marker.exists() else {}
names=['SSO_FLOW_KEY','SSO_MFA_KEY','SSO_BFF_KEY','DEMO_A_SECRET','DEMO_B_SECRET','BUSINESS_HYDRA_DB_PASSWORD','BUSINESS_HYDRA_SYSTEM_SECRET','BUSINESS_HOOK_KEY','DEMO_A_DB_PASSWORD','DEMO_B_DB_PASSWORD','DEMO_A_BFF_KEY','DEMO_B_BFF_KEY','CONSOLE_SECRET','CONSOLE_DB_PASSWORD','CONSOLE_BFF_KEY']
fingerprints={}
for name in names:
    if name not in values:
        if name in previous:raise SystemExit('An existing SSO key is missing; restore it rather than regenerating it')
        values[name]=secrets.token_hex(32)
    value=values[name]
    if len(value)!=64 or any(c not in '0123456789abcdef' for c in value):raise SystemExit('Invalid local SSO key encoding')
    fingerprints[name]=hashlib.sha256(bytes.fromhex(value)).hexdigest()
    if name in previous and previous[name]!=fingerprints[name]:raise SystemExit('SSO key changed; explicit migration required')
values['SSO_ENABLED']='true'
def atomic(path,text):
    fd,name=tempfile.mkstemp(prefix='.sso-',dir=path.parent)
    try:
        with os.fdopen(fd,'w') as f:f.write(text);f.flush();os.fsync(f.fileno())
        os.replace(name,path)
    finally:
        if os.path.exists(name):os.unlink(name)
atomic(env,''.join(k+'='+v+'\n' for k,v in values.items()))
atomic(marker,json.dumps(fingerprints,sort_keys=True))
atomic(root/'.local/sso-init.sql',f"CREATE DATABASE IF NOT EXISTS hydra_sso CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;\nCREATE USER IF NOT EXISTS 'hydra_sso'@'%' IDENTIFIED BY '{values['BUSINESS_HYDRA_DB_PASSWORD']}';\nGRANT ALL ON hydra_sso.* TO 'hydra_sso'@'%';\n")
(root/'deploy/local/hydra-sso.yml').chmod(0o644)
print('Local SSO keys and schema initialization prepared (values hidden)')

# Local BFF identities have no credentials/MFA access. Per-client updatable
# views confine their state DML and grant revocation to that client.
statements=[]
readonly={
 'users':'SELECT id,status,security_version FROM sso_business.users',
 'mfa_factors':'SELECT user_id,active,security_version FROM sso_business.mfa_factors',
 'sso_sessions':'SELECT id,user_id,user_version,mfa_version,auth_time,amr,active,idle_expires_at,absolute_expires_at FROM sso_business.sso_sessions',
 'memberships':'SELECT id,tenant_id,user_id,status,security_version FROM sso_business.memberships',
 'tenants':'SELECT id,status,authz_version FROM sso_business.tenants',
 'applications':'SELECT id,active,security_version FROM sso_business.applications',
 'tenant_applications':'SELECT tenant_id,application_id,active,security_version FROM sso_business.tenant_applications',
}
bff_roles=[('local_demo_a_state','demo_a','local-demo-a','DEMO_A_DB_PASSWORD'),('local_demo_b_state','demo_b','local-demo-b','DEMO_B_DB_PASSWORD'),('local_console_state','console_bff','local-console','CONSOLE_DB_PASSWORD')]
for db,user,client,password_name in bff_roles:
 statements += [f"CREATE DATABASE IF NOT EXISTS {db};",f"CREATE USER IF NOT EXISTS '{user}'@'%' IDENTIFIED BY '{values[password_name]}';"]
 definitions=dict(readonly)
 definitions['oauth_clients']=f"SELECT client_id,application_id,active,security_version FROM sso_business.oauth_clients WHERE client_id='{client}'"
 definitions['login_interactions']=f"SELECT id,state FROM sso_business.login_interactions WHERE client_id='{client}'"
 for name,query in definitions.items():
  statements += [f"CREATE OR REPLACE SQL SECURITY DEFINER VIEW {db}.{name} AS {query};",f"GRANT SELECT ON {db}.{name} TO '{user}'@'%';"]
 for table in ['bff_transactions','bff_sessions','business_grants']:
  statements.append(f"CREATE OR REPLACE ALGORITHM=MERGE SQL SECURITY DEFINER VIEW {db}.{table} AS SELECT * FROM sso_business.{table} WHERE client_id='{client}' WITH CASCADED CHECK OPTION;")
  permissions='SELECT,INSERT,UPDATE,DELETE' if table!='business_grants' else 'SELECT,UPDATE(active)'
  statements.append(f"GRANT {permissions} ON {db}.{table} TO '{user}'@'%';")
# Append audit/outbox through a client-bound definer routine. The BFF cannot
# insert arbitrary tenant actors or modify existing audit records.
for db,user,client,password_name in bff_roles:
 scoped={
  'users':f"SELECT u.id,u.status,u.security_version FROM sso_business.users u WHERE EXISTS(SELECT 1 FROM sso_business.business_grants g WHERE g.client_id='{client}' AND g.user_id=u.id)",
  'mfa_factors':f"SELECT f.user_id,f.active,f.security_version FROM sso_business.mfa_factors f WHERE EXISTS(SELECT 1 FROM sso_business.business_grants g WHERE g.client_id='{client}' AND g.user_id=f.user_id)",
  'sso_sessions':f"SELECT s.id,s.user_id,s.user_version,s.mfa_version,s.auth_time,s.amr,s.active,s.idle_expires_at,s.absolute_expires_at FROM sso_business.sso_sessions s WHERE EXISTS(SELECT 1 FROM sso_business.business_grants g WHERE g.client_id='{client}' AND g.session_id=s.id)",
  'memberships':f"SELECT m.id,m.tenant_id,m.user_id,m.status,m.security_version FROM sso_business.memberships m WHERE EXISTS(SELECT 1 FROM sso_business.business_grants g WHERE g.client_id='{client}' AND g.membership_id=m.id AND g.tenant_id=m.tenant_id)",
  'tenants':f"SELECT t.id,t.status,t.authz_version FROM sso_business.tenants t WHERE EXISTS(SELECT 1 FROM sso_business.business_grants g WHERE g.client_id='{client}' AND g.tenant_id=t.id)",
  'applications':f"SELECT a.id,a.active,a.security_version FROM sso_business.applications a JOIN sso_business.oauth_clients c ON c.application_id=a.id WHERE c.client_id='{client}'",
  'tenant_applications':f"SELECT ta.tenant_id,ta.application_id,ta.active,ta.security_version FROM sso_business.tenant_applications ta JOIN sso_business.oauth_clients c ON c.application_id=ta.application_id WHERE c.client_id='{client}' AND EXISTS(SELECT 1 FROM sso_business.business_grants g WHERE g.client_id='{client}' AND g.tenant_id=ta.tenant_id)",
  'application_user_access_policies':f"SELECT p.tenant_id,p.application_id,p.mode,p.version FROM sso_business.application_user_access_policies p JOIN sso_business.oauth_clients c ON c.application_id=p.application_id WHERE c.client_id='{client}' AND EXISTS(SELECT 1 FROM sso_business.business_grants g WHERE g.client_id='{client}' AND g.tenant_id=p.tenant_id)",
  'application_user_grant_versions':f"SELECT v.grant_id,v.tenant_id,v.application_id,v.version FROM sso_business.application_user_grant_versions v JOIN sso_business.business_grants g ON g.id=v.grant_id WHERE g.client_id='{client}'",
  'application_user_access_members':f"SELECT am.tenant_id,am.application_id,am.membership_id FROM sso_business.application_user_access_members am JOIN sso_business.oauth_clients c ON c.application_id=am.application_id WHERE c.client_id='{client}' AND EXISTS(SELECT 1 FROM sso_business.business_grants g WHERE g.client_id='{client}' AND g.tenant_id=am.tenant_id AND g.membership_id=am.membership_id)",
  'role_bindings':f"SELECT b.tenant_id,b.membership_id,b.role_id FROM sso_business.role_bindings b JOIN sso_business.roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE r.code='owner' AND EXISTS(SELECT 1 FROM sso_business.business_grants g WHERE g.client_id='{client}' AND g.tenant_id=b.tenant_id AND g.membership_id=b.membership_id)",
  'roles':f"SELECT r.tenant_id,r.id,r.code FROM sso_business.roles r WHERE r.code='owner' AND EXISTS(SELECT 1 FROM sso_business.role_bindings b JOIN sso_business.business_grants g ON g.tenant_id=b.tenant_id AND g.membership_id=b.membership_id WHERE b.tenant_id=r.tenant_id AND b.role_id=r.id AND g.client_id='{client}')",

 }
 for name,query in scoped.items():
  statements += [f"CREATE OR REPLACE SQL SECURITY DEFINER VIEW {db}.{name} AS {query};",f"GRANT SELECT ON {db}.{name} TO '{user}'@'%';"]
 statements += [f"DROP PROCEDURE IF EXISTS {db}.append_bff_event;",'DELIMITER //',f"""CREATE PROCEDURE {db}.append_bff_event(IN event BINARY(16),IN target BINARY(16),IN action_name VARCHAR(64)) SQL SECURITY DEFINER
 BEGIN
  IF action_name NOT IN ('authorization.logout','authorization.refresh_failed') THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Invalid BFF action'; END IF;
  INSERT INTO sso_business.audit_events(event_id,tenant_id,actor_membership_id,action,target_id)
    SELECT event,g.tenant_id,g.membership_id,action_name,g.id FROM sso_business.business_grants g WHERE g.id=target AND g.client_id='{client}';
  IF ROW_COUNT()<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Invalid BFF grant'; END IF;
  INSERT INTO sso_business.outbox_events(event_id,tenant_id,aggregate_id,aggregate_version,type)
    SELECT event,g.tenant_id,g.id,2,action_name FROM sso_business.business_grants g WHERE g.id=target AND g.client_id='{client}';
 END//""",'DELIMITER ;',f"GRANT EXECUTE ON PROCEDURE {db}.append_bff_event TO '{user}'@'%';"]
atomic(root/'.local/bff-init.sql','\n'.join(statements))
