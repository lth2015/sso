"""Exact synthetic leaf organizations; root/demo/foreign references are protected."""
import json,re,secrets,subprocess,sys
from pathlib import Path
root=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(root/'tests/support'))
from fixture_protection import protect_demo

def fail():raise SystemExit('Refusing unowned or unexpected assignment population; details withheld')
def valid(v):return isinstance(v,str) and re.fullmatch('[a-f0-9]{32}',v)
if len(sys.argv)!=4 or sys.argv[1] not in {'create','cleanup'}:fail()
action,user,tenant=sys.argv[1:]
if not valid(user) or not valid(tenant):fail()
command=['docker','compose','--env-file',str(root/'.local/m0.env'),'-f',str(root/'deploy/local/compose.yml'),'exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --default-character-set=utf8mb4 -uroot -N -B sso_business']
def sql(query):
 r=subprocess.run(command,input=query,text=True,capture_output=True)
 if r.returncode:raise SystemExit('Assignment population SQL failed; details withheld')
 return r.stdout.strip()
row=sql(f"SELECT JSON_OBJECT('email',a.normalized_email,'root',LOWER(HEX(tr.org_id))) FROM login_aliases a JOIN memberships m ON m.user_id=a.user_id JOIN role_bindings b ON b.tenant_id=m.tenant_id AND b.membership_id=m.id JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id JOIN tenant_roots tr ON tr.tenant_id=m.tenant_id WHERE a.user_id=UNHEX('{user}') AND m.tenant_id=UNHEX('{tenant}') AND m.status='active' AND r.code='owner' AND a.verified_at IS NOT NULL;")
try:
 owned=json.loads(row);assert valid(owned['root']) and re.fullmatch(r'browser-[a-f0-9]{32}@example\.invalid',owned['email'])
except (ValueError,KeyError,TypeError,AssertionError):fail()
protect_demo(root,[owned['email']],[tenant]);root_id=owned['root']
if action=='create':
 if sql(f"SELECT COUNT(*) FROM org_nodes WHERE tenant_id=UNHEX('{tenant}');")!='1':fail()
 seed=secrets.token_hex(10);orgs=[{'id':'f2'+seed+format(i,'010x'),'name':'directory_fixture_'+format(i,'03d')} for i in range(55)]
 statements=['START TRANSACTION;']
 for org in orgs:
  statements += [f"INSERT INTO org_nodes(id,tenant_id,parent_id,name) VALUES(UNHEX('{org['id']}'),UNHEX('{tenant}'),UNHEX('{root_id}'),'{org['name']}');",f"INSERT INTO org_closure(tenant_id,ancestor_id,descendant_id,depth) VALUES(UNHEX('{tenant}'),UNHEX('{root_id}'),UNHEX('{org['id']}'),1),(UNHEX('{tenant}'),UNHEX('{org['id']}'),UNHEX('{org['id']}'),0);"]
 statements+=['COMMIT;'];sql('\n'.join(statements));print(json.dumps({'root':root_id,'orgs':orgs}));raise SystemExit(0)
try:
 record=json.load(sys.stdin);pop=record['population'];orgs=pop['orgs'];members=record['members']
 assert pop['root']==root_id and len(orgs)==55 and len(members)==105
 assert all(valid(o['id']) and o['id']!=root_id and re.fullmatch('directory_fixture_[0-9]{3}(_changed)?',o['name']) for o in orgs)
 assert len({o['id'] for o in orgs})==55 and all(valid(m) for m in members) and len(set(members))==105
except (ValueError,KeyError,TypeError,AssertionError):fail()
ids=','.join("UNHEX('"+o['id']+"')" for o in orgs);member_ids=','.join("UNHEX('"+m+"')" for m in members)
try:
 rows=sql(f"SELECT JSON_OBJECT('id',LOWER(HEX(id)),'name',name,'parent',LOWER(HEX(parent_id))) FROM org_nodes WHERE tenant_id=UNHEX('{tenant}') AND id IN ({ids});")
 actual=[json.loads(line) for line in rows.splitlines()]
 assert len(actual)==55 and {(o['id'],o['name']) for o in actual}=={(o['id'],o['name']) for o in orgs} and all(o['parent']==root_id for o in actual)
 assert sql(f"SELECT COUNT(*) FROM org_nodes WHERE tenant_id=UNHEX('{tenant}') AND parent_id IN ({ids});")=='0'
 assert sql(f"SELECT COUNT(*) FROM org_memberships WHERE tenant_id=UNHEX('{tenant}') AND org_id IN ({ids}) AND membership_id NOT IN ({member_ids});")=='0'
 assert sql(f"SELECT COUNT(*) FROM memberships m LEFT JOIN login_aliases a ON a.user_id=m.user_id LEFT JOIN password_credentials p ON p.user_id=m.user_id WHERE m.tenant_id=UNHEX('{tenant}') AND m.id IN ({member_ids}) AND a.user_id IS NULL AND p.user_id IS NULL;")=='105'
 assert sql(f"SELECT COUNT(*) FROM org_closure WHERE tenant_id=UNHEX('{tenant}') AND (ancestor_id IN ({ids}) OR descendant_id IN ({ids}));")=='110'
 assert sql(f"SELECT COUNT(*) FROM org_closure WHERE tenant_id=UNHEX('{tenant}') AND descendant_id IN ({ids}) AND ((ancestor_id=descendant_id AND depth=0) OR (ancestor_id=UNHEX('{root_id}') AND depth=1));")=='110'
except (ValueError,KeyError,TypeError,AssertionError):fail()
sql(f"START TRANSACTION; DELETE FROM org_memberships WHERE tenant_id=UNHEX('{tenant}') AND org_id IN ({ids}); DELETE FROM org_closure WHERE tenant_id=UNHEX('{tenant}') AND (ancestor_id IN ({ids}) OR descendant_id IN ({ids})); DELETE FROM org_nodes WHERE tenant_id=UNHEX('{tenant}') AND parent_id=UNHEX('{root_id}') AND id IN ({ids}); COMMIT;")
print('Exact caller-owned assignment organization population removed')
