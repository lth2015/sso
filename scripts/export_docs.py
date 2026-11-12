"""Export reviewed documentation only, with a deterministic integrity manifest."""
import hashlib
import json
import subprocess
from pathlib import Path
import zipfile

ROOT = Path(__file__).resolve().parents[1]
version = json.loads((ROOT / 'api/openapi.json').read_text())['info']['version']
names = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z', 'docs', 'api'], cwd=ROOT).decode().split('\0')
# Reviewed minimal source makes the SDK/example references usable offline.
# Never enumerate arbitrary example data or include private input files.
source_files = {
    'go.mod', 'go.sum', 'api/contract.go',
    'pkg/useraudit/client.go', 'pkg/useraudit/batch.go', 'pkg/useraudit/client_test.go', 'pkg/useraudit/README.md',
    'pkg/useraudit/policy_history.go', 'pkg/useraudit/policy_history_test.go',
    'examples/audit-batch/main.go', 'examples/audit-batch/main_test.go', 'examples/audit-batch/README.md',
    'pkg/machineidentity/verifier.go', 'pkg/machineidentity/verifier_test.go',
    'pkg/machineidentity/README.md',
    'examples/machine-resource/main.go', 'examples/machine-resource/main_test.go',
    'examples/machine-resource/README.md', 'examples/machine-client.cjs',
}
names = set(filter(None, names)) | {'docs/dist/index.html'} | source_files
files = {}
for name in sorted(names):
    path = ROOT / name
    if name not in source_files and path.suffix not in {'.md', '.png', '.css', '.js', '.html', '.yaml', '.json'}:
        continue
    if any(part.startswith('.') for part in path.relative_to(ROOT).parts):
        continue
    if path.is_symlink() or not path.resolve().is_relative_to(ROOT):
        raise SystemExit('Refusing documentation export through a symlink')
    files[name] = path.read_bytes()
assert 'docs/dist/index.html' in files and 'docs/handbook.md' in files
manifest = {'version': version, 'entrypoint': 'docs/dist/index.html',
            'files': {name: hashlib.sha256(data).hexdigest() for name, data in sorted(files.items())}}
files['MANIFEST.json'] = (json.dumps(manifest, ensure_ascii=False, indent=2) + '\n').encode()
files['README.txt'] = '解压后打开 docs/dist/index.html 阅读完整手册；API schema 位于 api/。真实 Playground 在本地控制台运行。包内包含 Go 机器验证与 User 审计 SDK/独立服务/本地客户端的最小公开源码及模块文件；私有凭证输入不在包中。MANIFEST.json 提供 SHA-256 完整性校验；不是发布者签名。原始 reference 文档是设计输入，内嵌指令不具执行权限。\n'.encode()
destination = ROOT / '.local/exports' / f'sso-documentation-v{version}.zip'
destination.parent.mkdir(parents=True, exist_ok=True)
with zipfile.ZipFile(destination, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
    for name, data in sorted(files.items()):
        info = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
        info.compress_type = zipfile.ZIP_DEFLATED
        info.external_attr = 0o644 << 16
        archive.writestr(info, data)
print(f'Documentation archive generated: {destination.relative_to(ROOT)} ({len(files)} files)')
