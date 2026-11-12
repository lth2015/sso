# ADR 0028：应用准入变更历史

状态：已接受并实现于 0.26.0，保留 e958607 / 0.25.0 演示基线；实际证据见 [验证记录](../validation.md)。

复用 012 已有的 application_user_access_changes 与同 event 的 audit_events，不修改迁移或原准入写入/撤销语义。只读取实际已捕获的模式、授权、撤销事件；不回填、不恢复权限，不从当前成员/应用状态改写历史。记录展示事件、操作者成员 ID、目标成员 ID（模式事件为 null）、动作、时间、当时模式和独立 User 准入版本。没有邮箱、秘密或完整 audit payload。

新增 GET /v1/tenants/{tenant}/applications/{application}/user-access/history，当前有效 User、platform:read、当前 owner 且应用为本空间自有应用；旧 MFA 可以读取，普通 audit.read 委托不因此取得成员名单。应用停用后仍可读取；共享应用与其他 namespace 继续拒绝。事件与上下文的 tenant/app 必须匹配，畸形事件整页失败，不交付部分结果。

固定格式 sso.application.user-access-history.v1；最多 100、默认 50，按事件 ID 升序分页，after 必须属于同 tenant/application 的已记录事件，显式全零游标与缺省不同。无跨页冻结快照或 HTTP ETag，历史不是当前准入决定。原审计 batch v1 和 Go SDK 投影保持。

控制台自有应用行新增只读历史入口，每批 20 项，支持下一批/首批/重读。严格 DTO/ID/时间/版本字符串与动作语义验证，读取取消、关闭/换空间/迟到响应隔离；开始重读即清旧页，异常不显示旧成功结果。无 members.read 的 owner 可读取历史 ID，不泄露目录邮箱。演示手卡在授权/撤销后查看真实记录，原两空间/账号/凭证和新增应用演示均保留。

验收包含真实 MySQL 的捕获/no-op/分页/只读/空间/owner/损坏行，真实 Bearer/scope/MFA/机器 realm，真实 UI 分页/取消/故障/当前权限撤销，OpenAPI/Playground 和保留演示。CI/GCP/部署仍由 SRE 负责；不宣称签名/WORM/完整归档或 scoped RBAC 已完成。
