# ADR 0024：所有者专用角色策略历史

状态：提议，计划 0.22.0；尚未实现/验收。当前运行合同仍为 0.21.0 / 59 项，完成后以 validation.md 和功能提交为准。

当前审计保留角色动作/目标与绑定时的 code，但不能还原具体策略前后值。为自定义角色 create/update/delete 增加同事务快照，使用独立 owner-only 读取，不扩大 audit.read 或修改冻结的审计批次格式。角色管理保留 roles.manage 的现有恢复边界，读无需新增近期 MFA，写仍由主库检查现有 owner/MFA/If-Match。

新增 migration 011 的 role_policy_changes 表，以 audit event ID 关联审计/事务 outbox，保存历史 role ID/code、全局授权版本与最多 12 条规范化 before/after policy；每条只允许现有六种 permission 与 allow/deny。角色 ID 不引用当前 roles 行，删除后保留、code 重用不会串接历史。审计事件删除级联清理快照；不改动原迁移或回填未知过去。所有 role 策略真变更必须写成功，任一步失败整体回滚；no-op 与角色绑定沿用原语义。

提议独立 GET /v1/tenants/{tenant}/roles/{role}/policy-history，固定 sso.role.policy-history.v1 格式/namespace，limit/after 有界且验证同角色真实游标。每次在主库事务核验当前 owner/身份/grant，再校验当前角色或已有历史，读取同时约束快照与审计 tenant/target。未知资源/游标 404，普通审计员/机器 realm 不可读取。无邮箱或认证材料、不返回私密正文，也不提供写入 ETag。

UI 从角色及策略审计事件进入，可显示已删除角色的规范化前后规则与 membership actor ID；按现有权限保护邮箱，不自动查询成员资料。历史只表示启用采集后实际捕获的变化，空列表不证明从未改动；没有签名、不可篡改存储、冻结全档或保留期限承诺。

完整实施约束/验收计划见 ../progress/role-policy-history-next.md。完成主库、真实协议/界面和兼容验收后再接受此决策并发布新合同；保留周五基线与 0.21 可用节点，CI/GCP/部署交由 SRE。
