# ADR 0006：公共业务 API、事务内授权和 OpenAPI

日期：2026-11-10。状态：本地外部接入参考实现；公网部署由 SRE 负责。

## 公共接入边界

新增独立 `platform-api`，本地固定 origin `http://api.localhost:8083`，提供 18 个业务操作及公开文档。它使用 Authorization Bearer access token，不接受中央 cookie 或 BFF cookie 作为 API 凭证。Hydra 仍是唯一签发者，平台 audience 为 `https://api.sso.local`，与示范应用的 audience 分开。固定 issuer/JWKS 目标、RS256、有效期、client、scope、subject、业务 grant/session/member/tenant 字段全部检查；ID token 不可调用资源 API。

读取需要 `platform:read`，写入需要 `platform:write`；所有写入，包括组织移动预览，要求中央会话认证时间距现在不超过 300 秒，且第二因素为 otp 或一次性恢复码。refresh 不更新 auth_time，过期后需重新走 `max_age=0` 的密码/MFA 流程。不得将旧会话的刷新当作 step-up。

服务端/原生客户端可直接调用；浏览器应用通过 BFF 接入。没有 wildcard CORS，也不因 Cookie、Origin、X-Forwarded-For 或用户提供的 actor/tenant 字段建立身份。租户资源路径必须与 token 固定 tenant 相同，即使 User 属于另一个空间也返回 404；切换空间需新授权。`X-Tenant-ID` 不可改变上下文。

## 事务内权限

验签后从主库检查当前授权；每个数据操作的事务再次锁定和复查 grant、完整交互、User、MFA、中央 Session、Membership、Tenant、client、Application 和租户启用关系及版本。写入还检查 token 到期和最近 MFA。Owner 管理仓储携带经过验证的 `Authorization`，在原有 tenant 行锁内复查，不能只依赖 HTTP 中间件检查时的结论。

组织、成员、邀请、审计管理暂为 active owner 权限，不能据此声称完整 scoped RBAC。普通 active 成员可读取自己的空间概要和自己的身份/会话；加入组织不会增加管理权限。原有 last-owner、组合 FK、闭包树和 audit/outbox 事务约束继续有效。

成员停用/恢复、组织归属和组织移动递增 tenant 授权版本，会使该空间原有 grant 失效，包括执行变更的调用者；调用者须重新授权，不会把旧 grant 原地升级。组织创建只递增 org_version。所有版本来自事务内主库，不使用副本或缓存继续授权。

## 交付的业务能力

User 可列出自己 active 空间，创建新空间（最多 20 个 active owned 空间），列出/撤销自己的中央会话。新空间同事务创建 root、Membership、owner、audit/outbox，并启用第一方平台 Application；其他应用不隐式启用。新增空间仍需独立 token 授权。

Owner 可列出成员、停用/恢复成员，创建组织、预览/执行移动、关联组织成员，创建/列出/撤销邀请，查询审计。邀请绑定已验证邮箱，摘要保存，一次性消费，只授予 Membership；创建响应只返回一次 proof，列表和审计不返回 proof。此阶段由调用者安全转交，不发送外部邮件。撤销将 consumed_at 置为关闭状态，列表用 closed 同时表示已接受/已撤销，具体动作由审计区分。邀请接受还要求创建者仍为 active owner。

撤销单个中央会话只允许本 User。事务先关闭业务会话并记录 identity event，再入队按 sid 定位的 Hydra 清理；业务资格立即拒绝，协议清理可退避重试。不返回 cookie 摘要、CSRF、token 或 MFA 材料。

## 合同与兼容性

`api/openapi.yaml` 是公共业务合同，JSON 同步版本用于集成和测试。原 `api/openapi.yaml` 的 M0 内部实验合同原样保留为 `api/m0-internal.openapi.yaml`；账户浏览器接口和可信 SSO 交互分别保留独立合同，不能混作公共 Bearer 管理 API。

`GET /docs` 从嵌入的 OpenAPI 生成接口列表；`GET /openapi.yaml` 和 `/openapi.json` 无需凭证。生成脚本 `--check` 检查两个文件是否同步；浏览器/原生客户端验收对实际请求和响应执行 JSON Schema 校验，并检查 18 个业务操作均被覆盖。

列表使用 ascending UUID keyset，limit 默认 50、最大 100，next_cursor 为空表示当前页结束。它不是事务快照，也不承诺跨事务的事件提交时间顺序。组织修改使用 `If-Match: "org-N"`；成员/组织归属使用 `"authz-N"`。缺失 428，版本过期 412；并发同版本最多一项修改成功。当前未实现通用 Idempotency-Key；不能对创建操作盲目自动重试，重复 slug 返回 409。

错误采用 RFC 9457 `application/problem+json`，附固定 code、HTTP status、request_id 与 X-Request-ID，401/不足 scope 附 Bearer challenge。JSON body 最大 4096 字节，拒绝重复/未知字段、嵌套容器、非法 UTF-8、多 JSON 文档和不支持的媒体类型；查询参数白名单、分页上限、8 秒 deadline、并发容量与进程内 IP/User 限流构成当前本地保护。

## 未冻结的生产要求

本地固定域名/明文/loopback、开发 SQL 账号和单进程限流不得直接当作生产配置。正式域名、TLS、issuer/JWKS 接线、ingress、专用最小权限数据库账号、分布式限流、监控与容量由后续实施和 SRE 验收。没有部署公网、动态第三方 client 注册、通用机器身份凭证或完整控制台；已有 M0 机器 grant 仍是独立实验，不能调用此用户管理 API。
