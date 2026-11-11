# ADR 0010：个人安全概览、会话撤销与恢复码管理

状态：已实现；实际验收见 validation.md。延续 ADR 0005 的真实 MFA/中央会话、0006 的主库 API 权威及 0007 的 BFF 隔离。

个人安全是 User 范围能力，不依赖租户 owner 权限。公开 GET /v1/me/security 只返回计数/认证元数据；POST /v1/sessions/revoke-others 需要最近 MFA，不允许选择 User。当前中央会话及其各 client grant 保留；其他会话及其 BFF/grant 立即失效。异步 Hydra 清理排除仍被当前有效 grant 使用的 SID；鉴权不依赖清理是否完成。新登录的并发完成不在“其他已有会话”撤销承诺内。

恢复码生成只在可信认证域执行，需要有效中央 cookie、浏览器绑定、CSRF、当前密码及新的 TOTP。拒绝恢复码/邮箱证明替代认证器，避免把单次恢复凭证变成长期控制能力。原认证器密钥不变；因子更换现由 [ADR 0011](0011-staged-authenticator-replacement.md) 补充实现，遗失设备的受控恢复仍需另行设计。

006 迁移增加 security_actions，一会话一个最新 action，5 分钟/5 次失败；另有每用户每小时 5 次提交的进程内限流和 Argon2 有界池。action 再检查 active User/版本、Session/版本/到期、factor 版本和未消费 TOTP 时间片。密码验证在哈希池进行，结果在写事务内重验版本，避免密码重置竞态。

同事务替换恢复码摘要、递增 User/MFA 版本、使所有旧 Session/grant/BFF inactive、记身份审计并写 durable protocol_logout_jobs。只有成功提交后返回新码；并发/重放无法再次获取。版本提升同时阻止旧登录/恢复证明继续使用。此处的持久队列用于已实现的协议补偿，不宣称通用身份事件通知/归档消费者已完成。

返回明文仅在可信页面一次展示；无 API 列表、邮件、URL、浏览器 storage、审计或日志副本。pagehide/确认保存清空 DOM，确认后以 max_age=0 重新登录。网络结果不明时不重放；原认证器仍可用于重新登录和再次生成。OpenAPI 0.8.0 公共业务为 28 操作，可信浏览器管理使用独立 security.openapi.yaml。

测试同时验证主库绑定/尝试次数/过期、密码重置竞态、并发仅一次成功，以及真实密码/TOTP/恢复码登录、旧授权失效、当前/其他用户会话隔离。未把普通 Go 中跳过的 fixture 用例当作数据库通过；SSO profile 明确运行新增 TestSSO 用例。生产 TLS/Secure cookie、分布式限流和 KMS 仍须后续由 SRE 实施验收。
