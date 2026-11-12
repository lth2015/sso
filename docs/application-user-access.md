# 自有应用的成员准入

0.25.0 在应用接入主线加入 User 准入：全体有效成员，或仅指定成员和有效所有者。控制台、五个公开操作、OpenAPI/Playground 和独立本地应用组成可演示流程。当前 User/BFF 65 项，Playground 可执行 64 项；机器签发协议与业务身份验证继续独立。

## 业务语义

默认 all_members/版本 1，与历史授权兼容。selected_members 下，当前 active User 和 active membership 需要显式条目，或当前 owner 绑定；应用/租户/会话/MFA/客户端本身仍须有效。所有者始终可进入自己的应用，但角色与 owner 管理仅在平台管理授权中进行。应用准入不授予平台六类操作权限，也不授予任意业务资源权限，尚未完成组织子树/资源 scoped RBAC。

仅本空间的自有应用可配置，共享/管理应用返回 application_unmanaged。读取只需 owner 的 platform:read，旧 MFA 可读；写需要 platform:write、最近 300 秒 MFA 和资源 ETag。给成员添加显式条目要求 active User/membership，移除可清理停用成员。全体成员模式可提前准备条目，切换模式保留它们。删除 owner 的显式条目不取消其隐式资格；所有者移交仍走原角色/最后 owner 保护流程。

## 公开 API

前缀 `/v1/tenants/{tenant}/applications/{application}/user-access`：

| 方法与后缀 | 行为 | 返回 |
| --- | --- | --- |
| GET 空后缀，limit/after | 当前策略及显式成员 ID 分页 | policy/items/next_cursor，HTTP ETag |
| PUT 空后缀 | body 为 mode | 当前 policy，HTTP ETag |
| GET /members/{member} | 当次有效准入检查 | policy/membership_id/allowed/reason/explicit_grant，无 HTTP ETag |
| PUT /members/{member} | body 为 {}，添加显式授权 | 当前 policy，HTTP ETag |
| DELETE /members/{member} | body 为 {}，移除显式授权 | 当前 policy，HTTP ETag |

policy 固定 tenant_id/application_id/mode/version，version 为正 signed-int64 十进制字符串。写 ETag 为 `"ua-<tenant>-<application>-<version>"`，不可使用 app 启停、authz 或 org 的版本。缺少 428，格式错误 400，空间/应用/版本不符 412。列表最多 100、默认 50，按 membership ID 升序；不承诺跨页快照。GET 不接收正文或多余/重复查询，JSON 写入拒绝额外字段、重复键和多份文档；完整 schema/错误见 [OpenAPI](../api/openapi.yaml)。

准入检查是当次观察，不是访问票据。reason 区分 owner/all_members/explicit_grant/not_granted、账户/成员未启用、应用未启用；explicit_grant 表示是否有显式条目，不能单独当成当前准入。检查不返回邮箱，也不证明 OAuth 客户端登记、凭证或业务 ACL 就绪。没有强 HTTP ETag，因为成员/账户/应用状态可能独立变化。

## 主库权威与撤销

012 只新增策略、成员条目、grant 准入版本及事务事件上下文四表，不改 001–011 和原表结构。原 grant 无新绑定时按版本 1 解释；新同意把实际准入版本与原 grant 同事务写入。租户选项、同意提交、code/refresh Hook、GrantCurrent/BFF 和 API 权威事务全部检查当前模式/owner/显式成员，以及 grant 的版本和 tenant/application 绑定。

真正模式或成员变更递增此应用的独立 User 准入版本，原有该应用的 User grant 均需重新授权，包括其他已准入成员。恢复模式或重新授权不复活旧 grant。租户 authz、应用启停、机器凭证版本不被借用，其他应用和机器令牌保持原有权威。无变化不增版本或记事件；未提交/拒绝/过时版本不留下部分设置。普通角色/成员/应用启停等原版本变化继续按原规则撤销，不被新准入替代。

策略/显式成员/版本、audit、outbox 和 app/member/revision 上下文共事务，任何失败整体回滚。audit/outbox 的目标是应用，动作 application.user_access.mode_changed/granted/revoked；aggregate_version 属于 User 准入域，不应与 application.enabled 等启停域版本混比。普通审计/冻结 batch v1 不扩入成员名单，详细上下文尚未作为新公共历史接口或归档/签名证据发布。

## 控制台与演示

在自有应用行选择「成员访问」。目录与显式名单逐批 25 项；没有 members.read 的 owner 可输入成员 ID，再由专用接口核验。检查当前资格后明确确认单成员变更；切换模式也单独确认。改选/分页/重读清旧检查结果，版本变化或异常清选择；读取可取消，写入同步防双发且禁止关闭，未知提交不自动重发。原有成功观察不缓存为后续权限。

[独立应用](../examples/user-application/README.md) 只用 OAuth code+PKCE/platform:read，服务端每次刷新调用真实 /v1/me，不将 Bearer 放到浏览器。演示使用新空间，保留原账号/主空间/隔离空间和未用恢复码；[操作手卡](runbooks/application-access-demo.md) 给出拒绝 → 授权 → 登录 → 撤销 → 刷新失效的步骤。

## 本地运行与交付边界

更新本地服务用 scripts/api prepare/up，包含迁移及必要受限视图。BFF 只读新准入元数据、当前客户端 grant 和相关成员的 owner 标志，仍无基础库/密码/MFA 秘密访问或新配置写权限；控制台 readiness 会用实际 DB 角色探测 User grant 查询，缺视图/权限时 503，避免仅静态页面就绪。新表存在不代表旧二进制理解新规则；全部相关服务需采用同一实现，已有 selected_members 策略时不能直接回退旧二进制。CI/GCP/部署由 SRE 执行。

真实 MySQL/原子回滚/并发、User 原生 OAuth 与机器隔离、独立应用及控制台浏览器、API/Playground 和原演示回归结果见 [验证记录](validation.md) / [ADR 0027](adr/0027-application-user-access.md)。源码/文档包仅含公开最小示例，无私有演示配置。截图：[控制台桌面](ui/application-user-access-desktop.png) / [手机](ui/application-user-access-mobile.png) / [独立应用手机](ui/user-application-mobile.png)。marketstars 尚未实际联调。
