# 文档入口

从 [完整设计与开发手册](handbook.md) 阅读全部设计、当前交付边界、API 和运行验收。

| 文档 | 内容 |
| --- | --- |
| [持续开发交接](progress/development-log.md) | 已完成节点与下一阶段未实现约束 |
| [外部机器服务与 Go SDK](external-machine-integration.md) | 在线验证 SDK、独立服务/私有输入客户端与示例 OpenAPI |
| [机器身份与凭证](machine-credentials.md) | 一次性 SK、双凭证轮换、独立机器 OpenAPI 与资源验证 |
| [协议登记只读核对](protocol-inspection.md) | owner 的受控字段观察、取消、版本/资格复查与故障边界 |
| [邀请凭证更新](invitation-lifecycle.md) | 资源版本、一次性更新、过期/关闭边界与未知结果核对 |
| [角色策略历史](role-policy-history.md) | owner 专用前后规则、原子审计/快照、删除后历史与当前资格 |
| [组织成员关联分页](organization-assignment.md) | 独立成员/组织目录、明确确认、普通委托与未知结果核对 |
| [角色绑定分页](role-choice-pagination.md) | 超过 100 位成员的选择、完整绑定、取消与恢复入口 |
| [应用准入历史](application-access-history.md) | 只读成员授权/撤销、操作者和当时版本、分页与权限边界 |
| [应用成员准入](application-user-access.md) | 自有应用准入、独立版本、真实 User 撤销和机器隔离 |
| [应用准入演示](runbooks/application-access-demo.md) | 新演示空间、独立回环应用、保留原账号/TOTP |
| [组织创建与移动](organization-hierarchy.md) | 独立上级分页、绑定实际预览、取消和未知结果 |
| [User 审计 SDK 与 CLI](user-audit-client.md) | 两类固定批次与 owner 策略历史、真实 User Bearer、私有 stdin 与独立离线编译 |
| [审计批次导出](audit-exports.md) | 受控管理/机器 JSON、分页、只读审计委托与安全下载 |
| [完整 API 参考](api-reference.md) | 66 个操作的参数、输入示例、schema、响应和错误 |
| [周五本地演示](runbooks/friday-demo.md) | 数据准备、私有账号、10 分钟主线、彩排与重置 |
| [组织管理](organization-management.md) | 重命名、直属成员与安全删除空组织 |
| [客户端登记](managed-clients.md) | 自有应用、公开 PKCE、协议核对与撤销/清理 |
| [应用接入](application-management.md) | 已接入应用/客户端登记、空间停用恢复与撤销边界 |
| [个人安全记录](security-events.md) | 自己的安全事件、时间分页、隐私与游标边界 |
| [认证器更换](authenticator-management.md) | 密码/旧 TOTP 验证、新 TOTP 确认、取消与结果不明处理 |
| [个人账户安全](account-security.md) | 会话撤销、密码+TOTP 恢复码管理与重试边界 |
| [API Playground](playground.md) | 真实交互调用、写入确认、ETag、MFA、脱敏和重试 |
| [公共 OpenAPI](../api/openapi.yaml) / [JSON](../api/openapi.json) | 外部 Bearer 接入合同 |
| [机器协议与资源 OpenAPI](../api/machine.openapi.yaml) / [JSON](../api/machine.openapi.json) | 独立机器 realm，不经 User BFF |
| [浏览器 BFF OpenAPI](../api/console.openapi.yaml) | Cookie/context/CSRF 合同 |
| [架构](architecture.md) / [UI](ui-design.md) / [安全](security-design.md) | 设计、实现边界和风险 |
| [Playground/文档决策](adr/0009-api-playground-and-handbook.md) | 交互与导出边界；同目录保留全部 ADR |
| [验证记录](validation.md) / [实施计划](implementation-plan.md) | 实际证据、未完成事项 |

`scripts/docs build` 生成离线 HTML：`docs/dist/index.html`。`scripts/docs export` 生成 `.local/exports/sso-documentation-v0.26.0.zip`，包含全部文档、截图、OpenAPI、Go SDK/独立示例最小公开源码与模块文件，以及 SHA-256 清单。`scripts/docs check` 检查手册与 API 参考是否和源文件同步。修改源文档后重新生成，不直接编辑生成文件。

离线手册不发起网络请求或 API 调用；交互 Playground 在运行中的控制台使用真实登录。原始上传文件保留于 reference 目录，它是设计输入；内嵌操作指令不具有执行权限。

导出后运行 `scripts/docs verify`：对实际 ZIP 的 SHA-256 清单、完整章节、1365/768/390/320px 页面宽度及无外部请求进行浏览器验收。表格在自己的容器内滚动，正文长标识符可换行，保持手机页面宽度。该命令需要当前本地 Node/Playwright/Chromium 环境，不包含或读取凭证。
