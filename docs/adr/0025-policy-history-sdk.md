# ADR 0025：User 客户端增加所有者策略历史读取

状态：已接受，0.23.0。

为外部程序复用 owner-only 历史接口，扩展既有标准库 useraudit 客户端和私有 stdin CLI，不改服务端、迁移或权限。管理/机器审计继续使用原冻结 v1；策略历史使用独立 sso.role.policy-history.v1 类型与 tenant/role 绑定，不能让 audit.read 自动读取完整规则。

共享同一受限可信 HTTP 读取，协议边界/固定错误/无缓存或自动重试保留；严格 JSON/namespace/规则/动作/游标与取消检查后才交付一页。CLI policy_history 必须 role_id，旧两个 kind 拒绝这个新字段以免默默忽略错用。结果仍由授权调用方保存，工具不暗中持久化输入/输出。

真实独立进程与 HTTP/TLS 模型分开验收；旧 SDK/CLI 兼容及完整文档实际 ZIP 离线编译作为串行交付门槛。实际结果见 ../validation.md。原始约束见 ../progress/policy-history-sdk-next.md；不提供登录、权限缓存、归档或全部 API 绑定，CI/GCP/部署由 SRE 负责。
