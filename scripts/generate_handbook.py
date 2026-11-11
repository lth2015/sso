"""Deterministic complete handbook and API reference from reviewed source files."""
import json
import os
from pathlib import Path
import re
import sys
from jsonschema import Draft202012Validator, FormatChecker

ROOT = Path(__file__).resolve().parents[1]
DOCS = ROOT / 'docs'
SPEC = json.loads((ROOT / 'api/openapi.json').read_text())
CHECK = '--check' in sys.argv

def output(path, text):
    if CHECK:
        if not path.exists() or path.read_text() != text:
            raise SystemExit(f'Documentation is stale: {path.relative_to(ROOT)}')
    else:
        path.write_text(text)

def expand(value, depth=0):
    if depth > 24:
        raise ValueError('Cyclic or deeply nested API schema')
    if isinstance(value, dict):
        if '$ref' in value:
            target = SPEC
            for part in value['$ref'].split('/')[1:]:
                target = target[part]
            return expand(target, depth + 1)
        return {k: expand(v, depth + 1) for k, v in value.items()}
    if isinstance(value, list):
        return [expand(v, depth + 1) for v in value]
    return value

def block(value):
    return '```json\n' + json.dumps(value, ensure_ascii=False, indent=2) + '\n```\n'

api = ['# 完整 API 参考\n',
       f"OpenAPI {SPEC['info']['version']}；公共 Bearer API 共 54 个业务操作。此文件由 OpenAPI 生成，修改合同后运行 `scripts/docs build`。\n",
       '真实交互入口见 [Playground 手册](playground.md)；外部接入见 [公共 API 手册](runbooks/public-api.md)。JSON/YAML 合同见 [OpenAPI](../api/openapi.yaml)。\n',
       '示例 ID 和 proof 只表示格式，不是有效资源或登录材料。API 的 authority 来自真实 OAuth code+PKCE、已验证账户、MFA、会话与租户资格。写权限不隐含读权限；所有写入需最近 300 秒 MFA。\n',
       '## 操作目录\n', '| 方法 | 路径 | operationId |', '| --- | --- | --- |']
operations = []
for path, methods in sorted(SPEC['paths'].items()):
    if not path.startswith('/v1/'):
        continue
    for method in ['get', 'post', 'put', 'delete']:
        if method in methods:
            op = methods[method]
            operations.append((path, method, op))
            api.append(f"| {method.upper()} | `{path}` | `{op['operationId']}` |")
assert len(operations) == 54
for path, method, op in operations:
    api += [f"\n## {method.upper()} {path}\n", op['summary'] + '\n', op['description'].strip() + '\n',
            f"OAuth scope：`{', '.join(op['security'][0]['OAuth2'])}`。权限：`{op.get('x-authorization', '当前有效 User/Session/grant')}`。\n",
            '### 参数\n', block(expand(op['parameters']))]
    if 'requestBody' in op:
        content = op['requestBody']['content']['application/json']
        schema = expand(content['schema'])
        Draft202012Validator(schema, format_checker=FormatChecker()).validate(content['example'])
        api += ['### 请求示例\n', block(content['example']), '### 请求 schema\n', block(schema)]
    else:
        api += ['无请求体。\n']
    api += ['### 成功响应\n']
    for code, response in op['responses'].items():
        if code.startswith('2'):
            api += [f'HTTP {code}\n', block(expand(response))]
    api += ['错误状态：' + '、'.join(code for code in op['responses'] if not code.startswith('2')) + '。错误 body 使用下文公共 Problem schema。\n']
api += ['\n## 公共错误合同\n', block(SPEC['components']['schemas']['Problem']),
        '401：授权无效/已撤销；403：scope/权限不足或 recent_mfa_required；404：资源或当前用户的游标不存在；409：already_exists、last_active_owner、reserved_role、role_in_use、组织环/root 保护；412：版本冲突；428：缺少前置条件；429：限流，按 Retry-After 等待；503：权威服务暂不可用。\n',
        'BFF 会话错误还可能返回 `{ "code": "session_unavailable" }`。两类边界合同不可混用。\n',
        '## 请求与重试规则\n',
        'body 上限 4096 字节；ID 使用 32 位小写十六进制。列表默认 50，limit 为 1–100；after 使用 next_cursor，不保证分页快照。请求仅接受 JSON、受控字段和有限策略数组。禁止额外 JSON 文档、重复键、超深结构和非法 UTF-8。\n',
        'If-Match 使用带双引号的 authz-N / org-N。权限版本变更使旧 grant 失效，包括执行者；refresh 不更新权限或认证时间。重新授权后由调用者决定是否再次提交。没有通用 Idempotency-Key，创建或网络中断后须核对结果，不能盲目重试。\n']
