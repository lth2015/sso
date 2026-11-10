# 公共多租户 SSO 身份平台

独立身份平台：Ory Hydra 负责 OAuth/OIDC，Go 服务管理身份、租户、组织和权限；React 控制台通过 BFF 使用服务。目标部署 GCP，当前从本地 M0 验证开始。

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

已提供 Go token-policy-hook、本地 MySQL + Hydra、固定镜像/工具链、真实协议测试；运行方式见 [M0 运行手册](docs/runbooks/m0.md)，具体适配发现与未通过的生产门槛见 [ADR 0002](docs/adr/0002-hydra-m0-findings.md)。M0 本地协议验证已扩展，M1 已交付第一批内部数据/仓储能力；完整登录、React 控制台和后续生产功能尚未实现。

```sh
scripts/install-go
scripts/m0 prepare
scripts/m0 up
scripts/m0 test
```

## M1 数据与租户骨架

新增事务性租户引导、组合外键隔离、组织创建/移动/预览、并发版本控制、最后 owner 保护、邀请一次性接受、组织成员关联、audit/outbox。见 [M1 手册](docs/runbooks/m1.md) 和 [ADR 0003](docs/adr/0003-transaction-bindings-and-m1.md)。当前仅内部仓储，没有公开业务 API，M0 与 M1 权威状态尚未连通。

```sh
scripts/m1 prepare
scripts/m1 up
scripts/m1 test
```

本地验收现有 28 项真实协议测试和 10 项业务数据库测试。后续开发与本地验收继续在此仓库进行；GCP 部署和云端运维由 SRE 专门负责。
