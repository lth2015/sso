# 自定义角色策略历史

0.22.0 已实现，最终交付证据见 validation.md。新增所有者专用的策略历史读取，公共 User/BFF 合同共 60 项，通用 Playground 执行其中 59 项；一次性 SK 创建仍走专页。记录自定义角色创建、真实规则修改和删除的规范化 before/after，不扩大现有六种可委托权限或角色管理资格。

## 读取与权限

`GET /v1/tenants/{tenant}/roles/{role}/policy-history?limit=20&after=...` 使用真实 User platform:read 和当前有效 owner/roles.manage。audit.read 委托、机器 Bearer、仅 platform:write 都不能读取。有效的旧 MFA 可以读；角色写入仍要求最近 300 秒 MFA，在存储事务内重新检查，即使可信调用者没有主动设置 Recent 标记。

每次在主库事务内核验当前身份、session/grant、空间及 owner，之后读取同一空间/角色的记录。limit 默认 50、范围 1–100；after 必须是同一角色和空间中实际捕获的事件 ID，未知/外部/其他角色游标 404。重复/未知/空查询和 GET body 拒绝。按 event ID 递增，不保证跨页冻结快照或按墙钟严格排序。此读取不提供写入 ETag；修改角色仍先读当前角色集合/权限的 authz ETag，不能使用历史版本直接提交。

```json
{
  "format": "sso.role.policy-history.v1",
  "tenant_id": "11111111111111111111111111111111",
  "role_id": "22222222222222222222222222222222",
  "items": [{
    "id": "33333333333333333333333333333333",
    "actor_membership_id": "44444444444444444444444444444444",
    "role_code": "audit_viewer",
    "action": "role.created",
    "occurred_at": "2026-11-11T23:00:00Z",
    "authz_version": 2,
    "before": [],
    "after": [{"permission": "audit.read", "effect": "allow"}]
  }],
  "next_cursor": null
}
```

以上 ID 仅为格式示例。每个事件的 authz_version 是当次提交后的租户授权版本，不是当前角色写入前置条件。策略只允许原六种 permission 和 allow/deny，每组最多 12 条，按 permission/effect 规范排序、去重校验；空规则是 `[]`。同权限同时允许/拒绝时，运行授权仍显式拒绝优先。没有邮箱、User/session/grant ID、proof/token/AK/SK/hash、私密正文或任意附加字段。

## 捕获、删除和边界

migration 011 只新增 role_policy_changes，不修改 001–010，不推测回填。角色真正变更与授权版本递增、原 audit event、原事务 outbox 和快照共用事务；任一步失败整体回滚。相同规范规则 no-op、拒绝/过时版本/冲突/失败均不产生快照；角色绑定/解绑继续使用已有绑定事件，不被伪装成规则变更。

快照使用历史 role ID/code，并同时约束审计 event 的 tenant/target；不依赖当前 roles 行还原规则。删除角色后仍可用原 ID 读取，后来复用 code 的新角色拥有新 ID 和独立历史。当前角色尚无捕获记录返回空页，未知角色且无历史返回 404；内置 owner 策略不可修改，因此其旧历史不会凭空补齐。

记录只表示启用采集后实际捕获的变化，空页不证明从未改动；旧服务、直接数据库操作或历史数据不能靠当前规则推断过去。它是应用层记录，没有签名、WORM、DB 管理员防篡改或长期完整档案保证；审计事件清理时关联快照级联删除，正式保留/归档策略仍属后续。已有 AuditSummary、绑定历史和 sso.audit.batch.v1 导出投影不增加字段，Go User 审计 SDK 保持兼容。

## 控制台与验收

所有者在「角色与权限」的自定义角色行选择「查看策略历史」，也可从审计中的角色创建/修改/删除事件进入，查看已删除角色。每批最多 20 项，支持下一批/返回首批/显式重读；同时显示前后六种能力和 membership actor ID，不自动查邮箱。目录读取被 deny 时仍保留 owner 的历史恢复入口。

读取中可关闭；初始即处于加载态，数据到达前不能重读。AbortController、请求代数、当前 URL context 和卸载隔离取消/重开/换空间的迟到响应。开始重读先清除旧数据，异常/未知私密字段不保留先前成功；真实 401 进入原重新授权流程。此界面没有恢复/重放策略的写操作，也不将读取成功缓存为权限。

命令：`scripts/console policy-history-api-test`、`scripts/console policy-history-test`、`scripts/m1 test` 和 `node --test tests/policy-history/model.test.mjs`。模型/传输单元与真实 MySQL、真实 User PKCE/Bearer、BFF/UI 分别记录；真实 UI 通过原 API 一次创建和 20 次实际策略替换生成 21 条初始分页记录，随后删除产生第 22 条记录，没有虚构历史行。角色写入失败注入使用连接真实 MySQL 的测试 driver，在快照 INSERT 处拒绝，验证此前实际规则/版本/audit/outbox 全部回滚。详细结果见 validation.md / ADR 0024。

截图仅有规则/身份元数据：[桌面](ui/role-policy-history-desktop.png) / [手机](ui/role-policy-history-mobile.png) / [删除后历史](ui/role-policy-history-deleted.png)。CI/GCP/部署继续由 SRE 负责。
