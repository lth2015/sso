# ADR 0012：个人安全事件查询

状态：已实现，实际验证见 validation.md。延续 ADR 0010/0011 的 User 范围安全能力。

提供 GET /v1/me/security-events，scope 为 platform:read。当前用户在有效租户授权下可读取自己的安全记录，不受租户 audit.read 或 owner 控制；任何租户角色都不能借此查询其他 User。身份、会话和 grant 在 SQL 事务内复查，查询与游标都限定同一 User。

复用 identity_events 及现有索引，按 occurred_at DESC/id DESC 返回有限页。游标只暴露事件 ID，通过用户限定的查询获取微秒时间作边界；不同用户、未知或未公开类型的 ID 统一 404。允许记录增长但不承诺快照；游标失效时显式回到首页。

响应只有 id/action/occurred_at。独立事件 ID 不授予操作权；target_id 可能是私有 challenge/action 标识，因此永不返回。只开放审核过的事件类型，不提供任意 payload、用户/租户过滤或其他人的记录。收到重置请求不代表本人操作；身份认证事件不保证应用回调完成；不把有限事件列表宣传为完整安全监控或独立归档。

控制台在安全会话页展示中文事件名、受控代码和时间，20 条分页/刷新。读取与 context/cursor/generation 绑定，取消/迟到响应不覆盖新页面，context 变更重建组件。没有自动重试或持久浏览器历史；React 文本输出转义。Playground 使用现有 BFF 固定白名单和真实主库权限。公共/BFF/security 合同版本统一 0.10.0，公共业务增至 29 操作。

本轮不改 CI、部署、既有迁移及 GCP；不可变归档、登录失败/设备监控、正式通知及事件消费者仍需继续实施。
