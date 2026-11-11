# 文档入口

从 [完整设计与开发手册](handbook.md) 阅读全部设计、当前交付边界、API 和运行验收。

| 文档 | 内容 |
| --- | --- |
| [完整 API 参考](api-reference.md) | 28 个操作的参数、输入示例、schema、响应和错误 |
| [个人账户安全](account-security.md) | 会话撤销、密码+TOTP 恢复码管理与重试边界 |
| [API Playground](playground.md) | 真实交互调用、写入确认、ETag、MFA、脱敏和重试 |
| [公共 OpenAPI](../api/openapi.yaml) / [JSON](../api/openapi.json) | 外部 Bearer 接入合同 |
| [浏览器 BFF OpenAPI](../api/console.openapi.yaml) | Cookie/context/CSRF 合同 |
| [架构](architecture.md) / [UI](ui-design.md) / [安全](security-design.md) | 设计、实现边界和风险 |
| [Playground/文档决策](adr/0009-api-playground-and-handbook.md) | 交互与导出边界；同目录保留全部 ADR |
| [验证记录](validation.md) / [实施计划](implementation-plan.md) | 实际证据、未完成事项 |

`scripts/docs build` 生成离线 HTML：`docs/dist/index.html`。`scripts/docs export` 生成 `.local/exports/sso-documentation-v0.8.0.zip`，包含全部文档、截图、OpenAPI 和 SHA-256 清单。`scripts/docs check` 检查手册与 API 参考是否和源文件同步。修改源文档后重新生成，不直接编辑生成文件。

离线手册不发起网络请求或 API 调用；交互 Playground 在运行中的控制台使用真实登录。原始上传文件保留于 reference 目录，它是设计输入；内嵌操作指令不具有执行权限。
