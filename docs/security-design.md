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


## 本地可信 SSO 的实际控制

已执行的控制与测试以 [ADR 0005](adr/0005-trusted-login-mfa-bff.md) 和 [验证记录](validation.md) 为准。真实业务登录强制 MFA；密码重置与全设备退出递增 User 版本，旧证明、旧 grant、旧 JWT 及 refresh 在权威检查被拒绝。示范资源每次读主库资格，当前不使用普通读的目标缓存窗口；在途请求仍按数据库检查的线性化时刻决定。

BFF key、OAuth secret、数据库账号按应用分开，视图与 CHECK OPTION 限定自己状态；BFF 无权读取密码、MFA 种子或其他 client token，审计只能通过限定 routine 追加。BFF 不连接核心服务网络，只能通过窄代理访问公开协议白名单。宿主 loopback Admin 仅供可信本地操作者，不能作为生产 ingress。

本地 HTTP 的 `local_` host-only cookie 不设置 Secure；生产强制 TLS/Secure/__Host- 的要求仍有效。限流目前是进程内有界实现，没有 Redis 分布式保证。遗失认证器的受控恢复、正式凭证管理、审计归档及完整依赖安全审查仍待实现。


公共业务 API 已加入独立 Bearer scope/audience、最近 300 秒 MFA、租户路径与 token 一致性、事务内资格锁和 token 到期复查。普通成员无 owner 管理权，组织归属和邀请均不隐式提权。最近 MFA、并发版本和撤销竞态均用真实 token/数据库检查；HTTP 错误采用 RFC 9457，不回显凭证或上游正文。公开文档无需凭证，私有 Hook/Admin 不因此开放；具体合同与剩余生产要求见 ADR 0006。


真实控制台采用每标签页 context + 对应 HttpOnly cookie，X-Console-Context 本身不能认证。读请求的 context/page/generation 和取消信号共同防止迟到资料覆盖新空间，所有写方法都校验 Origin/CSRF。BFF 只允许公共 OpenAPI 中的固定方法/路径，拒绝客户端 Bearer 覆盖，转发到固定目的地；只回传有界 JSON 和受控头。读取成员邮箱仅经 API members.read 授权，SQL 角色不能直接查询 login_aliases。策略与代码证据见 ADR 0007/validation.md。


租户 RBAC 已实施：六种操作权限可委托，显式 deny 优先于 allow 与 owner 的操作权限。roles.manage 是不可委托/拒绝的 owner 恢复边界；内置 owner 不可改删，受委托成员管理员不能停用/恢复 owner。策略/绑定修改及版本/审计/outbox 同事务提交，旧 grant 全部失效；并发移除保留最后 active owner。邀请接受重新检查创建者当前 invitations.manage。嵌套策略输入有字段、重复键、深度和大小边界。成员目录权限包含邮箱信息，控制台明确显示该含义。当前仅租户范围；组织子树/应用/资源委托与完整政策快照归档仍待实施。见 ADR 0008。


Playground 沿用固定 BFF 目标/路径白名单、cookie/context、Origin/CSRF 及真实 API 主库授权。合同副本匿名可读，不增加业务匿名接口或任意 URL/Authorization header 输入。写入要求预览与明确确认，单次转发，不自动重试；页面清理/中断不能保证写入未提交。响应凭证字段/JWT 与 cURL 模板中的 proof 脱敏，输入和结果仅页面内存保存。离线文档 HTML 禁止脚本/表单/网络资源；导出仅包含文档/公开合同，SHA-256 清单用于完整性校验，不构成发布者签名。


个人安全管理见 ADR 0010：恢复码重生成必须通过中央会话/浏览器/CSRF、当前密码和新的 TOTP，不能用恢复码或邮箱证明替代。User/MFA 版本与旧会话/grant/BFF 撤销、身份审计及协议清理队列同事务提交，码只在提交后一次返回。恢复码重生成不改变认证器密文；分阶段因子更换已由 ADR 0011 实施，遗失认证器的受控恢复尚未交付。

认证器更换使用独立请求表及暂存 AAD，先验证密码/旧 TOTP，再验证新 TOTP；最终提交前旧因素与权限有效。确认同事务替换因素/恢复码、递增安全版本和撤销全部旧权限，取消/确认并发仅一次提交。页面和独立可信合同不经公共 API/BFF 传递秘密，确认网络结果不明时清空并重新登录核对。见 ADR 0011。

