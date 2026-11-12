# 应用准入变更历史

0.26.0 增加只读历史页面和 `listApplicationAccessHistory`。它回答“谁在什么时候改变了访问模式、给哪位成员授权或撤销”，复用 0.25 起已捕获的事务上下文和审计事件，无新迁移。当前 User/BFF 66 项操作，Playground 65 项可执行。

## 控制台与演示

进入「应用接入」，在本空间自有应用行选择「访问历史」。页面每批 20 条，支持下一批、返回首批和重新读取；显示实际动作、操作者成员 ID、目标成员 ID、时间、当时访问模式及独立准入版本。模式变化的目标为应用访问模式，目标成员为 null。成员已停用或应用已停用不改写历史。

先按 [应用准入演示手卡](runbooks/application-access-demo.md) 完成授予/撤销，再打开同一应用的历史，能看到实际成员 ID 与对应事件。已有演示应用的 0.25 事务记录直接可读，不重新生成账号、密码、TOTP、恢复码或授权设置。这里没有恢复授权按钮；重新授权仍在「成员访问」中明确操作，旧 grant 不会复活。

只有当前 owner 可读；单独 audit.read 委托没有这份成员历史。没有 members.read 的 owner 仍能读取记录 ID，页面不关联当前邮箱、姓名或目录。记录是已交付的信息，当前访问资格仍由主库实时检查；不能拿历史当准入票据。

## 公开 API

`GET /v1/tenants/{tenant}/applications/{application}/user-access/history`

需要真实 User Bearer、platform:read、当前 owner 和本空间自有应用；旧 MFA 允许读取。共享应用返回 409 application_unmanaged，未知/跨 namespace 对象返回 404；当前授权或会话失效返回 401，当前非 owner 返回 403。User 和机器 realm 独立。

| 字段 | 语义 |
| --- | --- |
| format | 固定 sso.application.user-access-history.v1 |
| tenant_id / application_id | 固定当前请求 namespace |
| items | id、actor_membership_id、membership_id、action、occurred_at、mode、version |
| membership_id | 模式事件为 null；授予/撤销为当时目标成员 ID |
| action | application.user_access.mode_changed / granted / revoked |
| version | 正 signed-int64 十进制字符串，独立 User 准入修订版本 |
| next_cursor | 下一批使用的事件 ID，结束为 null |

limit 默认 50、范围 1–100，按事件 ID 升序；after 必须属于同空间同应用的已捕获事件。显式全零/未知/其他应用游标返回 404，省略 after 读取首批。GET 不允许正文、空值、重复或额外查询。无 HTTP ETag，所有响应 no-store，不承诺跨页冻结快照。完整字段、输入示例及错误见 [OpenAPI](../api/openapi.yaml) / [API 参考](api-reference.md)；控制台 Playground 可实际读取。

## 存储、安全和边界

012 的准入变化原本就与策略、显式条目、版本、audit/outbox 同事务记录。本接口仅投影已存在的上下文，不推测或回填历史，不改变原写入、令牌签发/刷新和撤销逻辑。读取绑定 context/audit 两边的 tenant/application；未知动作、畸形目标/版本或错 namespace 整页失败，不交付部分记录。读取不依赖可能被消费的 outbox，也不读取账户/MFA/秘密或原始事件 payload。

客户端验证固定字段、namespace、ID/升序游标、时间、版本字符串和动作/目标语义后显示；读取开始就清旧页，失败后无旧成功结果，取消/关闭/换空间隔离迟到响应。原管理/机器审计批次 v1 和 Go SDK 不增加字段；普通审计仍只看到应用目标，详细成员历史走当前 owner 专用接口。

空列表只表示没有捕获到记录，不证明应用从未改动；记录不能防止数据库管理员改写，不提供签名、WORM、全档归档、跨页快照或权限恢复。CI/GCP/部署由 SRE 负责。实现和真实验收见 [ADR 0028](adr/0028-application-access-history.md) / [验证记录](validation.md)。
