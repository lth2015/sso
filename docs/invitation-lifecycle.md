# 邀请凭证更新

0.17.0 已实现邀请生命周期的受控凭证更新，验收证据见 validation.md。新增 invitation detail 和一次性 proof 更新，管理者可处理凭证遗失或过期；目标邮箱和资源 ID 固定，旧 proof 在更新提交后失效，已接受/撤销的邀请不能重新开启。当前没有正式邀请邮件投递，安全转交仍由调用者负责。

公共接口：`GET /v1/tenants/{tenant}/invitations/{invitation}` 返回当前 email/state/expiry/revision 及资源 ETag；`POST .../reissue` 需要空 JSON、精确 If-Match、platform:write、当前 invitations.manage 与最近 300 秒 MFA。读取需要 platform:read/invitations.manage。同一版本并发仅一项成功，其他 412；已关闭且使用当前版本时 409。响应只一次返回新 token、id、expires_in=86400、revision；读取不恢复 proof。遇到未知结果先读取当前状态，再显式决定是否重新更新，不能自动重试。

migration 010 的独立 invitation_revisions 以全局唯一 invitation ID 关联原记录；缺少辅助记录视为 revision 1，保留原邀请行。后续成功接受、撤销或更新递增 revision；辅助行随父邀请删除级联清理，所有访问先检查父邀请所属空间/当前资格。更新将责任管理者改为当前操作成员，接受仍重新验证该管理者当前资格。proof digest、期限、revision 和审计/outbox 同事务，不改变租户授权版本。已应用 001–009 不改动。

控制台「成员邀请」对待接受/过期条目提供「更新邀请凭证」。先读当前元数据与版本，再明确确认旧 proof 失效。成功后新 proof 只在专用对话框显示一次；关闭、切换空间、取消和迟到响应清除结果。写入丢包不重发、不恢复秘密；重新读取后可再次显式更新。Playground 可查看/调用合同，其通用响应仍脱敏 proof，安全转交请使用专用界面。

实际原生 API/浏览器、主库并发、近期 MFA、丢包和秘密排除验收记录在 validation.md；实施前约束保留于 progress/invitation-reissue-next.md。本地运行不等于正式邮件、审计归档、互联网部署或受控 MFA 恢复已经交付。

实际界面：[桌面](ui/invitation-reissue-desktop.png) / [手机](ui/invitation-reissue-mobile.png)。截图只包含确认表单，未包含一次性 proof。验收命令：`scripts/console invitations-api-test`、`scripts/console invitations-test`；主库 `scripts/m1 test`，前端边界 `node --test tests/invitations/model.test.mjs`。
