# 安全设计与威胁模型

## 信任与资产

核心资产：密码散列、MFA 种子/恢复码、会话、code/refresh、签名密钥、SK、成员/角色与撤销记录、审计。攻击者包括匿名用户、恶意租户管理员、被攻陷应用、泄露机器凭证持有者，以及误配置运维账号。浏览器/第三方应用、公共协议服务、内网管理与 Hook、权威数据库、云控制面是不同信任区。

| 威胁/入口 | 约束与控制 | 可重复验证 |
| --- | --- | --- |
| 跨租户 IDOR / 混淆代理 | token 固定 tid；真实资源归属；组合 FK；tenant-aware repository | A token/B ID/header 篡改、缓存隔离 |
| 登录 CSRF / code 注入 | state、nonce、S256、交易 cookie、Origin/CSRF、一次性挑战 | 错 state/nonce/verifier、code 重放 |
| XSS / token 外泄 | BFF HttpOnly cookie、严格 CSP、无任意 JS/HTML、输出转义 | CSP/注入/浏览器 storage 无 token |
| 协议/算法混淆 | Hydra 签发；固定 issuer/audience/alg；JWKS 来自预配置 issuer | ID token 调 API、错误 aud/iss/kid/alg |
| Hook 绕过 | 全 grant 实测；内网认证；主库状态与授权上下文校验 | code/refresh/M2M 下拒绝、断网、超时无 token |
| SK 泄露/越权 | 每 credential 独立 client、CSPRNG ≥256bit、只显示一次、scope/aud 上限 | 日志扫描、响应丢失、禁用/过期/orphan |
| 权限提升 | 默认拒绝、显式 deny 优先、委托边界、last owner 保护 | developer 自提权、跨 app admin、并发 owner 删除 |
| 爆破/资源耗尽 | IP/账号/client/租户配额、哈希有界池、请求体上限 | 错误密码洪泛、Redis 故障、连接预算 |
| 重放/回退 | refresh 轮换、恢复码原子消费、MFA 时间片防重用、版本/DR 屏障 | refresh 并发重用、恢复旧快照 |
| 云/内网泄露 | Admin/Hook/SQL 不公网路由、Workload Identity、最小权限与 JIT | ingress/NetworkPolicy、IAM 审计 |
| 供应链 | 固定版本与 digest、校验下载、依赖审查/SBOM/漏洞扫描 | 篡改制品失败；记录例外与维护状态 |
| 审计篡改 | 变更/audit/outbox 同事务、event_id 去重、独立归档和保留策略 | 故障回滚、重复/乱序、脱敏与完整性 |

## HTTP 与交互合同

生产必须 TLS；host-only `__Host-` cookie：Secure、HttpOnly、Path=/、SameSite=Lax，无 Domain。会话 ID 仅保存摘要；CSRF 令牌与会话绑定，校验 Origin。CORS 默认关闭，credentials 不配 wildcard。CSP 默认 default-src 'none'，按页面显式开启同源 script/style/connect，frame-ancestors 'none'、base-uri 'none'、form-action 'self'。设置 HSTS、nosniff、Referrer-Policy: no-referrer。HTML/身份敏感响应 Cache-Control: no-store。

所有输入 schema 校验、body 上限、deadline；统一错误响应 `{code, message, request_id}`，日志不输出 query、body、Authorization、Cookie、密码、SK、token 或 MFA 材料。审计只记受控元数据。日志错误采用 reason code，不能直接回显上游异常正文。

## Hook 与撤销

Hook 必须验证调用方、client registry、active tenant/application、grant 上限、scope/audience 子集。用户流还校验 subject/session/membership、绝对期限和版本；机器流校验 SP/credential 及到期。上下文来自服务器，不能接受 payload tenant_id。未知字段不能成为标准 claims 的覆盖通道。

普通读撤销上限 300+30 秒；高风险在执行事务前读权威状态，依赖失败拒绝。缓存事件仅加速。发布前必须明确 Hook 与禁用事务的在途竞态；不宣传“撤销后绝不返回在途 token”。所有已启用 grant 的拒绝和故障行为需用真实引擎验证。

## 数据与密钥

Argon2id 起点 64MiB/t3/p1，按硬件压测；不得降低成本换吞吐。重置/邀请/验证令牌仅保存摘要并原子消费。TOTP 种子封装加密，密钥托管与轮换覆盖备份解密。JWT keys 与数据库加密 keys 分开管理；不擅自删除仍被引用的旧密钥。Cloud Secret Manager 不等于可以把值写入 Git/终端日志。

## 安全发布门槛

M0：真实 MySQL migration、所有 grant 的 Hook 通过/拒绝/断连、PKCE、JWT/JWKS、refresh 生命周期。M1–M4：多租户/事务/会话/MFA/授权/凭证安全测试，依赖与镜像漏洞处理。M5：外部评审、容量与故障注入、备份恢复、DR 安全屏障及 30 天试运行；不能由本地 demo 替代。

本地 M0 允许仅绑定 loopback 的明文 HTTP、合成身份与临时密钥；不得连生产数据或公开发布。此例外必须在运行说明和 UI 明确标注，不能进入生产配置。
