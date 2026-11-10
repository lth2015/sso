# ADR 0003：授权交易绑定与 M1 数据切片

状态：接受为本地开发实现。Cloud SQL 环境由用户准备；依用户明确要求，本轮不执行云端验证，不宣称云端 HA 已通过。此变更允许继续内部 M1 数据层开发，不等于生产发布。

## 授权上下文

M0 独立实验库新增 tenant_qualifications 和 authorization_contexts。可信同意适配器为每次授权创建随机 256-bit context ID，记录 client、subject、tenant、principal、audience、scope、login_session_id、资格/客户端版本和绝对期限。浏览器 hint 不选择租户。资格复合外键防止上下文引用不存在的 client/tenant/subject 组合。

Hook 事务锁定 client 策略、授权上下文和资格行；主库查不到、资格停用、版本不匹配、过期均拒绝。已签发 ext 只传服务器 context ID 和最小身份字段，不再用 session extra 中的 scope/audience 作为授权权威。三类 grant 的实际请求 scope/audience 也独立校验；发现 Hydra refresh 可能忽略显式越权 scope 参数，因此不能只检查最终 granted scopes。

Hydra 2.3.0 的公开 consent challenge 是编码 flow，完成同意时才生成新的内部 consent ID，不能事先把两者摘要等同。依据固定版本 [VerifyAndInvalidateConsentRequest](https://github.com/ory/hydra/blob/v2.3.0/persistence/sql/persister_consent.go)，本实现保存原 challenge 摘要用于追踪，在首次受认证 authorization_code Hook 中，用行锁和条件 UPDATE 原子绑定内部 consent ID 摘要。刷新不允许初始化绑定；后续所有调用必须匹配已有摘要。另校验 subject、client 和原始 ID token claims 中的 sid。runtime Hook 只有绑定列的 UPDATE 权限，不可改写上下文资格或 tenant。

此初始绑定依赖可信适配器独占 consent accept 权限，以及 Hook 调用方认证。它不能抵抗被攻陷的 Hydra 或可信同意适配器；这两者本来就在签发信任边界内。生产交互还须补齐 browser transaction、CSRF/Origin、一次性状态迁移和真实身份/MFA，不能公开当前测试适配器。

## 会话与密钥的实测合同

- remembered login 复用中央会话；第二个客户端 login skip=true，但仍须独立同意。记住同意后同 client 的 prompt=none 可完成，首次无会话或 max_age=0 时返回 login_required。
- Hook 保留经过数据库验证的 sid 到 ID token 自定义 claims；不能在返回空 ID-token extras 时误删会话标识。
- 实验退出适配器先禁用该中央 sid 下的平台授权上下文，再接受 RP logout。之后 refresh 被拒绝、prompt=none 无法登录。没有把引擎 logout 单独描述为所有 refresh 都已撤销。旧 JWT 仍可在原 TTL 内验签，这不是实时授权。
- 引擎 consent revoke API 单独验证可拒绝后续 refresh。
- 通过 Admin 创建新 RS256 signing key 后，新 M2M token 使用新 kid；公开 JWKS 保留旧公钥，新旧签名均验证通过，且无私钥字段。未提前删除旧 key。生产 KMS、紧急 kid 拒绝、缓存水位和 key 清理策略仍需后续验证。

## M1 的首个切片

业务库 sso_business 与 Hydra、M0 实验库分离。UUIDv7 存 BINARY(16)，tenant-aware repository 参数显式携带租户和操作者。提供事务性租户引导（已有 active User → Tenant/Root/Membership/owner）、组织列表/创建/移动预览与执行、成员暂停。管理入口暂为 active owner，不能把此切片当作已完成 scoped RBAC。

所有相关变更在同一事务写 audit_events 和 outbox_events。移动锁定 tenant 版本，拒绝跨租户、环、root 移动、深度 >16；受控更新闭包并递增 org/authz 版本。暂停成员以 tenant 行锁串行化，保护最后一个 active owner，并递增 membership/authz 版本。全局 User 冻结尚未实现，未来必须同时维护相关租户 owner 不变量，不能用任意 SQL 更新代替业务操作。

根节点使用生成列 root_marker + UNIQUE(tenant_id,root_marker) 约束唯一 NULL parent；用独立 tenant_roots 表保存根引用，避免初始化时的循环 FK。根引用由引导事务维护。M1 当前没有公开修改根引用或删除根的服务方法。

当前 role_bindings 只实现租户级 membership owner 的最小切片；应用、组织作用域、service principal 和委托边界仍待后续迁移与服务层实现。组织移动 dry-run 当前给出节点数、深度和版本；完整权限影响摘要需等组织 scope 绑定落地。已补充内部邀请创建/接受和组织成员关联：邀请保存 SHA256 摘要、24 小时过期，必须匹配 active User 的已验证邮箱；接受原子消费，不隐式授予角色。组织归属也不授予权限。邀请方在接受时仍须具备有效 owner 资格。身份注册/验证页面、邮件发送、outbox 消费 worker、公开业务 OpenAPI/HTTP 与 MFA step-up 尚未交付。

M0 实验资格表与 M1 业务表尚未连通；不把两个独立测试库的通过当作身份平台端到端授权已完成。

## 与 SRE 的边界

用户进一步明确：本轮尽可能完成本地开发和验收，GCP 部署/云端运维由专门 SRE 负责。后续开发不等待 GCP 项目、实例或云凭证；本地迁移、接口合同和运行说明作为交接材料。云端 HA/DR、KMS 与上线 SLO 属于 SRE/联合生产验收，不由本轮代理执行，也不记为本地测试通过。
