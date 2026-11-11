# 公共业务 API 本地运行与接入

当前提供面向外部服务端/原生客户端的标准 Bearer API 合同和实现。公网部署不在本轮范围，GCP/TLS/正式域名由 SRE 负责。

```sh
cd /workspace/sso
scripts/install-go
scripts/api prepare
scripts/api up
scripts/api test
node tests/api/docs.cjs
```

环境需要 Docker/Compose、Python 3（PyYAML、jsonschema）、Node/Playwright 与 Chromium；Go 和服务依赖由仓库固定。`api prepare` 检查 spec 同步并编译服务/集成测试；`api up` 复用 SSO 启动链，启动独立 platform-api 和 PKCE-only 本地接入 client。服务本地地址为 api.localhost:8083，页面路径 `/docs`；JSON/YAML 合同分别为 `/openapi.json`、`/openapi.yaml`。所有宿主端口仅绑定 loopback；不创建公网预览。

## 接入合同

- [OpenAPI 3.1 YAML](../../api/openapi.yaml)、[JSON](../../api/openapi.json)：公共 Bearer API。
- [账户接口](../../api/accounts.openapi.yaml)：浏览器同源注册/恢复；不是 Bearer 管理 API。
- [可信登录接口](../../api/sso.openapi.yaml)：Hydra 重定向交互；不是 password-to-token API。
- [可信账户安全](../../api/security.openapi.yaml)：恢复码生成/分阶段认证器更换，需同源 cookie/浏览器/CSRF，不接受 Bearer。
- [M0 Hook](../../api/m0-internal.openapi.yaml)：内部合成身份实验，禁止对公网路由。

原生客户端使用 OIDC discovery，issuer 为 `http://auth.localhost:4464/`。本地预置 client `local-platform-cli`，token endpoint authentication 为 none，必须 S256 PKCE，redirect URI 严格为 `http://127.0.0.1:9876/callback`。scope 按需申请 `openid offline_access platform:read platform:write`，业务 audience 为 `https://api.sso.local`。客户端自行实现随机 state、nonce、verifier 和 loopback callback，核对 state 后交换 code；必须验证 OIDC ID token，不可用它代替 access token。测试 harness 实际启动 loopback callback，并经过真实 Hydra 换取访问令牌。

现有验证账户需由可信本地操作者显式启用平台应用：

```sh
scripts/api enable --email YOUR_LOCAL_ADDRESS
```

随后从 OAuth 客户端发起登录，完成密码和 MFA，选择有资格的空间。需要 step-up 时重新授权并请求 `max_age=0`。自有应用支持受控公开 PKCE 登记与独立机器 AK/SK；详见 managed-clients.md 和 machine-credentials.md。正式入口与域名配置由 SRE 负责。

取得平台 access token 后，可按下列格式调用（占位符不是可用凭证，不把实际 token 粘贴到日志/聊天）：

```sh
curl http://127.0.0.1:8083/v1/me \
  -H 'Host: api.localhost:8083' \
  -H 'Authorization: Bearer <ACCESS_TOKEN>'
```

## 能力与权限

| 资源 | 已实现能力 | 资格 |
| --- | --- | --- |
| 身份、空间 | 当前 principal、自己的空间列表、新建空间、当前空间概要 | 当前 User/Session/grant；新建要求最近 MFA |
| 成员 | 列表、停用、恢复、最后 owner 保护 | members.read / members.write；写入需最近 MFA + authz ETag |
| 组织 | 分页列表、创建、移动预览/执行、关联成员 | organizations.read / organizations.write；写入需最近 MFA + org/authz ETag |
| 邀请 | 创建一次性 proof、无 secret 的列表、撤销、验证邮箱后接受 | invitations.manage；接受按当前 User 的已验证邮箱并复查创建者当前资格 |
| 审计 | 分页读取受控事件元数据 | audit.read |
| 会话 | 列出自己的中央会话、撤销自己的单个会话 | 当前 User；撤销需最近 MFA |

读取要 `platform:read`，写入要 `platform:write` 和最近 300 秒 MFA。组织关联不授予角色；邀请码只增加成员资格。租户角色默认拒绝、显式 deny 优先，owner 保留 roles.manage 恢复能力；组织子树/资源范围授权尚未实现。API 不用 cookie 认证，也不开放跨域浏览器直接调用；浏览器应用应经 BFF。

列表 `limit=1..100`，默认 50；将响应 next_cursor 传为下一次 after。组织列表响应 ETag 例如 `"org-4"`，用于创建/移动组织的 If-Match；空间概要/成员列表的 `"authz-2"` 用于成员/组织关联。无 If-Match 为 428，旧版本 412。成员/组织权限变更后旧租户 grant 失效，执行者也应重新授权；refresh 不升级 grant 或 MFA 时间。切换空间也必须新授权，不能改 path/header 复用 token。

