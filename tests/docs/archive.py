"""Verify the exported artifact, not a mocked archive."""
import hashlib
import json
from pathlib import Path
import zipfile

root = Path(__file__).resolve().parents[2]
version = json.loads((root / 'api/openapi.json').read_text())['info']['version']
with zipfile.ZipFile(root / f'.local/exports/sso-documentation-v{version}.zip') as archive:
    names = archive.namelist()
    assert len(names) == len(set(names))
    assert all(not n.startswith('/') and '..' not in Path(n).parts for n in names)
    assert all(not any(p in Path(n).parts for p in ['.local', '.env', 'node_modules', '.git']) for n in names)
    manifest = json.loads(archive.read('MANIFEST.json'))
    assert manifest['version'] == version
    assert manifest['entrypoint'] in names
    assert set(names) == set(manifest['files']) | {'MANIFEST.json', 'README.txt'}
    for name, digest in manifest['files'].items():
        assert hashlib.sha256(archive.read(name)).hexdigest() == digest
    assert 'api/console.openapi.yaml' in names
    assert 'api/openapi.yaml' in names and 'api/openapi.json' in names
    assert 'docs/playground.md' in names and 'docs/handbook.md' in names
print(f'Documentation archive passed: {len(names)} files; all SHA-256 values match; public/BFF contracts and handbook included')
