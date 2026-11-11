# ADR 0013：空间应用关联与客户端登记元数据

状态：已实现，实际验收见 validation.md。客户端创建/凭证生命周期仍属于后续切片。

应用和 OAuth client 是全局登记对象；tenant_applications 是空间显式授权边界。当前只允许 active owner 查看本空间已存在的关联及其公开客户端元数据，并对该关联停用/恢复。不提供全局目录、任意应用启用、全局状态修改或 secret 读取；平台管理应用保持启用，避免 owner 失去恢复入口。此 owner 边界复用不可委托的管理资格，不增加六项可委托权限。

关联状态写入需近期 MFA 和绑定 tenant/application/version 的强 ETag。复用已有 tenant_applications.security_version，事务内检查 owner/当前 grant/最近认证/版本/全局应用状态。真实变更递增关联版本并记录 audit/outbox；同状态配合当前版本不重复变更。没有全局 tenant authz_version 变更，其他应用/空间无需重新授权。

停用同事务更改关联 active/版本，使当前 tenant/application 的旧 grant/BFF 权威失效。资源和 Hook 比对 grant 已绑定的 enabled_version；恢复再次递增关联版本，因此旧 grant 不会复活。避免在持有空间锁时批量锁定 grant/BFF 行，与签发过程的授权行锁形成环；存量加密会话行按既有到期清理，不承诺即时物理删除。没有注销共享中央/Hydra SID，防止误伤其他应用/空间；资源资格与 token Hook 在主库复查，旧 code/refresh 无法继续签发。恢复递增版本但不复活旧 grant，只接受新的授权。全局应用已暂停时空间 owner 无权绕过。

客户端登记页只读 client_id、精确 redirect URI、scopes、audience、active/version 与 issuer，不调用 Admin 读取 secret，也不宣称完整协议认证配置已可自助维护。客户端列表按字符串 ID 分页，采用独立有界游标校验；其余资源仍使用 UUID 游标。只在 MySQL 1213 已明确整笔事务回滚时重开事务一次，并重新检查权限/近期 MFA/预期版本；网络结果不明不重试。UI 在提交前读取目标 ETag，明确确认范围、无自动写入重试，取消和 context/generation 防止迟到响应覆盖。

无迁移或 CI/GCP 配置变更。演示基线固定为 demo-friday-2026-11-13 / 5d8b46b，源码/文档已分别备份；变更使用独立合成测试数据，演示账户保留并最后复验。公共/BFF/可信安全合同统一 0.11.0，公共业务 34 操作。
