"""Populate/clean only an exact synthetic owner's empty tenant. No credentials."""
import json,re,secrets,subprocess,sys
from pathlib import Path
root=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(root/'tests/support'))
from fixture_protection import protect_demo

def fail():raise SystemExit('Refusing unowned or unexpected role-choice population; details withheld')
def valid(v):return isinstance(v,str) and re.fullmatch('[a-f0-9]{32}',v)
if len(sys.argv)!=4 or sys.argv[1] not in {'create','cleanup'}:fail()
action,user,tenant=sys.argv[1:]
if not valid(user) or not valid(tenant):fail()
command=['docker','compose','--env-file',str(root/'.local/m0.env'),'-f',str(root/'deploy/local/compose.yml'),'exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --default-character-set=utf8mb4 -uroot -N -B sso_business']
def sql(query):
 r=subprocess.run(command,input=query,text=True,capture_output=True)
 if r.returncode:raise SystemExit('Role-choice population SQL failed; details withheld')
 return r.stdout.strip()
email=sql(f"SELECT a.normalized_email FROM login_aliases a JOIN memberships m ON m.user_id=a.user_id JOIN role_bindings b ON b.tenant_id=m.tenant_id AND b.membership_id=m.id JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE a.user_id=UNHEX('{user}') AND m.tenant_id=UNHEX('{tenant}') AND m.status='active' AND r.code='owner' AND a.verified_at IS NOT NULL;")
if not re.fullmatch(r'browser-[a-f0-9]{32}@example\.invalid',email):fail()
protect_demo(root,[email],[tenant])
if action=='create':
 if sql(f"SELECT COUNT(*) FROM memberships WHERE tenant_id=UNHEX('{tenant}');")!='1' or sql(f"SELECT COUNT(*) FROM roles WHERE tenant_id=UNHEX('{tenant}');")!='1':fail()
 seed=secrets.token_hex(10)
 members=[{'user':secrets.token_hex(16),'id':'f0'+seed+format(i,'010x')} for i in range(105)]
 roles=[{'id':'f1'+seed+format(i,'010x'),'code':'fixture_role_'+format(i,'03d')} for i in range(30)]
 statements=['START TRANSACTION;']
 for m in members:statements += [f"INSERT INTO users(id) VALUES(UNHEX('{m['user']}'));",f"INSERT INTO memberships(id,user_id,tenant_id) VALUES(UNHEX('{m['id']}'),UNHEX('{m['user']}'),UNHEX('{tenant}'));"]
 for r in roles:statements += [f"INSERT INTO roles(id,tenant_id,code) VALUES(UNHEX('{r['id']}'),UNHEX('{tenant}'),'{r['code']}');"]
 statements+=['COMMIT;'];sql('\n'.join(statements));print(json.dumps({'members':members,'roles':roles}));raise SystemExit(0)
try:
 record=json.load(sys.stdin);members=record['members'];roles=record['roles']
 assert len(members)==105 and len(roles)==30
 assert all(valid(m['id']) and valid(m['user']) and m['user']!=user for m in members)
 assert all(valid(r['id']) and re.fullmatch('fixture_role_[0-9]{3}',r['code']) for r in roles)
 assert len({m['id'] for m in members})==105 and len({m['user'] for m in members})==105 and len({r['id'] for r in roles})==30
except (ValueError,KeyError,TypeError,AssertionError):fail()
member_ids=','.join("UNHEX('"+m['id']+"')" for m in members)
role_ids=','.join("UNHEX('"+r['id']+"')" for r in roles)
rows=sql(f"SELECT JSON_OBJECT('id',LOWER(HEX(m.id)),'user',LOWER(HEX(m.user_id)),'count',(SELECT COUNT(*) FROM memberships x WHERE x.user_id=m.user_id),'aliases',(SELECT COUNT(*) FROM login_aliases a WHERE a.user_id=m.user_id),'passwords',(SELECT COUNT(*) FROM password_credentials p WHERE p.user_id=m.user_id)) FROM memberships m WHERE m.tenant_id=UNHEX('{tenant}') AND m.id IN ({member_ids});")
try:
 actual=[json.loads(line) for line in rows.splitlines()]
 assert len(actual)==len(members)
 assert {(m['id'],m['user']) for m in actual}=={(m['id'],m['user']) for m in members}
 assert all(m['count']==1 and m['aliases']==0 and m['passwords']==0 for m in actual)
 rows=sql(f"SELECT JSON_OBJECT('id',LOWER(HEX(id)),'code',code) FROM roles WHERE tenant_id=UNHEX('{tenant}') AND id IN ({role_ids});")
 actual=[json.loads(line) for line in rows.splitlines()]
 assert {(r['id'],r['code']) for r in actual}=={(r['id'],r['code']) for r in roles}
 # A synthetic role must not have become attached to an unrelated member.
 assert sql(f"SELECT COUNT(*) FROM role_bindings WHERE tenant_id=UNHEX('{tenant}') AND role_id IN ({role_ids}) AND membership_id NOT IN ({member_ids});")=='0'
except (ValueError,KeyError,TypeError,AssertionError):fail()
# No credentials/aliases are created or removed; every deletion uses exact IDs.
statements=['START TRANSACTION;']
for m in members:
 for table in ['role_binding_changes','role_bindings','org_memberships']:
  statements+=[f"DELETE FROM {table} WHERE tenant_id=UNHEX('{tenant}') AND membership_id=UNHEX('{m['id']}');"]
 statements += [f"DELETE FROM memberships WHERE id=UNHEX('{m['id']}') AND tenant_id=UNHEX('{tenant}') AND user_id=UNHEX('{m['user']}');",f"DELETE FROM users WHERE id=UNHEX('{m['user']}');"]
for r in roles:
 for table in ['role_policies','role_bindings']:
  statements+=[f"DELETE FROM {table} WHERE tenant_id=UNHEX('{tenant}') AND role_id=UNHEX('{r['id']}');"]
 statements+=[f"DELETE FROM roles WHERE tenant_id=UNHEX('{tenant}') AND id=UNHEX('{r['id']}') AND code='{r['code']}';"]
statements+=['COMMIT;'];sql('\n'.join(statements));print('Exact caller-owned role-choice population removed')
