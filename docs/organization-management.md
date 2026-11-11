# 组织生命周期管理

控制台“组织”现支持创建/移动之外的重命名、直属成员分页、移除直接归属和删除空节点。它沿用租户角色权限、真实 BFF 与主库事务，不引入组织子树授权。组织归属本身不授予任何角色或业务权限。

## 操作与权限

| 操作 | API | 权限 / If-Match |
| --- | --- | --- |
| 重命名 | POST `/v1/tenants/{tenant}/organizations/{org}/rename` | organizations.write / `"org-N"` |
| 直属成员 | GET `/v1/tenants/{tenant}/organizations/{org}/members` | organizations.read **且** members.read |
| 移除归属 | DELETE `/v1/tenants/{tenant}/organizations/{org}/members/{member}` | organizations.write / `"authz-N"` |
| 删除空节点 | DELETE `/v1/tenants/{tenant}/organizations/{org}` | organizations.write / `"org-N"` |

公共与 BFF OpenAPI 0.21.0 共 59 个业务操作，Playground 与运行时 API 参考同步。写入需要 platform:write 和最近 300 秒 MFA；读取需要 platform:read。所有有效权限均在主库事务内检查，显式 deny 优先。直属成员沿用 Member schema，包括受 members.read 保护的已验证邮箱；不能仅凭结构读取权限枚举成员资料。

重命名 body 为 `{"name":"平台工程组"}`，服务端 trim 后要求非空、有效 UTF-8、最多 128 字节。其余两个写入 body 为 `{}`，包括 DELETE。使用 Node http 等底层客户端时，DELETE 也须正确设置 Content-Length 或传输分块，确保 `{}` 实际发送；服务端不会把空 body 当成有效 JSON。所有资源只能属于 token 指定的空间，另一空间路径或对象不允许借用。对象缺失返回 404；列表 limit 1–100，默认 50，按 membership ID 升序，以 next_cursor 作 after，不承诺快照。

## 删除与授权边界

根组织名称同时用作空间名称；组织操作委托不允许更改它。根节点重命名/删除返回 409 protected_organization；原移动接口仍使用 root_cannot_move。普通重命名只增加 org_version，不撤销权限；相同 trim 后名称和最新版本是 no-op，不增加审计/版本。

删除仅限没有直接子组织、没有任何直接成员关联的非根节点。已停用/已离开成员的关联也会阻止删除，返回 409 organization_not_empty。必须先显式移除关联或移动子组织，服务端不级联清理。删除以同一租户行锁与创建、移动、关联操作串行化，原子删除节点/闭包引用，增加 org_version 和 authz_version，写 audit/outbox。不会删除用户、空间成员资格、角色或邻接节点。删除不可恢复；同 ID 再删返回 404。

移除直接归属可针对已停用成员，保留其空间成员资格、owner/其他角色、其他组织归属。真实变更增加 authz_version；已无关联且最新版本时 no-op。为保守维护未来组织授权边界，删除和真实移除都会使该空间旧 grant/BFF 失效，包括操作者，必须重新确认授权；其他空间不受此版本变更影响。移除 owner 的组织归属不等于撤销 owner，不触发最后 owner 移交。

直属列表只列直接关联，不包含后代节点，不表示有效权限。通过成员列表刚读取的 authz ETag 可移除该关联；结构版本从组织列表读取。缺失前置条件 428、格式不正确 400、过时版本 412。前置版本校验先于资源检查，旧版本请求可能先收到 412。客户端不自动重放写入；网络结果不明须先重新登录/读取确认结果，不能靠旧 UI 判断是否已提交。

审计事件：org.renamed / org.deleted 的 target 为组织 ID，org.member_unassigned 的 target 为成员 ID，沿用既有 org.member_assigned 事件边界；不保存名称/邮箱/任意请求内容，暂未提供完整历史组织归属对照。事务 outbox 复用同一事件 ID；独立长期归档和消费者仍待实现。

## 控制台与验证

行操作隐藏根节点重命名/删除入口，直属成员入口同时要求两种读取权限。重命名/删除/移除均显示目标与影响并要求确认；陈旧版本/网络错误后禁用原表单，关闭、刷新后由用户重新决定。直属成员有分页，关闭或切换空间会取消读取，晚到响应不会重新打开弹窗。按钮可见性不代替 API 权限。

界面截图：[桌面直属成员](ui/organization-members-desktop.png)、[手机直属成员](ui/organization-members-mobile.png)。

运行 `scripts/console organizations-test` 验证真实登录、BFF→API→MySQL、合同响应、MFA、根/非空保护、跨空间、成员分页及桌面/手机操作。`scripts/m1 test` 包含闭包完整性、停用成员清理、委托读取双权限和并发创建/删除测试；原演示账号和数据不作这些测试的 fixture。周五基线标签 demo-friday-2026-11-13 保持原提交，开发后另跑 scripts/demo check。

0.21 起关联成员使用独立分页/确认弹窗，支持后页成员与组织，普通委托操作员沿用现有权限，写入未知回包不重发。详见 [组织关联分页](organization-assignment.md)；创建/移动上级选项的进一步改进仍待后续。
