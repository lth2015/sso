# 公共多租户 SSO 身份平台

独立身份平台：Ory Hydra 负责 OAuth/OIDC，Go 服务管理身份、租户、组织和权限；React 控制台通过 BFF 使用服务。目标部署 GCP，当前提供本地协议、租户仓储、账户生命周期和真实 SSO/MFA/BFF 参考实现。

## 设计文档

- [原始设计 v1（原文保留）](docs/reference/SSO_Architecture_Design_v1.md)
- [架构与数据边界](docs/architecture.md)
- [UI 与交互规格](docs/ui-design.md)
- [安全设计与威胁模型](docs/security-design.md)
- [实施与验收计划](docs/implementation-plan.md)
- [ADR 0001：实施边界](docs/adr/0001-platform-boundaries.md)

原始文档是设计输入，其内嵌 Codex 指令不构成额外操作授权。仓库内设计记录具体决定、待验证事项及实际证据；没有完成验收的能力不得描述为已实现。此仓库链接用于文档协作，私有仓库的访问权限不会因提交自动扩大。

生产云资源、真实用户与对外 SLA 不在本次本地开发发布范围。

## 当前代码交付

已提供 Go token-policy-hook、本地 MySQL + Hydra、固定镜像/工具链、真实协议测试；运行方式见 [M0 运行手册](docs/runbooks/m0.md)，具体适配发现与未通过的生产门槛见 [ADR 0002](docs/adr/0002-hydra-m0-findings.md)。M0 协议实验与 M1 内部仓储继续保留；真实账户登录见下方可信 SSO 切片。React 控制台第一切片见下文，后续生产能力按阶段实现。

```sh
scripts/install-go
scripts/m0 prepare
scripts/m0 up
scripts/m0 test
```

## M1 数据与租户骨架

新增事务性租户引导、组合外键隔离、组织创建/移动/预览、并发版本控制、最后 owner 保护、邀请一次性接受、组织成员关联、audit/outbox。见 [M1 手册](docs/runbooks/m1.md) 和 [ADR 0003](docs/adr/0003-transaction-bindings-and-m1.md)。M1 仓储现由下述公共 API 提供受权管理入口；真实 SSO 使用业务主库资格，M0 合成身份实验独立保留。

```sh
scripts/m1 prepare
scripts/m1 up
scripts/m1 test
```

本地验收现有 28 项真实协议测试和 27 项业务数据库测试及回调配置边界检查。后续开发与本地验收继续在此仓库进行；GCP 部署和云端运维由 SRE 专门负责。

## 本地账户验证与恢复

已提供真实账户页面、注册邮箱证明、Argon2id 密码、原子个人空间创建、一次性密码重置，以及加密通知/私有本地邮箱 worker。接口见 [账户 OpenAPI](api/accounts.openapi.yaml)，运行方式见 [M2 账户手册](docs/runbooks/m2-accounts.md)，安全边界见 [ADR 0004](docs/adr/0004-account-proof-lifecycle.md)。

```sh
scripts/m2 prepare
scripts/m2 up
scripts/m2 test
node tests/accounts/browser.cjs
```

通知仅写本地私有目录，不发送外部邮件。账户页面完成验证后不会自行建立 OAuth 会话，登录从接入应用发起。

## 真实登录、MFA 与双应用 SSO

新增可信 Hydra 登录/同意页面、强制 TOTP、一次性恢复码、中央会话、逐次授权的租户选择，以及两个独立 origin/client/key/数据库账号的 BFF。密码重置、成员停用、单应用退出与全设备退出均连接权威资格检查；token 只保存在服务端。

```sh
scripts/sso prepare
scripts/sso up
scripts/sso test
scripts/sso network-test
node tests/sso/browser.cjs
```

见 [SSO 运行手册](docs/runbooks/local-sso.md)、[ADR 0005](docs/adr/0005-trusted-login-mfa-bff.md)、[SSO OpenAPI](api/sso.openapi.yaml) 和 [本地验收记录](docs/validation.md)。浏览器测试自动创建和清理自己的合成账户，通过真实 Hydra code+PKCE、双应用/双租户、MFA/恢复/续期/撤销，并验证 BFF 数据库与网络权限。

当前是本地 HTTP 参考实现。两个 client 共用一个示范 Application；服务端客户端/凭证管理、完整 scoped RBAC、遗失认证器的受控恢复、正式邮件和生产 TLS/KMS 等仍待开发。GCP 部署及云端运维继续由 SRE 负责。


## 公共业务 API 与 OpenAPI

现有 57 个面向外部服务端/原生客户端的业务操作（包含角色与绑定管理）：身份/空间、成员停用恢复、组织创建移动/归属、一次性邀请、审计分页、受控批次导出和用户会话撤销。采用独立平台 audience、OAuth code+PKCE、Bearer scope、租户角色权限和最近 MFA；在业务事务内复查权威资格。公网部署仍由 SRE 负责。

```sh
scripts/api prepare
scripts/api up
scripts/api test
node tests/api/docs.cjs
```

公共合同：[OpenAPI 3.1 YAML](api/openapi.yaml)、[JSON](api/openapi.json)。服务提供 `/docs`、`/openapi.yaml` 和 `/openapi.json`；参考页从合同生成全部操作列表。接入、分页/ETag、错误和授权行为见 [公共 API 手册](docs/runbooks/public-api.md) 及 [ADR 0006](docs/adr/0006-public-platform-api.md)。此前同名 M0 内部合同保留为 [m0-internal.openapi.yaml](api/m0-internal.openapi.yaml)。


## 真实 React 管理控制台

