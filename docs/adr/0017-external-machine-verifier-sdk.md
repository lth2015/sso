# ADR 0017：外部业务服务的在线机器身份验证器

状态：已采纳并实现于 0.15.0。Go 单元/竞态及真实端到端 SDK/独立服务/客户端验收通过，见 validation.md。基于已推送 fd8c594 / 0.14.0 的主库机器身份资源，不扩大 User BFF 或平台管理范围。

外部业务服务通过 Go SDK 将入站 Bearer 提交给固定配置的 SSO 原点，取得完整验签及主库资格复查后的身份。可信业务资源归属提供预期 tenant/application；请求 header/payload 不选择原点或命名空间。每次验证在线执行，权威不可用时拒绝，不缓存旧身份，不重放业务操作。

默认 HTTPS、禁止重定向/Cookie、总超时与 context、有限响应体、精确 JSON/namespace/scope/audience、no-store 与缓存 Age 检查构成传输/响应边界。错误与日志只保留受控类别，不透传原始正文或令牌。当前本地 HTTP 入口单独显式启用并固定回环路由。

身份元数据不等于任意业务权限。SDK 只提供机器身份验证，具体业务 ACL 由业务服务实现；User 登录/平台管理仍使用原 realm。独立 loopback 服务仅提供 whoami 和匿名合同，真实本地客户端通过 stdin 接收 AK/SK、执行一次交换与调用，输出身份元数据。客户端参考与 SDK 的职责分开，不把秘密管理/token 缓存/轮换引入第一 SDK。

SDK/独立示例使用各自清晰的合同与错误格式，不向 User Playground 自动加入机器 Bearer。固定周五数据与基线保留，CI/GCP/部署仍由 SRE 对应。Go HTTP/TLS 模拟边界和真实 Hydra、主库、独立服务的验收分别记录；不将真实 SSO 身份资源验证冒充外部任意业务 ACL 验收。
