"""Keep persistent demo data outside ordinary disposable browser fixtures."""
import json,re
from pathlib import Path

def protect_demo(root,emails,tenants=(),reset_run=None,complete=False):
 path=Path(root)/'.local/demo/manifest.json'
 if not path.exists():
  if reset_run:raise SystemExit('Refusing reset without a demo manifest')
  return
 try:
  if path.is_symlink() or not path.is_file():raise ValueError()
  manifest=json.loads(path.read_text())
  if manifest.get('tool')!='scripts/demo' or manifest.get('format')!=1:raise ValueError()
  accounts=manifest['accounts']
  protected={a['email'] for a in accounts}
  if any(not re.fullmatch(r'browser-[a-f0-9]{32}@example\.invalid',e) for e in protected):raise ValueError()
  spaces={manifest[k]['id'] for k in ['primary','secondary'] if k in manifest}
  feature=manifest.get('applicationUserAccess')
  if feature is not None:
   if not isinstance(feature,dict) or feature.get('state') not in {'preparing','ready'}:raise ValueError()
   if 'tenant' in feature:
    if not isinstance(feature['tenant'],dict):raise ValueError()
    spaces.add(feature['tenant']['id'])
   elif feature['state']=='ready':raise ValueError()
  if any(not re.fullmatch('[a-f0-9]{32}',v) for v in spaces):raise ValueError()
 except (OSError,ValueError,KeyError,TypeError):raise SystemExit('Refusing cleanup with an unreadable demo protection manifest')
 if reset_run is not None:
  if not re.fullmatch('[a-f0-9]{32}',reset_run) or reset_run!=manifest.get('runId'):raise SystemExit('Refusing mismatched demo reset context')
  if manifest.get('fingerprint')!=(Path(root)/'.local/sso-key.id').read_text().strip():raise SystemExit('Refusing demo reset in another environment')
  if not set(emails)<=protected or (complete and set(emails)!=protected):raise SystemExit('Refusing incomplete or unrelated demo reset')
  return
 if set(emails)&protected or {str(v).lower() for v in tenants}&spaces:raise SystemExit('Refusing ordinary fixture cleanup of the retained demo; only explicit scripts/demo reset may remove it')
