# 角色策略历史：实施约束

基于已推送 0.21.0 / 8cec1fb，补齐自定义角色策略 create/update/delete 的事务前后快照。此文件为实施前约束，未验收前不表示功能已交付；当前 API 仍 59 项。

- 新增独立 role-policy-history 读取，要求真实 User platform:read、当前有效 owner/roles.manage，主库每次复查。audit.read 委托不获得角色规则；不增加可委托权限。旧 MFA 可读有效元数据，写入继续需要最近 300 秒 MFA 和精确 authz ETag。
- 自定义角色创建、真正修改、删除在同一事务捕获 role code、before/after 规范化策略、提交后 authz_version，并与同一 audit/outbox event 关联。无操作、拒绝、陈旧版本、冲突和回滚均不新增快照。绑定/解绑沿用现有独立绑定历史，不混成策略变化。
- migration 011 只新增表，001–010 原文/摘要不变，不推测回填旧历史。event_id 引用审计事件，清理事件时级联；role_id 为历史引用，不随角色删除。查询必须同时绑定快照与审计的 tenant/role/action/target；删除后仍可按原 role ID 查看，重新使用 code 的新角色不继承旧 ID 历史。
- 对外仅返回固定版本格式、tenant/role、event ID、membership actor、code、action、time、版本和受限 before/after 策略。无邮箱、User/session/grant/proof/token/AK/SK/hash 或任意请求正文。旧 AuditSummary/受控导出 sso.audit.batch.v1 不增加字段，Go 审计 SDK 保持兼容。
- 提议 GET /v1/tenants/{tenant}/roles/{role}/policy-history，limit 1–100、after 为实际同角色/空间事件游标；未知/外部资源与游标 404，未授权优先拒绝。主库事务中完成权限/存在性/分页读取，失败不返回部分数据。此读取不提供用于写入的 ETag。
- UI 所有者可从角色行或角色策略审计事件打开只读历史；删除后的 ID 仍可查。明确记录仅自启用采集后捕获，不代表完整档案，不重建旧规则。分页/取消/换空间/卸载/迟到与未知私密字段防护沿用现有边界；成员目录被 deny 时仅显示 actor ID，不扩大邮箱读取。
- 这是应用层历史记录，没有签名、WORM、防 DB 管理员篡改或长期归档承诺。生产保留/外部消费者由后续独立任务处理；不介入 SRE 的 CI/GCP/部署。

验收包括真实 MySQL 的快照/审计/outbox/版本原子性、空策略/no-op、错误及回滚、并发仅一次真实变更、删除后读取和 code 重用、跨角色/空间游标、当前 owner/委托/失效/旧 MFA；真实 User/BFF/Playground 与严格固定 DTO、未知字段拒绝、读中取消/空间切换和秘密排除。保留演示基线及全部原始凭证；完整资料/离线验收后再记录功能交付。