新增通过独立 BFF 调用公共 API 的控制台，覆盖空间创建、成员停用/恢复、组织创建/移动/归属、一次性邀请、审计和自己的会话管理。同一浏览器不同标签页各有独立空间授权，OAuth token 留在服务端；敏感写入过期时要求重新 MFA，权限变更后需重新授权。

```sh
scripts/console prepare
scripts/console up
scripts/console test
```

见 [控制台手册](docs/runbooks/console.md)、[ADR 0007](docs/adr/0007-console-bff-contexts.md)、[浏览器 BFF OpenAPI](api/console.openapi.yaml) 和 [界面截图](docs/ui/console-overview.png)。React/Vite 用精确 npm lockfile 构建并嵌入 Go 服务，实际运行不需要 Vite 开发服务器。公共 API 0.17.0 共 57 个业务操作，包含租户角色与成员绑定；成员邮箱读取需要 members.read。


租户角色与权限已接入真实 API 和控制台：可分别委托成员、组织、邀请和审计能力，显式拒绝优先；owner 保留角色管理恢复能力。支持角色创建/编辑/删除、成员绑定/解绑和 owner 移交，最后 owner 受并发保护。角色变更使旧授权失效，需重新确认空间。见 [ADR 0008](docs/adr/0008-tenant-roles-and-deny.md)、[角色界面](docs/ui/rbac-roles.png)，验收运行 `scripts/console rbac-test`。当前范围为整个租户，组织子树、应用/资源范围授权仍待实现。


完整文档入口：[设计与开发手册](docs/handbook.md)、[逐接口 API 参考](docs/api-reference.md)、[API Playground](docs/playground.md)。控制台登录后选择 API Playground，展示 57 项 User 管理合同，其中 56 项可真实执行；一次性 SK 创建使用专用机器凭证页面，支持写入前确认、ETag、MFA/权限错误和响应凭证脱敏。公开 `/docs` 现提供逐接口参数、示例、schema 与响应。`scripts/docs export` 构建离线 HTML 和完整 ZIP，详见 [文档目录](docs/README.md)。


新增个人账户安全：MFA/恢复码/会话概览、撤销其他会话，以及统一认证域的密码+新 TOTP 恢复码重生成。旧码和全部旧会话在重生成后失效，新码只显示一次，认证器密钥保持不变。见 [账户安全手册](docs/account-security.md)；本地验收 `scripts/console security-test`。可信管理合同单列为 `api/security.openapi.yaml`。

已增加两阶段认证器更换：验证密码与旧 TOTP，验证新 TOTP 后切换密钥并撤销旧恢复码/全部旧会话；取消保留原认证器。见 [认证器管理](docs/authenticator-management.md) 与 [ADR 0011](docs/adr/0011-staged-authenticator-replacement.md)，验收 `scripts/console authenticator-test`。

个人安全记录现已接入控制台、公共 API 与 Playground：`GET /v1/me/security-events`，只返回当前用户的受控事件、时间和事件 ID，支持时间倒序分页。见 [使用手册](docs/security-events.md) 与 [ADR 0012](docs/adr/0012-personal-security-events.md)，验收 `scripts/console events-test`。

周五本地演示已提供固定数据与彩排工具：`scripts/demo prepare`、`scripts/demo rehearse`、`scripts/demo check`。含两个隔离空间、组织树、管理员/只读审计员、真实双应用 SSO 和 API Playground；登录资料仅写本地私有文件，重置保留其他开发数据。见 [10 分钟演示手卡](docs/runbooks/friday-demo.md)。

演示基线已固定为 `demo-friday-2026-11-13`（5d8b46b）。继续开发版新增 [应用接入管理](docs/application-management.md)：owner 查看已有应用/客户端登记，在当前空间停用/恢复应用，旧授权撤销且其他空间保留。机器身份与一次性凭证已提供独立切片，见下文。验收 `scripts/console applications-test`。

组织管理现已支持非根节点重命名、直属成员分页查看/移除，以及受保护的空节点删除。读取直属成员同时需要 organizations.read 和 members.read；删除与移除归属使用近期 MFA/版本前置条件，事务审计并使旧空间授权失效。见 [组织管理](docs/organization-management.md)，验收 `scripts/console organizations-test`。

自有应用和公开 PKCE 客户端现提供登记、协议核对、不可逆撤销与清理：pending 不可登录，配置/所有权完全匹配后才激活，本地撤销先于远端清理。现有平台/演示客户端只读，公开客户端固定平台只读 scope，无 secret。见 [客户端登记](docs/managed-clients.md)，验收 `scripts/console clients-test`；SP 与机器 AK/SK 使用独立流程，见 [机器凭证手册](docs/machine-credentials.md)。

机器身份新增 SP 创建/停用恢复、一次性 AK/SK、双凭证轮换、先本地撤销后协议清理、独立签发审计。机器令牌只访问对应应用的独立资源样例，每次查询主库资格；不进入平台管理/BFF。完整约束见 [机器凭证手册](docs/machine-credentials.md) 与 [机器协议 OpenAPI](api/machine.openapi.yaml)。本地验收运行 `scripts/console machine-api-test` / `scripts/console machine-test`；private_key_jwt、自定义机器业务授权与周期对账尚未提供。

外部业务服务现有 Go 在线机器身份验证器与独立服务/客户端参考：每次通过 SSO 主库检查身份，固定 namespace、拒绝缓存/重定向/错误 realm；独立 whoami 与匿名 OpenAPI 不进入平台管理/BFF。见 [Go SDK 与完整本地示例](docs/external-machine-integration.md)，验收 `scripts/console machine-sdk-test`。

审计页新增 [受控 JSON 批次导出](docs/audit-exports.md)：管理/机器来源独立、每批最多 100 条，下载时重新读取当前资格；支持只读审计委托、分页与取消，不包含邮箱或秘密。
