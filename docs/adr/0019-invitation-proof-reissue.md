# ADR 0019：显式更新一次性邀请凭证

状态：已接受并实施；真实主库、原生 API 与浏览器专项通过，完整兼容回归与资料记录见 validation.md。

对尚未关闭的邀请增加详情/资源 revision 及显式 proof 更新。依当前 invitations.manage 与最近 MFA/精确 ETag 在主库串行化；旧 proof 立即失效，新 proof 只返回一次。资源/空间/邮箱固定，已接受或撤销不复活；更新后当前操作者负责邀请接受所需的管理者资格。无需撤销无关租户 grant。

辅助版本表按邀请全局唯一 ID 关联原记录，缺省 revision=1，不重写原邀请或旧迁移。接受、撤销和更新的实际转换同事务递增 revision，更新审计/outbox 使用新 revision。更新与接受并发时按主库锁顺序产生单一结果；初始 proof 查找后被替换仍受控拒绝。未知回包不恢复或自动重放。

UI 明确说明旧 proof 失效、24 小时有效期和无自动邮件。秘密只在专用对话框内存/受控 DOM；关闭/空间/页面改变与迟到响应不再显示。公开 API/BFF/OpenAPI/Playground 一致。真实数据库、原生 Bearer、浏览器、故障注入和既有功能回归证据以 validation.md 为准；见 invitation-lifecycle.md / validation.md。
