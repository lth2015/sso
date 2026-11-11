# ADR 0008：租户角色、显式拒绝与 owner 恢复边界

状态：已实现，验收结果见 [validation.md](../validation.md)。范围为租户级权限；组织子树、Application 和资源范围策略另行实施。

## 授权模型

每个租户的 Role 通过 RoleBinding 绑定到 Membership，组合外键阻止跨租户引用。普通成员默认无操作权限；组织归属和邀请接受只增加关联/成员资格，不授予角色。以下权限分别授权，写权限不隐含读取权限。

| 权限 | 能力 |
| --- | --- |
| `members.read` | 读取当前租户成员目录，包括已验证邮箱 |
| `members.write` | 停用/恢复普通成员；受委托操作者不能停用或恢复任何带 owner 绑定的成员 |
| `organizations.read` | 读取组织树 |
| `organizations.write` | 创建/移动组织、预览移动、关联成员 |
| `invitations.manage` | 创建/读取/撤销邀请；接受时重新检查创建者仍具该权限 |
| `audit.read` | 读取当前租户的受控审计元数据 |

同一成员所有角色的 allow 取并集，任一相同权限的 deny 优先，包括 owner 的上述操作权限。内置 owner 角色不可修改/删除；其 `roles.manage` 是保留的恢复管理能力，不能放进自定义 allow/deny 策略。只有 active owner 可管理角色与绑定，因此委托的成员管理员不能自提权。owner 可解除使自己被拒绝的操作角色；这项恢复边界明确不提供可被 deny 锁死的不可逆策略。

owner 绑定支持显式移交：先给另一 active 成员绑定 owner，再移除旧绑定。删除最后一位 active owner 被拒绝；停用保护沿用同一规则。UI 对 owner 绑定额外要求勾选确认。带绑定的自定义角色禁止删除，先显式解除绑定。每租户最多 100 个角色（含 owner），每角色最多 12 条策略（六种权限的 allow/deny）；不接受通配符、未知权限、重复策略或任意 JSON 条件。

## 权威事务与撤销

每次操作在主库事务内锁定租户、检查当前 User/Session/Membership/grant 和所需权限。写入仍需 `platform:write` 与最近 300 秒 MFA；读取仍需 `platform:read`。修改角色、策略和绑定还需 `If-Match: "authz-N"`，缺失 428，过期 412。

实际变更与 authz_version 递增、审计、outbox 同事务提交，任一失败整体回滚。旧租户授权全部失效，包括执行者；刷新 token 不升级权限版本，必须重新授权。重复绑定/解绑及相同策略替换是 no-op，不递增版本，也不新增审计。租户行锁串行化并发 owner 移除和策略修改。

005 迁移增加 role_policies 与 role_binding_changes。绑定事件对象保留角色 ID、当时的 code、成员 ID 和 bind/unbind；审计以事件 ID 为 target，角色删除后仍可解释绑定历史。列表不返回凭证或证明。政策变更事件只记录动作、目标与版本，完整政策前后快照/合规归档仍未实现。

## API 与界面

公共 [OpenAPI 0.6.0](../../api/openapi.yaml) 共 26 个业务操作；本批新增 8 个：当前有效权限、角色列表/创建/更新/删除、成员绑定列表/绑定/解绑。嵌套 JSON 仅用于明确的策略数组：4 KiB 限额、字段白名单、递归重复键检查、深度与解析工作量边界同时生效。公共 Bearer 接入与浏览器 cookie/context/CSRF 合同继续分离，BFF 仅转发固定 API 方法/路径。

React 先加载当前有效权限，再显示相应导航与操作按钮；服务端授权仍是最终边界。角色页面支持逐权限选择允许/拒绝/未设置、编辑/删除角色和成员绑定。最近 MFA 过期要求重新认证，变更后提示重新确认空间授权，均不自动重放写入。角色选择最多 100 个；成员选择最多 100 个，尚未提供全量搜索。若 owner 自身被拒绝读取成员目录，角色页仍可管理自己的绑定，以保留恢复入口。

验收入口为 `scripts/m1 test` 与 `scripts/console rbac-test`。后者通过真实注册、密码/MFA、Hydra code+PKCE、BFF 和 MySQL 检查新合同、委托、拒绝优先、撤销、恢复边界、owner 移交与真实 UI；截图仅包含合成业务资料。公网部署、GCP 与云端 HA 仍由 SRE 负责。