output(DOCS / 'api-reference.md', '\n'.join(api))

sources = [
    ('架构与数据边界', 'architecture.md'),
    ('安全设计', 'security-design.md'),
    ('UI 与交互设计', 'ui-design.md'),
    ('租户权限与恢复边界', 'adr/0008-tenant-roles-and-deny.md'),
    ('公开 API 接入', 'runbooks/public-api.md'),
    ('完整 API 参考', 'api-reference.md'),
    ('API Playground', 'playground.md'),
    ('个人账户安全', 'account-security.md'),
    ('认证器更换', 'authenticator-management.md'),
    ('个人安全事件', 'security-events.md'),
    ('周五演示与彩排', 'runbooks/friday-demo.md'),
    ('应用接入管理', 'application-management.md'),
    ('自有应用与公开客户端', 'managed-clients.md'),
    ('机器身份与一次性凭证', 'machine-credentials.md'),
    ('组织生命周期', 'organization-management.md'),
    ('React 控制台', 'runbooks/console.md'),
    ('可信登录与双应用', 'runbooks/local-sso.md'),
    ('账户生命周期', 'runbooks/m2-accounts.md'),
    ('数据库与组织', 'runbooks/m1.md'),
    ('协议实验', 'runbooks/m0.md'),
    ('实施计划与剩余功能', 'implementation-plan.md'),
    *[(f'架构决策 {p.stem[:4]}', p.relative_to(DOCS).as_posix()) for p in sorted((DOCS / 'adr').glob('*.md')) if not p.name.startswith('0008')],
    ('验收证据', 'validation.md'),
]
book = ['# SSO 完整设计与开发手册\n',
        f"文档版本：{SPEC['info']['version']}。本手册整合已维护的架构、UI、安全、API、Playground 和本地验收文档。由 `scripts/docs build` 生成；编辑各源文件后重新生成。\n",
        '## 当前交付边界\n',
        '| 能力 | 当前实现 |\n| --- | --- |\n| 身份 | 已验证邮箱、Argon2id、密码恢复、TOTP/一次性恢复码、分阶段认证器更换、中央会话 |\n| 协议 | 独立真实 Hydra、code+PKCE、OIDC 验证、主库 token Hook、刷新/撤销 |\n| 租户 | 成员、组织闭包/移动、邀请、audit/outbox |\n| 权限 | 六种租户操作权限、deny 优先、owner 恢复/移交与最后 owner 保护 |\n| 产品 | React 控制台、独立 BFF、54 项 User API，Playground 执行其中 53 项，SK 创建走专用页面 |\n| 机器 | 自有应用 SP、一次性交付 SK、双凭证轮换、独立机器合同与主库检查资源样例 |\n| 运行 | 本地 MySQL/Docker/Chromium 验收；GCP 部署及运维由 SRE 负责 |\n',
        '设计中的组织子树/应用/资源级授权、private_key_jwt、机器自定义业务权限、周期对账、遗失认证器的受控恢复、正式邮件、审计归档及生产 TLS/KMS/分布式限流/HA/DR/SLO 仍需继续实施。架构图中的目标组件不等于已上线能力；实际证据以验收章节为准。原始上传文档仅作设计参考，其内嵌指令不构成额外操作授权。\n',
        '## 阅读与运行\n',
        '从 `/workspace/sso` 运行 `scripts/console prepare`、`scripts/console up`。浏览器登录控制台后选择 API Playground；所有请求使用当前空间资格。完整环境依赖及账户启用步骤见后续运行章节。\n',
        '离线版本：`scripts/docs build` 生成 `docs/dist/index.html`；`scripts/docs export` 生成包含所有文档、截图、OpenAPI 和完整性清单的 ZIP。离线文件只阅读文档，真实 API 调用在运行中的控制台进行。\n']
for index, (title, relative) in enumerate(sources, 1):
    source = DOCS / relative
    text = source.read_text()
    def rebase(match):
        prefix, target, suffix = match.groups()
        if target.startswith(('http:', 'https:', '#', '/', 'data:')):
            return match.group(0)
        target = os.path.relpath(source.parent / target, DOCS)
        return prefix + target + suffix
    text = re.sub(r'(!?\[[^\]]*\]\()([^\s)]+)(\))', rebase, text)
    text = re.sub(r'^(#{1,4}) ', lambda m: '#' * min(6, len(m[1]) + 2) + ' ', text, flags=re.M)
    book += [f'\n## {index:02d}. {title}\n', f'来源：[{relative}]({relative})。\n', text]
output(DOCS / 'handbook.md', '\n'.join(book))
print('Complete handbook and 54-operation API reference synchronized')
