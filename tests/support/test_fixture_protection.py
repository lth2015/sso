import json,tempfile,unittest
from pathlib import Path
from fixture_protection import protect_demo

class DemoProtection(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(dir='/tmp');self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name)
  (self.root/'.local/demo').mkdir(parents=True)
  self.run='a'*32;self.email='browser-'+'b'*32+'@example.invalid';self.space='c'*32
  self.data={'format':1,'tool':'scripts/demo','runId':self.run,'fingerprint':'fixture-fingerprint','accounts':[{'email':self.email}],'primary':{'id':self.space}}
  self.path=self.root/'.local/demo/manifest.json';self.path.write_text(json.dumps(self.data));(self.root/'.local/sso-key.id').write_text('fixture-fingerprint\n')
 def test_protected_email_even_if_it_matches_disposable_pattern(self):
  with self.assertRaises(SystemExit):protect_demo(self.root,[self.email])
 def test_protected_tenant_before_any_mutation(self):
  with self.assertRaises(SystemExit):protect_demo(self.root,['browser-'+'d'*32+'@example.invalid'],[self.space.upper()])
 def test_ordinary_unrelated_fixtures_allowed(self):protect_demo(self.root,['browser-'+'d'*32+'@example.invalid'],['e'*32])
 def test_explicit_matching_complete_reset_allowed(self):protect_demo(self.root,[self.email],reset_run=self.run,complete=True)
 def test_wrong_or_partial_reset_refused(self):
  for emails,run in [([self.email],'d'*32),([],self.run),([self.email,'browser-'+'d'*32+'@example.invalid'],self.run)]:
   with self.assertRaises(SystemExit):protect_demo(self.root,emails,reset_run=run,complete=True)
 def test_other_environment_reset_refused(self):
  (self.root/'.local/sso-key.id').write_text('different-fingerprint')
  with self.assertRaises(SystemExit):protect_demo(self.root,[self.email],reset_run=self.run,complete=True)
 def test_unreadable_protection_refuses_all_cleanup(self):
  self.path.write_text('not-json')
  with self.assertRaises(SystemExit):protect_demo(self.root,[])
 def test_missing_manifest_only_allows_ordinary_cleanup(self):
  self.path.unlink();protect_demo(self.root,[])
  with self.assertRaises(SystemExit):protect_demo(self.root,[self.email],reset_run=self.run)

if __name__=='__main__':unittest.main()
