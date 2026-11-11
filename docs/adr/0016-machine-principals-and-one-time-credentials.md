# ADR 0016：机器身份、一次性凭证与独立资源边界

状态：已采纳并实现于 0.14.0。基于 e4184ab / 0.13.0 的自有应用模型；专项、修正后的全部既有回归与完整周五彩排已通过，实际范围见 validation.md。

机器身份属于租户自有 Application，与 User/MFA/session 分离。第一期由租户 active owner 集体管理，created_by 为审计历史而非持续资格来源。SP 可停用/恢复并递增安全版本，旧 token 不随恢复复活；尚未撤销/到期的 credential 可在恢复后重新换取 token。

每 credential 独立 client_credentials-only client，固定 machine:invoke 和 urn:sso:application:<application ID> audience，无平台管理 scope/audience、无登录/ID token/refresh。机器分支按引擎认证 client_id 查主库 SP/credential/应用/空间，User 分支继续要求 business_context_id、MFA 和原授权链。未知/pending/orphan/expired/revoked/disabled 或版本不符均拒绝。

SK 使用至少 256-bit 随机，只在首次成功创建响应交付，不存业务库可恢复副本，不通过 GET/reconcile/日志/审计/Playground 恢复。先提交 pending，再创建并核对固定远端 profile，最后复查 owner/MFA/状态并激活。未知协议/SQL/浏览器回包不能自动重放；pending 不得靠 public-client reconcile 变 active，只能撤销清理再建。API 成功但用户未收妥秘密的 active credential 也需撤销重建。

每 SP 最多两份未过期 active/pending credential。新凭证成功激活时才原子缩短旧 active credential 的过期至最多 24 小时重叠；失败保留旧凭证。API revision 与 token security_version 分离，缩短 expiry 更新 API revision，避免无故使仍在重叠期的 JWT 失效；到期始终按实时主库判断。撤销增加两种版本，物理清理在本地失效之后，不覆盖/删除不匹配的协议对象。

管理 API/Console BFF/Playground 继续使用 User/MFA realm。机器业务资源另列合同与固定路由前缀，只接受目标应用 machine audience，且主库复查签名 claims 对应的资格/版本/到期；不能把机器 Bearer 输入并入 User BFF，不能使 User token 被误当机器凭证。机器 token 不能管理平台，User token 也不能调用机器资源样例。当前示范资源实时拒绝不等于任意外部仅验 JWT 的服务也实时撤销。

机器签发审计单列受控事件与 outbox，不能将 creator membership 冒充每次机器签发的 actor。管理变更继续写现有 User 审计；不记录秘密或 token。审计读取沿用 tenant audit.read，不隐式授予 SP/凭证管理权。

验证必须同时覆盖真实 MySQL/Hydra、真实机器 token/resource、secret 一次性及丢包、配额/双凭证轮换/到期、SP/应用停用与恢复、跨空间/应用/realm、未知/orphan 拒绝、metadata/导出/日志秘密排除、User 回归与固定演示复验。故障注入和真实协议证据分开。CI/GCP/部署仍由 SRE 负责。

## 实现细节与边界

机器 JWT 的主库版本和 credential_expires_at 采用规范十进制字符串，避免引擎中间的浮点 JSON 表达影响精度或严格解码；标准 exp 仍由引擎签发。创建过程中保存于内存的 SP/应用版本绑定必须在激活时复核，不可从 pending GET 重建这个授权快照。公开 Playground 的 createMachineCredential 明确禁止执行，完整合同仍可读，实际 SK 交付仅在专用 UI。

SP 属于租户集体所有者，而非创建者持续个人所有。创建者离开后，新 owner 可以管理；旧 tenant revision 的 JWT 失效，但仍有效凭证可以按当前主库版本换取新 token。没有把创建者 membership 假作机器事件 actor，也没有另行实现命名 SP owner 的转移模型。
