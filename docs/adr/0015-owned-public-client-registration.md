# ADR 0015：自有应用、公开客户端与权威优先的核对

状态：接受为本地开发实现；实际验收见 validation.md。延续 ADR 0013/0014 的演示和权限恢复边界，不修改共享客户端或既有迁移。

现有应用关联可管理启用/停用，但其全局客户端不能被任意关联租户修改。新增 managed_applications 将一个全局 Application 唯一绑定到拥有空间，并用组合 FK 约束其当前空间关联/创建者；managed_oauth_clients 将管理状态限制到该所有权，实际 oauth_clients 仍为签发/资源权威。服务读写同时核对 application/client 映射，不能只凭全局 client_id 行命中更新。

先实现无 secret 的公开 PKCE profile，固定 audience 与只读平台 scope。平台公开 API 不接受 arbitrary grant/scope/audience/auth/JWKS，服务端/机器凭证另立边界。所有管理为 owner 和近期 MFA，名字/精确回调/配额有界。共享应用只读，其他租户不能通过 enabled 关联夺取所有权。

业务库和引擎库不能原子提交。创建先提交 pending/inactive，reconcile 才创建/读取远端并核对不可变 profile 与 metadata，再原子激活和审计。未知回包可能产生远端 orphan，但主库未激活就不能完成可信登录或业务签发。恢复先读已有对象，不盲目重建、覆盖或采用。撤销本地先提交，令版本失效；协议清理不构成资格撤销的前提。

普通并发核对/撤销在有界租户事务锁内串行，选择可审查的首个切片，代价是该租户管理可能短暂等待远端调用。总 deadline 4 秒，RPC 3 秒，不做任意网络重试。必须先完成现有基础行为验证后，再为规模引入细分锁/lease/job；不能在未经验证的跨库竞态中乐观提交 active。此前超时请求可能稍后完成，absent 只能描述当次观察；revoked 墓碑持续拒绝，后续显式核对可重复清理。当前没有周期性 reconciliation worker，不将此首期切片视作完成全部补偿运维。

控制台明确 pending/active/revoked、目标 URI、固定范围和确认，失败先刷新，再由用户决定；Playground 沿用同一合同、只读元数据与资源绑定 ETag。新 UI 不接收/显示 Admin 或协议 secret，普通 browser BFF 的网络/SQL 隔离继续保留。

业务主库注入响应丢失/冲突并验证回滚、恢复、no-op 和并发；真实 Hydra 独立验证 code/PKCE、scope、refresh/撤销和所有权 metadata；真实浏览器检查 owner/member/错空间/MFA、冲突拒绝恢复、核对/撤销/清理、Playground 和手机布局。演示账号不作可变 fixture。
