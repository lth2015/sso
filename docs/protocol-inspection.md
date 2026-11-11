# 协议登记只读核对

0.18.0 已实现，实际验收证据见 validation.md。针对自有公开客户端与机器凭证，当前 owner 可以显式读取协议登记的受控字段观察；这不是修复任务或周期 worker，不会创建、激活、撤销、删除或收养协议对象，也不会恢复秘密。

公开 User GET 在已有 managed client 或 machine credential 详情后加 `/protocol-status`，需要 platform:read 和主库 active owner/roles.manage。只读没有额外近期 MFA 门槛，身份/会话/版本仍完整检查。请求前确认资源所属空间与所有权，在固定 Admin GET 后再次复查当前资格/资源修订；RPC 期间不持有租户锁，四秒总上下文与三秒 Admin 请求限制。失权/到期拒绝，资源修订改变为 409 observation_changed，依赖错误为 503；不返回部分观察。查询参数与 GET body 拒绝。

响应固定包含资源类型、tenant/application/可空 SP/resource ID、revision、本地状态、profile_state、differences，以及 observed_at/authority_checked_at。matching 只表示 client_id/name/auth method/redirect/grant/response/scope/audience/ownership metadata 这些受控非秘密字段一致，不证明 SK、JWT、端到端业务或完整引擎健康。absent/conflict 保留本地状态；pending 即使 matching 也不激活，遗失/未知 SK 仍需撤销/清理后重建。引擎缺少登记不等于本地主库撤销，已签发 JWT 的权威变更仍通过本地 revoke。

differences 只有固定字段名；没有实际差异值、Admin 原文、AK/SK/hash/registration token。引擎响应有大小上限，必须是一份完整 JSON；未知敏感字段丢弃，超限、截断、尾部第二文档/杂项和错误类型受控拒绝。每次观察记录自己的时间，不能当作持续授权缓存或原子全系统快照。

控制台在客户端专页与机器凭证卡片增加显式只读核对，显示状态、受控差异字段和时间。取消/关闭/资源版本或空间变化后丢弃旧响应，不自动修复或重试。原写入仍需确认、近期 MFA 与精确 ETag。

真实主库、模拟协议边界、真实 Hydra/Bearer/BFF/浏览器及兼容验收记录在 validation.md；实施约束见 progress/protocol-profile-inspection-next.md。CI/GCP/部署继续由 SRE 负责。

验收：`scripts/console protocol-api-test`、`scripts/console protocol-test`，仓储 `scripts/m1 test`，Admin HTTP 投影 `go test ./internal/adapter/hydra`。截图：[公开客户端桌面](ui/protocol-inspection-desktop.png) / [手机](ui/protocol-inspection-mobile.png) / [机器凭证](ui/machine-protocol-inspection.png)，只保留观察元数据。

观察正文的 resource_etag 表示本地登记资源的写入前置条件；观察响应不发送 ETag。观察时间/远端字段可变化，不能把资源版本当作整份观察表示的缓存校验值。所有响应继续 no-store。