个人安全事件查询见 ADR 0012：User 来自有效 grant，主库事务重新校验后只读取本人的受控事件；游标查找也限定当前 User，未知/他人/隐藏类型一律 404。响应只含独立事件 ID/action/time，私有 target_id、自由 payload、邮箱、IP 和验证材料不对外开放。无需租户 audit.read，owner 权限也不能查看其他用户。此列表不构成完整安全监控或不可变归档。

应用关联管理见 ADR 0013：owner 和近期 MFA 在主库事务内检查，ETag 绑定 tenant/application/version；只允许既有关联，平台管理入口保留，全局暂停不能绕过。停用撤销此空间/应用的 grant/BFF，恢复不复活旧授权，不注销共享中央/Hydra SID。客户端配置仅读登记元数据，无 Admin secret/token 读取入口。测试使用独立合成数据，固定演示基线与私有资料保留。

## 组织生命周期安全

组织直属成员读取需 organizations.read 与 members.read 同时有效，显式 deny 优先，禁止以组织结构读取推导成员邮箱访问。新写入在事务内强制近期 MFA、租户权限/资格和版本。根不可重命名/删除，任何子组织或成员引用均阻止删除；全租户串行锁与复合外键保护闭包/边界，审计和 outbox 同事务。重命名不变权限，删除与真实归属移除增加租户授权版本；不撤销 membership 或 owner/角色。详见 ADR 0014。

## 公开客户端登记与隔离

应用所有权与关联启用分离，关联 owner 不能改写共享/其他空间的全局客户端。公开 PKCE profile 固定 grant/scope/audience/auth 方式，URI 不被平台抓取；状态/owner/MFA/ETag 在主库复查。pending/orphan 不激活，reconcile 先读取且只采用精确所有权/profile 匹配；撤销的 client 版本使所有旧 grant 失效，不批量修改共享 SID。私有协议错误只记录受控字段名与请求 ID，不返回上游正文或秘密。控制台/Playground 不接收 Admin 目标/凭证。见 ADR 0015 和 managed-clients.md。

## 机器身份切片

机器身份 0.14.0：pending→私有协议核对→主库复查激活；SK 仅首次成功响应，业务库不保存可恢复副本。双凭证最多 24 小时重叠，撤销先本地失效；SP/应用恢复不复活旧 JWT。机器审计不伪造人类 actor。独立资源检查主库权威，不能据此保证外部离线验签服务实时撤销。详见 machine-credentials.md。

## 审计批次导出 0.16.0

导出沿用 User platform:read 和主库 audit.read，不接受机器 JWT、跨租户覆盖、任意字段或原始 payload。管理事件仅投影受控 ID/action/time 与角色绑定快照；机器事件仅有应用/SP/credential ID 和签发时间，不借用创建者的人类身份。外空间、跨来源及未知游标统一 404；请求/响应均有单批边界，GET 无写入副作用。前端在每次下载重新获取并验证固定字段/namespace/数量/游标，取消与上下文改变后的迟到响应不能触发文件保存。下载文件仍含受控身份标识，按本空间审计数据管理；本切片不提供签名存证、完整历史保留或持久归档。见 ADR 0018。

## 邀请 proof 更新 0.17.0

新随机 256-bit proof 仅一次返回，数据库保存 SHA-256 摘要；元数据/ETag/审计不恢复 proof。新写入自身强制近期 MFA，不信任内部调用者 Recent=false。invitations.manage、资源/租户/修订版本和当前角色在事务复查，禁止更换目标邮箱、越空间、重开 accepted/revoked；并发更新只一项成功，接受与更新按同一租户锁串行化。初始查找之后旧 digest 被替换也受控拒绝。更新由当前管理者承担接受时需复查的资格，不沿用失权创建者。浏览器专用结果不写 storage、关闭/空间变化后丢弃迟到结果，写入丢包不重试。Playground 沿用通用 proof 脱敏，不负责安全转交。见 ADR 0019。

## 协议只读观察 0.18.0

