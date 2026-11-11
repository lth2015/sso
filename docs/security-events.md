# 个人安全事件记录

控制台“安全会话”中的“个人安全记录”展示当前用户的安全事件，按时间从新到旧分页。支持刷新到最新记录、查看较早一页；离开页面会取消读取，切换授权上下文会清空旧记录。读取失败时显示错误，由用户明确刷新，不自动重试。

事件包括账户注册、收到密码重置请求、密码重置完成、认证器启用/更换/取消、恢复码重生成、通过统一身份验证、会话撤销、全设备退出和拒绝应用授权。密码重置请求只说明系统收到请求，不能据此认定由用户本人发起。匿名事件、未经筛选的新事件类型和其他用户的记录不会返回。

## 公共 API

`GET /v1/me/security-events?limit=20&after=<next_cursor>`，operationId：listMySecurityEvents。使用 platform:read、平台 audience 和有效租户授权，不需要租户 audit.read 或 owner 权限；最近 MFA 超过 300 秒仍可读取。用户身份由已验证 access token 和主库权威确定，不能指定 user_id/tenant_id。没有公开读取其他用户安全记录的接口。

成功响应形状为 `{items:[{id,action,occurred_at}],next_cursor}`。字段、格式和错误详见 [公共 OpenAPI](../api/openapi.yaml) 与 [逐操作 API 参考](api-reference.md)。BFF 使用同一路径加 `/bff` 前缀和 cookie/context；Playground 中选择 listMySecurityEvents 即可真实调用。

- limit 默认 50，范围 1–100；控制台每页 20 条。
- 顺序为 occurred_at DESC、id DESC，保留数据库微秒精度；相同时间用 ID 降序稳定排序。
- after 是同一用户上一页最后事件的 ID；省略时从最新页开始，不存在、全零、其他用户或不可见事件的 ID 均返回相同 404，不能用游标查阅别人记录。格式错误/未知或重复查询参数返回 400。
- 没有分页快照。列表加载期间的新事件可通过“刷新最新记录”查看；游标所指记录被清理后需回到第一页。

只返回独立事件 ID、受控事件代码和时间。内部 target_id 可能指向私有登录挑战、恢复码管理或认证器更换请求，因此不对外返回；也不返回邮箱、IP、设备信息、密码、token、cookie、验证码、种子、恢复码或自由 payload。

## 数据与边界

复用已有 identity_events 及 (user_id,occurred_at,id) 索引，不新增或修改迁移。SQL 在同一权威事务内重新校验有效 User/Session/grant/当前租户资格，并限定 user_id；游标查询也使用相同用户和可见类型过滤。事件是 User 范围，切换租户不会赋予他人数据权限。

此能力不等于不可变审计归档、登录失败监控、异常设备检测、安全通知或完整事件消费者。现有身份事件在业务事务中的记录点决定其含义；例如 session.authenticated 表示通过身份验证，不承诺下游每个应用已完成回调。历史保留/删除策略、独立归档和正式邮件仍需后续实施。

## 验收

原生 API 套件使用真实 OAuth/PKCE token 验证成功/分页、只读 scope、普通成员、MFA 过期读取、未认证拒绝、用户参数拒绝及其他用户游标 404。其真实数据库探针另验证微秒时间/相同时间排序、连续分页、未公开事件过滤、范围限制和调用方伪造 User 被拒绝。

`scripts/console events-test` 验证真实 BFF/主库记录、控制台分页/刷新、导航后的迟到响应丢弃、手机布局和真实 Playground；测试账户和事件夹具在退出后清理。不使用认证 stub，不截图或记录登录秘密。设计见 [ADR 0012](adr/0012-personal-security-events.md)。
