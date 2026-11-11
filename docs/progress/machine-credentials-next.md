# 下一切片：机器身份与一次性凭证（未实现）

这是后续实现的工作记录，不是当前产品能力。0.13.0 只包含无 secret 的公开 PKCE 客户端。

- ServicePrincipal 归租户所有，与自然人/会话分离；现阶段仅 active tenant owner 管理，不沿用创建者离职后自动销毁的模型。
- 每 credential 独立 OAuth client，client_credentials 唯一 grant，无用户登录/refresh，无平台管理 audience。先固定受控业务 audience/scope，避免开放任意目标及自提权。
- SK 至少 256-bit 随机，只在首次成功创建响应显示一次，业务库不存 SK 明文/可恢复密文，读取/Playground/审计均不恢复秘密。响应丢失须撤销再新建，不复用 public-client 的“重新核对即可激活”语义。
- 创建 pending → 协议登记/核对 → active，未知结果保持本地主库的实际资格。pending/expired/revoked/disabled/orphan 不能签发。撤销先提交权威版本，再清理匹配的协议对象。
- 一名 SP 最多两份 active credential；轮换成功再收短旧凭证重叠期限，失败不得改变旧凭证。到期/撤销都要验证旧 JWT 在示范资源的权威检查中拒绝。
- 当前 BusinessHook 仅接受 user code/refresh。机器分支须按引擎认证过的 client registry 解析 tenant/SP/credential，不能接受 payload tenant/principal hint；用户分支保持原验证及 MFA/授权链。
- 平台管理 API 继续要求 user/MFA，不接受机器 token。提供独立业务资源验证样例，展示 scope/audience/凭证实时撤销，而非只有“拿到 token”或解码 JWT。
- 增加独立 migration、OpenAPI/UI、一次性秘密/丢包/补偿/权限边界/过期/双凭证轮换测试；真实 Hydra 和主库证据与故障注入分别记录。

先完成 0.13.0 全部既有回归/文档/秘密排除并推送，再动这部分代码。不得修改 CI/GCP/部署；不得改动周五演示账户、长期密钥或固定基线。

## 首期建议接口与资源验收（仍未实现）

管理沿用已验证 User/MFA/BFF 边界，在自有 Application 下创建 SP/credential。先用固定 machine:invoke 和 `urn:sso:application:<application ID>`，不给 platform audience 或 platform:write。先实现 tenant owner 集体管理，created_by 仅作历史审计引用；若增加命名 SP owner，则必须另补成员离职/移交不变量。

机器 Hook 在 user business_context_id 校验前独立分支，只按引擎认证 client_id 查询 SP/credential 及应用/空间资格。JWT extra 使用独立 principal_type、SP/credential/application ID 与各层版本，不伪造 user/session/MFA。用户 code/refresh 分支保持原严格逻辑，机器 grant 不提供 refresh/ID token。

需要一个明确标作“业务资源样例”的机器读取接口，按固定前缀与资源 application ID 构造预期 audience，验证签名/issuer/scope/claims 并主库复查版本/状态/到期；另一 application 的 token 必须拒绝。所有平台管理入口仍只接受 user token。资源请求可选择目标资源路径，但不能选择任意 JWKS/issuer 或从未经验证的 header 覆盖 tenant。

凭证初次成功响应返回 AK/SK；业务库不保存可回读 SK。丢失协议创建/响应、SQL 提交不明、浏览器关闭都不能通过 GET/reconcile 重新显示秘密或重新激活 pending credential。重试清理/revoke 后创建新凭证。双凭证重叠应在新密钥确实激活的同一业务事务才限制旧密钥期限，失败保留旧密钥。

单独验证：真实 token 无秘密字段、wrong audience/scope/tenant hint、不接受 public/用户 client 走 M2M、机器 token 不能管平台、pending/orphan拒绝、到期/禁用SP/应用停用/撤销时资源实时拒绝、轮换双密钥与响应丢失、元数据/导出/日志秘密扫描。测试输出仅固定阶段/计数，不打印令牌或 SK；UI截图只截元数据页。

机器资源与 User 管理合同必须分开：现有 BFF/Playground 使用固定 User OAuth2 audience，不应接受任意机器 Bearer 输入、错误地使 User 会话失效，或把所有公开合同路径自动加入 BFF。建议另列 machine-resource OpenAPI/路由（可同进程固定前缀），主库按已验签 machine claims 复查，外部业务资源 SDK 可据此做实时资格核对。平台 API 和其 Playground 仍保持 User/MFA 边界；机器资源示范用自己的合成客户端验收。

一次性凭证创建若在通用 Playground 中被脱敏，应明确限制/引导到凭证专页，避免“创建了但用户从未能保存 SK”的误导。外部 API 的首次创建响应依然是一份秘密交付，必须文档化网络结果不明后撤销再新建的路径，不允许 GET 恢复。
