# 持续开发交接

用户授权持续本地功能开发、验收、文档化并推送 master；CI/GCP/部署由 SRE 负责。周五演示基线 `demo-friday-2026-11-13` / 5d8b46b 及私有演示数据必须保留。此前稳定开发提交 f8e5dbc（OpenAPI 0.12.0 / 38 操作），完整验收见 validation.md。

当前推进：租户自有应用、公开 PKCE 客户端的 pending → active → revoked 生命周期与协议对账。先实现不含 secret 的注册，再推进 confidential/SP/凭证。已有共享平台/演示应用不能被任意租户改写客户端。每块功能在真实主库/Hydra/浏览器验收后提交，不把实验或待办写成已完成。

后续队列：服务端客户端与凭证一次性发放/轮换；SP/机器身份与受限 audience/scope；受控 MFA 恢复；组织/应用范围授权；审计投递及导出。当前会话停止不代表后台继续执行，以 Git/本日志和 validation.md 为实际进度。

工作中记录：已新增 migration 008、owned application/public PKCE pending/active/revoked 仓储、五个 API（合同草稿 0.13.0 / 43 操作）、控制台及核对/撤销。28 项仓储/配置检查已实际通过（其中 27 项连接主库），真实浏览器/原生协议及全部既有回归已通过，随当前节点提交；实际 SHA 以 Git 历史为准。私有 Admin 连接已与出站代理分离；注入协议 drift 的测试夹具明确使用 UTF-8，未放宽名称/profile 比较。以 validation.md 的最终通过记录为交付依据。

下一切片的未实现设计与验收约束记录在 machine-credentials-next.md，避免平台中断后丢失决策；先完成当前节点的提交，再切换实现。

0.13.0 本地节点完成：27 项主库测试 + 回调边界共 28 项、147 个原生 API 响应、30 个新浏览器合同响应与既有回归/演示全部通过，秘密排除/文档包在提交前完成检查。下一步按 machine-credentials-next.md 实施机器身份与一次性凭证。
