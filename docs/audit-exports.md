# 受控审计批次导出

本切片提供按权限读取并下载 JSON 审计批次的完整 API/UI。每批最多 100 条，管理操作与机器签发独立选择；读取使用当前 User 的 audit.read，支持只读审计委托。机器 Bearer 的权限仍限于其应用资源。

## 控制台

在「审计」中找到「导出审计批次」，选择管理操作或机器签发、25/50/100 条，然后生成首批预览。预览显示条数、权限版本、生成时间和是否有下一批，可展开 JSON。下载时再次读取并复核当前资格，文件以这次读取为准；旧预览不直接转成下载。用「读取下一批」继续同一类型的游标。

切换类型、条数、空间、授权上下文或页面会清空旧批次。读取中可以取消；迟到响应不触发下载。下载文件名只由固定类型、空间 ID 和 UTC 时间组成；不写浏览器存储，不自动重复请求。

## 公共 API 与合同

`GET /v1/tenants/{tenant}/audit-exports`，User `platform:read` 和当前主库 `audit.read`。无写入、ETag 前置条件或额外近期 MFA 要求；User 会话/grant/MFA 因素及角色资格仍完整复查。只读角色可以导出自己有资格读取的审计，不获得成员邮箱或凭证管理权。

| 参数 | 范围 |
| --- | --- |
| kind | management（缺省）或 machine |
| limit | 1–100，默认 50 |
| after | 上批 next_cursor；必须是当前空间与类型的已有事件 ID |

重复/未知参数、不合法类型/limit/cursor、GET body 为 400；未知/外空间/跨类型游标为 404；身份失效 401、权限/scope 不足 403、权威暂不可用 503。以带原空间 token 请求其他空间仍返回 404，即使账号属于两处。完整字段和错误见 [公共 OpenAPI](../api/openapi.yaml) 与 [API 参考](api-reference.md)。User 合同现在 57 项，Playground 可执行其中 56 项；一次性 SK 创建仍使用专用页面。

响应 format 固定为 `sso.audit.batch.v1`，包含 kind、tenant_id、generated_at、authz_version、items 与 next_cursor。items 为空时是 []；存在下一批时 next_cursor 为本批最后一条 ID。

管理记录仅有事件 ID、actor membership ID、受控 action、target ID、occurred_at，以及三个可空角色绑定快照字段。机器记录只有事件/应用/SP/credential ID、token.client_credentials 与时间，不伪造创建者为人类 actor。导出使用独立固定 DTO/字段投影，列表模型未来增加字段不会自动进入文件。没有请求正文/头、邮箱、口令、SK、邀请 proof、token、MFA/恢复码或任意 payload。

## 数据与一致性

每次读取在主库事务内检查当前权限，游标同时绑定租户和来源。权限版本/成员状态变化使旧 grant 失效；前端下载动作需要重新读取成功，权限拒绝时清除预览，不保存旧文件。客户端也检查固定 schema、类型、namespace、数量、游标和字段白名单。

批次记录单次主库读取的资格版本和时间，分页按 UUID keyset，后续新事件可能继续追加。连续批次用于受控读取；持久归档、签名存证、完整历史保留及审计消费仍是独立后续能力。本地验收、模拟错误/迟到响应与实际角色/空间隔离结果分别记录在 validation.md。