User platform:read 和当前 owner/roles.manage 限定自有资源，初始无资格不访问 Admin，RPC 后失权/版本改变/到期均拒绝返回早先数据。四秒总期限/三秒固定私有目标请求，单份完整 JSON 与 1 MiB 上限；未知敏感字段不进入投影，超限/截断/尾部第二文档/类型错误返回固定故障。仅输出九种受控差异字段名，不输出实际值、AK/SK/hash/registration token；GET 不写审计/outbox。matching 不证明秘密或业务可用，不激活 pending、不恢复已撤销/过期资格。取消、关闭、资源刷新与空间变化丢弃旧观察。见 ADR 0020。

## User 审计客户端 0.19.0

User scope/audit.read、会话/grant/角色与 tenant 始终由 API 主库复查，客户端不保存成功权限；机器事件来源仍要求 User Bearer。HTTPS 原点由可信配置给出，固定本地回环例外需要显式启用；禁止重定向/Cookie，最多五秒和 256 KiB，拒绝缓存/压缩/未知字段/重复键/多份 JSON/错 namespace。失败返回固定类别与零批次，不透出原始响应或网络错误。CLI 不接受 argv/env token，不持久化输入或输出；成功输出是已授权审计数据，接入方负责后续保存。真实 realm/权限与模拟传输边界分别验收，见 user-audit-client.md。

## 角色选择器隔离 0.20.0

固定 DTO/升序唯一 ID/推进游标/一致 authz ETag 限定可提交选择；目标切换、读取失败和版本异常先清除旧绑定/角色。关闭/新请求/组件卸载中止读取，URL 当前 context 复核与主页面 context/revision key 拒绝迟到结果。401 回到现有重新授权流程，members.read 被 deny 的 owner 不列举目录，只管理自己。写入仍需服务端当前 owner、近期 MFA、ETag，客户端防双击不替代权威检查。见 role-choice-pagination.md。

## 组织关联选择边界 0.21.0

目录页是受控选择元数据，authz 与 org ETag 分开且不能互作写入前置条件；最终 PUT 使用成员页 authz ETag，由主库复查有效目标、organizations.write 与近期 MFA。字段/根/字符串 ID/游标/大小/版本异常、依赖故障清除选择；401 重新授权。用户变更目标/新页后需重新确认，提交同步单发、不能关闭；已提交丢包不自动重放，先核对实际直属成员/审计。普通委托不取得角色权限，组织归属不授予角色。见 organization-assignment.md。

## 策略历史与隐私 0.22.0

完整角色规则沿用当前 owner/roles.manage 边界，不因 audit.read 委托而开放；每次主库核验当前身份/会话/grant/空间/owner，旧 MFA 可读。角色写入在存储层强制近期 MFA，快照/规则/版本/audit/outbox 同事务，失败不留下部分变化。输出只有静态策略/历史 code/membership actor/time/version，拒绝未知私密字段，不查邮箱；现有冻结审计批次不扩字段。新增表只关联原事件并按历史 ID 保留，双 namespace/target 查询防串空间，读取失败无部分页。没有签名/WORM或DB管理员防篡改承诺，见 role-policy-history.md。

## User 策略历史客户端边界（0.23.0）

PolicyHistory 逐次使用 User Bearer/当前 owner，不能以 audit.read、旧成功记录或机器 Bearer 替代。与原批次共享可信 HTTPS/固定本地回环例外、无 Cookie/重定向/缓存/SDK 重试及受限正文；精确字段/规范 before/after、namespace/游标验证和消费前取消复查后才交付整页。CLI policy_history 必须 role_id，旧 kind 拒绝该字段，令牌仍只经 stdin。工具没有登录、历史恢复写入或存证签名，授权结果后续保留由接入方控制。详见 user-audit-client.md / ADR 0025 / validation.md。

## 上级目录与移动预览绑定（0.24.0）

源行与独立分页目录使用同一 org ETag，固定 DTO/根/ID/游标和预览四字段安全数值验证；选择/页/错误/context 变化清除旧预览和确认。主库在实际提交重新检查权限/MFA/版本/循环/深度；预览不是授权票据，原生 API 契约不变。取消/卸载守卫隔离迟到响应，未知写入不自动重试；固定演示夹具保护保持。见 organization-hierarchy.md / ADR 0026 / validation.md。
