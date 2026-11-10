# 实施与验收计划

先提交设计到 master，再进入代码实施。当前选定首个可审查交付为 M0；以下功能在验收前均为待实现，不将原文的目标当成事实。

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
