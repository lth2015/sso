# ADR 0005：可信登录、MFA、中央会话与 BFF 边界

日期：2026-11-10。状态：本地参考实现，生产要求另行验收。承接 ADR 0003/0004，原始设计文档保持不变。

## 决定及实际行为

Hydra 仍是唯一 OAuth/OIDC 签发者。新增独立 `hydra_sso` 协议库与本地业务引擎，issuer 固定为 `http://auth.localhost:4464/`；原 M0 合成身份实验继续使用原库和 4444/4445。业务登录读取已验证 User 的 Argon2id 凭证，完成密码和 MFA 后才调用 Hydra Admin 接受 challenge。没有 password grant 或本地直接签 token 的接口。

`login_interactions` 将 challenge、浏览器摘要、CSRF 摘要、client、scope、audience、User 安全版本、MFA 版本、认证时间和中央会话绑定在同一记录。challenge 用独立 flow key 加密，5 分钟过期，每次交互最多 5 次失败。密码证明携带读取时的安全版本，提交时事务复查，避免密码重置或全设备退出期间的旧证明建立新会话。

所有本地账户首次登录必须设置 TOTP，因为当前注册流程均创建 owner。TOTP 使用 RFC 6238 SHA-1、6 位、30 秒、±1 时间片；数据库锁定并保存最大已用时间片，拒绝并发重放。种子以独立 MFA key 加密；10 个 128 位恢复码只返回一次，数据库只保存绑定 User 的摘要，使用与认证状态更新同事务。密码恢复保留 MFA；遗失种子和全部恢复码时，当前没有绕过或人工清除接口。

中央会话使用 256 位随机 cookie，数据库只存摘要；绝对期限 24 小时、空闲期限 8 小时。会话记录认证时间与方法，复用会话不会刷新原始认证时间；`max_age=0` 要求重新认证。每次授权单独选择租户，事务复查 active Membership、Tenant、Application、client 与租户启用关系；两个标签页不会通过共享“当前租户”覆盖授权。`prompt=none` 缺少登录时返回 `login_required`，无法唯一确定租户或没有可复用同意时返回 `interaction_required`。

授权先写入 `submitting`，再调用 Admin，最后标为 `complete`；远程调用结果不确定时终止该交互并要求从应用重试。业务 Hook 只接受 authorization_code/refresh_token，并读取业务主库，复查 User、MFA、Session、Membership、Tenant、client、Application 和启用关系的状态及版本。首次 code Hook 将 Hydra 内部授权链 ID 原子绑定到 business grant；refresh 必须沿用同一链、subject、sid、scope、audience 和租户。

## BFF 与数据库、网络权限

两个示范应用位于独立 origin，各自使用 OAuth client、secret、加密 key、浏览器 cookie、state/nonce/S256 PKCE。使用锁定版本的 go-oidc/x/oauth2 验证 issuer、RS256、audience、nonce、MFA acr/amr、access token 的 client/scope/业务字段。ID token 不能作为资源 access token。token、verifier 和 nonce 只保存在服务端加密状态，不进入浏览器存储或 JSON 响应。

BFF 会话绝对 8 小时、空闲 30 分钟。refresh 在会话行锁内串行；成功后原子保存新 token，失败或结果不确定时关闭该 BFF 会话与业务 grant，要求重新登录。每次资源请求除验签外还查询 `GrantCurrent` 主库资格；请求头 `X-Tenant-ID` 不能切换租户。两个示范 client 属于同一个 demo Application/audience，证明两个接入方和租户绑定，不代表完整跨产品 RBAC。

每个 BFF 使用独立 MySQL 账号和独立状态 schema，获得带 client 条件与 CHECK OPTION 的可更新视图。它不能读取原表中的密码、MFA 密文、中央 cookie 摘要或另一 client 的 token。资格视图只投影必要状态/版本，并限制到本 client 的 grant。BFF 只能修改自己 grant 的 active 字段；审计通过限定 client/action 的 definer procedure 追加，不能直接写任意租户审计。

核心服务、应用 A、应用 B 分属三张 Docker bridge。MySQL 通过数据库权限限制共享入口；`protocol-public` 是固定目标的窄代理，只允许 discovery/JWKS 的 GET 与 token/revoke 的 POST，禁止查询参数及管理路径。BFF 无核心网连接，不能访问 Hydra Admin 或私有 Hook。网桥保留本机 loopback 映射以供浏览器验收；这不是生产防火墙配置。Admin 的 4465/4445 仅映射宿主 loopback，可信本地开发操作者仍可使用。

## 撤销与失败

单应用退出撤销本地会话及该 grant，不影响另一应用或中央登录。中央退出须确认及 CSRF，事务递增 User 安全版本、撤销该 User 所有中央会话、记录审计并创建按旧 sid 定位的协议清理任务。业务资格立即失效，协议清理失败会退避重试。密码重置也递增安全版本；旧 JWT 即使尚未到期，示范资源仍拒绝，真实旧 refresh 在 Hook 被拒绝。M0 的合成身份状态独立，不受业务 User 重置影响。

数据库不可用、未知 client、缺少上下文、版本不符、签名或范围不符均拒绝。过期交互擦除 challenge/待激活种子，过期 BFF 状态清理。有效 grant 所需的已完成交互状态保留；完整审计留存与归档策略尚未实现。

## 生产剩余工作

当前为 loopback HTTP，cookie 明确使用 `local_`、HttpOnly、SameSite=Lax、host-only；生产必须 TLS、Secure、`__Host-` cookie、受控域名及 ingress allowlist。分布式限流、KMS/密钥轮换、专用 migration/definer 最小权限账号、远程 PDP/SDK、跨实例会话策略、可观测性与容量尚需正式实现和评审。当前视图由本地 root 初始化，不能直接复制为生产账号方案。

未实现正式 React 控制台、完整 scoped RBAC、MFA 撤销/更换及受控恢复、30 天 refresh 策略、产品级邮件供应商和全部业务事件消费者。GCP 部署、Cloud SQL HA/DR 验证由 SRE 负责；本地通过不代表生产验收。