邀请创建响应的 token 只提供一次，列表不返回；调用者安全转交。撤销/接受后都显示 closed，审计区分动作；请求丢失时不承诺可找回 secret。不自动发送外部邮件。创建空间没有通用幂等键，避免盲目重试；duplicate slug 返回 409。

## 验收与排错

`api test` 注册两个独立合成账户，通过真实原生 OAuth code+PKCE、密码/MFA 后访问 API；实际成功请求和响应由 OpenAPI JSON Schema 校验，原有 18 个业务操作均须覆盖；另运行 `scripts/console rbac-test` 覆盖新增 8 个操作及真实角色 UI。反例包含 ID token/非法 token/cookie 旁路、scope、错租户、非 owner、非法 JSON/媒体类型/body、最近 MFA、并发 If-Match、邀请重放/错误邮箱/撤销、成员恢复旧授权拒绝、会话归属和审计。专用 Go 检查在验签后递增 fixture User 版本，证明写事务阻止旧证明；也检查事务内 token 到期。

浏览器测试仅把 token/proof 保存在测试进程内，失败只打印阶段/类别；不会录制秘密页面。共享 fixture 清理要求租户全部成员都属于本轮合成账户，按叶到根删除组织，并清理自己的私有通知；拒绝清理包含其他账户的租户。`docs.cjs` 检查实际参考页的 54 项操作、桌面/手机布局、无凭证存储并生成截图。

既有回归依次运行 `scripts/m0 test`、`scripts/m1 test`、`scripts/m2 test`、`scripts/sso test`、`scripts/sso network-test`、两个账户/SSO 浏览器脚本。普通 Go 测试明确跳过真实 fixture 案例，不能替代 API 浏览器集成测试。命令会短暂停止自己的通知 worker，不能与浏览器通知流程并发执行。

用 `scripts/sso down` 停止全部本地服务，保留 volume/私有 key。不要修改既有迁移校验和、输出完整 env/Compose/容器 inspect、token/proof、MFA 材料或 SQL 输入。详见 [ADR 0006](../adr/0006-public-platform-api.md)。


公共 API 0.15.0 共 54 项业务操作，成员 email 由 members.read 授权（无 verified alias 时 null）。新增真实 React 控制台见 [控制台手册](console.md)，浏览器边界合同为 `api/console.openapi.yaml`；该合同要求对应 cookie/context/CSRF，不替代公共 Bearer 接入合同。


## 角色 API

`GET /v1/tenants/{tenant}/permissions` 返回当前成员的有效权限、显式拒绝、可委托目录和 authz_version，任何有效当前成员可读取自己的结果。`/roles` 提供 GET/POST，`/roles/{role}` 提供 PUT/DELETE；`/members/{member}/roles` 提供 GET，`/members/{member}/roles/{role}` 提供 PUT/DELETE。这七个管理操作仅 active owner 可用。

创建使用 `{"code":"auditor","policies":[{"permission":"audit.read","effect":"allow"}]}`；更新使用完整 policies 数组，删除/绑定/解绑提交 `{}`。均需 JSON、最近 MFA 和 authz If-Match。角色 code 不可更名，owner 不可修改/删除；带绑定角色返回 409 role_in_use。解绑最后 active owner 返回 409 last_active_owner。真实变更后旧授权失效，重复绑定/解绑及相同策略替换不改变版本。权限目录、恢复边界、并发与历史审计详见 [ADR 0008](../adr/0008-tenant-roles-and-deny.md)。


完整字段和示例见 [API 参考](../api-reference.md)；运行中 `/docs` 展示同一份合同的参数、请求/响应 schema。控制台 [Playground](../playground.md) 使用现有 BFF 发起真实调用，支持写入确认、ETag、MFA/权限错误与凭证脱敏。OpenAPI 0.15.0 提供 54 个业务操作，包括组织生命周期、角色/权限读取 ETag 和写入 JSON 示例；新增同源只读 `/platform-openapi.json` 记录在 BFF 合同。

新增 `GET /v1/me/security-events`：platform:read、有效当前租户 grant，仅本人记录；无需 audit.read。支持 limit/after，按时间与 ID 降序，其他用户/隐藏/未知游标统一 404。仅返回 id/action/occurred_at，完整语义见 [个人安全事件](../security-events.md)。

组织完整管理新增重命名、空节点删除、直属成员分页及移除关联，权限、版本及撤销边界见 [组织管理](../organization-management.md)。
