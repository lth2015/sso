# 实施与验收计划

先提交设计到 master，再进入代码实施。当前已推进到 M2 本地可信 SSO 切片；以下功能在相应验收前均为待实现，不将原文的目标当成事实。

| 阶段 | 代码交付 | 通过门槛 |
| --- | --- | --- |
| M0 | 固定 Hydra/MySQL、本地启动、Go Hook、真实协议测试、运行手册 | code+PKCE/refresh/M2M、ext/aud/JWKS、拒绝/故障、重放测试；未验证项列明 |
| M1 | business migrations、tenant-aware repo、组织/成员、audit/outbox | 组合 FK、并发组织移动、最后 owner、事务回滚与隔离 |
| M2 | 可信登录 UI/API、密码/邮箱/MFA、BFF/SDK、双应用示例 | 真正 SSO、CSRF/state/nonce、租户固定、refresh/退出 |
| M3 | React 控制台、客户端、SP/凭证与补偿 | 一次性 SK、step-up、限权、orphan/pending 拒绝、日志脱敏 |
| M4 | scoped RBAC、授权接口与两类应用适配 | deny 优先、委托边界、资源归属、缓存撤销上限 |
| M5 | GCP IaC、监控、压测、HA/DR、安全恢复 | 真实容量/恢复证据、安全评审、试运行后才承诺 SLA |

M0 不通过不扩写业务功能。选择版本需记录源码引用、许可证、digest 与兼容性；“存在配置项”不等于“所有 grant 安全”。引擎短板、维护状态不明确、Cloud SQL 未验证均单列为剩余风险，禁止自行实现 OAuth 绕过。

每次交付保留：执行命令、测试数量与结果、代码 SHA、环境限制、未运行项。当前可运行证据见 runbooks/m0.md（代码阶段创建）。生产发布另行执行。

## 当前进展

M0 本地已覆盖每交易/链绑定、28 项真实协议测试、会话/退出与签名轮换；云端 HA 按用户要求本轮不验证，不记为通过。M1 第一切片已实现内部数据迁移与 tenant-aware 仓储，已补充内部邀请/组织成员关联，当时尚缺账户验证服务与通知 worker，现已由下述 M2 切片补充；公开管理 API、租户事件 worker 与完整 RBAC 仍待实现。当前状态以 [验证记录](validation.md) 为准，不将本地结果写成生产架构冻结。

后续仅推进本地开发与验收；GCP 部署/运维由 SRE 对应，不等待云端凭证或实例信息。生产验收项单列给 SRE，不在本地标记通过。

M2 账户生命周期已落地：验证邮箱后设置密码，原子创建个人空间，重置递增 User 安全版本，通知加密入库并可靠落到本地私有邮箱。四个 HTTP API 与真实页面/浏览器验收已提供；后续可信登录切片已连接独立 Hydra 业务引擎，详见下段；真实邮件与正式控制台仍待实现。


M2 可信 SSO 已实现本地密码/TOTP/一次性恢复码、中央会话、独立授权租户、权威 token Hook、两个 BFF、串行 refresh、单应用及全设备退出。真实浏览器与实际签名 token 反例覆盖见 validation.md。BFF 使用各自的 key、受限 MySQL 视图及独立网桥；Admin/私有 Hook 不可从 BFF 网络访问。当前 SDK 能力是 Go BFF 中的实际验证适配，并非已发布多语言 SDK。

下一步优先提供会话与 MFA 管理、React 控制台的身份/空间页面，以及管理 API 的逐操作授权合同；其后推进产品级 scoped RBAC、客户端/机器身份凭证与事件消费。设计中长期 refresh、完整 step-up 和生产恢复屏障不能由当前 demo 替代。


公共业务 API 第一批已实现 18 个操作及 OpenAPI 3.1 YAML/JSON，覆盖身份/空间、owner 成员与组织管理、一次性邀请、审计和自己的会话。正式 React 控制台、任意 scoped RBAC、动态第三方 client/机器凭证管理、MFA 更换/恢复管理仍待后续交付。外部客户端必须通过受权 OAuth token 调用，不将 Cookie 或内部仓储直接当成公共接口。见 ADR 0006 与最新验证记录。


M3 控制台第一切片已交付：真实 React 页面经独立 BFF 使用公共 API，包含空间、成员、组织、邀请、审计、会话管理及多标签页空间隔离。OAuth token 留在服务端，资格/最近 MFA/版本仍由 API 事务检查；控件可见性不能代替授权。该切片不完成 M3 的动态 client/SP/credential 管理；完整 RBAC、MFA 更换和受控恢复仍待后续实现。见 ADR 0007。
