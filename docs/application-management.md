# 应用接入管理

这一切片提供当前空间已接入应用的目录、客户端登记元数据，以及应用在该空间的停用/恢复。后续自有应用/公开 PKCE 登记见 [客户端登记](managed-clients.md)；client secret 发布/轮换、机器身份与任意共享应用管理仍在后续计划中。

空间 owner 可在控制台“应用接入”查看名称、可用状态、启用状态和客户端数量，并分页查看已登记 client_id、精确回调 URI、scope、audience、客户端状态/版本和 issuer。它不读取协议管理端 secret 或令牌，也不对全体用户开放全局应用目录；普通成员及受委托审计员不能管理此入口。

停用需要最近 300 秒 MFA、明确确认和刚读取的应用版本。动作只针对当前空间和目标应用：关联状态/版本和审计/outbox 同事务提交，旧 grant/BFF 按其绑定版本立即失去权威；其他空间、其他应用和中央登录保留。不会注销可能共享的 Hydra SID。资源权限和 code/refresh Hook 在主库复查，因此撤销不等待协议清理。

恢复只能用于已有的关联且全局应用仍有效。恢复后必须重新授权；旧 grant 和旧 BFF 会话不会重新生效。存量加密会话行按既有到期任务清理，不代表立即物理删除。相同状态配合最新 ETag 不增加版本或重复记变更事件。平台管理入口保持启用，以保留 owner 的恢复入口；全局停用由可信平台管理端控制，本接口不能绕过。

## API 和并发

| 方法与路径 | 用途 |
| --- | --- |
| GET `/v1/tenants/{tenant}/applications` | 当前空间已有应用，包含已停用项，按应用 ID 分页 |
| GET `/v1/tenants/{tenant}/applications/{application}` | 读取状态与 ETag |
| GET `/v1/tenants/{tenant}/applications/{application}/clients` | 按 client_id 升序分页查看公开登记元数据 |
| POST `/v1/tenants/{tenant}/applications/{application}/disable` | JSON `{}`，停用关联 |
| POST `/v1/tenants/{tenant}/applications/{application}/enable` | JSON `{}`，恢复关联 |

读取需要 platform:read；写入需要 platform:write、owner 和最近 MFA。完整合同见公共/BFF OpenAPI 0.11.0，公共操作增至 34 个；Playground 使用同一合同。停用后，受保护资源拒绝旧 access token；本地固定 Hydra 的直接 refresh 拒绝为 HTTP 403 且不签发新 token，不使用公共业务 API 的 RFC 9457 body。应用列表使用 UUID 游标；客户端列表的 after 是上页返回的可打印 client_id，需 URL 编码，不能套用 UUID 格式。两者 limit 均为 1–100，不承诺分页快照。

写入的 If-Match 形如 `"app-<tenant ID>-<application ID>-<version>"`，必须使用目标应用详情刚返回的完整值；它绑定空间和应用，不可复用其他对象的标签。缺失 428、非法格式 400、其他对象或旧版本 412。数据库确认死锁导致整笔事务回滚时，服务端可重新检查并重开一次事务；这不适用于网络结果不明。成功响应返回当前状态及新 ETag；网络结果不明时先重新读取，不自动重发。

409 protected_application 表示管理平台不能停用；409 application_unavailable 表示全局应用停用，当前 owner 无权恢复其全局资格。未知/非当前空间关联统一 404，owner 校验、权威版本及近期 MFA 均在事务内检查，UI 按钮不代替服务端权限。

## 验证与演示保留

新增真实主库/浏览器验收覆盖 owner/普通成员、同空间目标客户端撤销、其他空间/平台会话保留、恢复后旧授权仍拒绝、重新授权成功、版本冲突/并发、MFA、元数据与审计。演示数据不参与这些可变测试，最后使用 scripts/demo check 复验演示主线。

周五基线已固定在标签 `demo-friday-2026-11-13`，对应提交 5d8b46b；源码与原文档单独备份，后续 0.11.0 文档包与 0.10.0 基线分开。当前功能不会声明 Marketstars 已接通。设计与实际证据分别见 ADR 0013 和 validation.md。

后续 OpenAPI 0.12.0 保留上述应用合同并增加组织管理，现共 38 个业务操作。Playground 的“读取最新 ETag”对应用操作须先填目标 application ID，读取该应用的详情版本，支持完整对象绑定 ETag；不会误用空间 authz 版本。
