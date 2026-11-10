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

已提供 Go token-policy-hook、本地 MySQL + Hydra、固定镜像/工具链、真实协议测试；运行方式见 [M0 运行手册](docs/runbooks/m0.md)，具体适配发现与未通过的生产门槛见 [ADR 0002](docs/adr/0002-hydra-m0-findings.md)。M0 协议实验与 M1 内部仓储继续保留；真实账户登录见下方可信 SSO 切片。React 控制台和后续生产功能仍待实现。

```sh
scripts/install-go
scripts/m0 prepare
scripts/m0 up
scripts/m0 test
```

## M1 数据与租户骨架

新增事务性租户引导、组合外键隔离、组织创建/移动/预览、并发版本控制、最后 owner 保护、邀请一次性接受、组织成员关联、audit/outbox。见 [M1 手册](docs/runbooks/m1.md) 和 [ADR 0003](docs/adr/0003-transaction-bindings-and-m1.md)。管理能力当前仅内部仓储，没有公开管理 API；真实 SSO 使用业务主库资格，M0 合成身份实验独立保留。

```sh
scripts/m1 prepare
scripts/m1 up
scripts/m1 test
```

本地验收现有 28 项真实协议测试和 10 项业务数据库测试。后续开发与本地验收继续在此仓库进行；GCP 部署和云端运维由 SRE 专门负责。

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

当前是本地 HTTP 参考实现。两个 client 共用一个示范 Application；正式 React 控制台、完整 scoped RBAC、MFA 管理、正式邮件和生产 TLS/KMS 等仍待开发。GCP 部署及云端运维继续由 SRE 负责。
