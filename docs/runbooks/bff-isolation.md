# BFF 独立数据库与授权 API

Kubernetes 模式下，BFF 不再查询业务库或跨库视图。参考应用的 local-accounts 模式继续使用原有本地仓储，以保留本地协议验收。

## 数据与访问边界

- BFF 账号只拥有 sso_console 中 bff_transactions、bff_sessions、bff_revocation_outbox 三张表的 DML 权限；迁移见 migrations/bff/001_sessions.sql，无跨库外键、视图或存储过程。
- platform-api 使用业务库资格与版本，在每次授权检查时校验当前账户、会话、成员、租户、应用和 grant。
- 内部合同见 api/bff-internal.openapi.yaml。该接口不加入公开业务合同，也不由浏览器 /bff 代理放行。
- 平台 API 的 BFF_CLIENT_ID 默认为 sso-bff，只有该 client 的 platform:read 用户授权可以查询资格和申请撤销凭证。

BFF 仍验证 JWT 签名、issuer、audience、client、subject、MFA ID Token 以及授权上下文标识。API 返回的 user、tenant、grant、member、session、client 必须全部匹配，不能使用浏览器提供的身份或租户覆盖。

## 过期、刷新与退出

普通请求在 Token 即将过期时，先串行刷新，再调用 API 检查实时资格。没有资格证明或 API 不可用时，拒绝业务请求；不会用签名正确但已撤销的 JWT 继续操作。

仅持有本地有效会话和正确 CSRF 的刷新/退出操作，不要求旧 Access Token 仍有效：

- 刷新仍经过 Hydra Hook，并对新 Token 调用平台 API 重新校验。
- 退出使用专用、随机、只绑定一个 grant 的撤销凭证。该凭证不能登录、读取资料、刷新 Token 或选择其他 tenant/grant。业务库只保存其 SHA-256 摘要，BFF 将原文与 Token 一起加密。
- 业务库迁移 013 创建撤销凭证表。只有当前有效授权才能获得凭证，已停用用户不能再申请；先前有效签发的凭证仍可完成单向撤销。

本地退出或刷新失败会在同一 BFF 数据库事务中使会话失效、清空会话 Token，并把原加密载荷写入撤销 outbox。立即尝试投递，失败保留并由后台每 2 秒调度、指数退避（上限 300 秒）重试。多副本通过行锁与 SKIP LOCKED 协调，不重复消费。

本地 idle/absolute 超时会由同一后台流程排队撤销。平台端对 grant 失效与 audit/outbox 同事务处理，重复撤销不会重复写审计。成功或撤销凭证过期后，BFF 清空 outbox 中的加密载荷。

数据库间不存在分布式事务。API 故障期间，本地会话已退出，但中央 grant 的撤销可能延迟，直到重试成功或 grant 自身到期；不能将本地 204 宣称为中央立即撤销。应监控 pending 积压、最老任务时间与重试失败。整个 outbox 保留加密载荷，不能在失败时丢弃凭证。

## 配置与迁移

1. 先对业务库执行 migrations/business/013_bff_revocation_credentials.sql，并按现有 checksum 机制记录迁移。
2. 对专用 BFF 库执行 migrations/bff/001_sessions.sql，创建只限这三张表的运行账号。
3. 保留既有 SSO_BFF_KEY 与 OAuth client secret；仅切换 BFF 数据库账号及密码。存在旧跨库视图/旧会话时需单独迁移，不能覆盖真实数据。
4. 先更新平台 API 镜像，再更新 BFF，保持平台 audience 与 client 注册一致。
5. 浏览器只使用 Host-only Secure/HttpOnly 会话 Cookie；写入仍要求精确 Origin 和 CSRF。

集群清单位于 Git 忽略的 deploy/kubernetes，不纳入 Git 提交。公网入口只路由控制台和身份页面，不开放 /internal/bff/*。

## 验证

```sh
go test ./...
go vet ./...
bash scripts/test-bff-isolation
```

最后一项只连接临时 Docker MySQL 的 loopback 随机端口，不读取部署凭证，退出后删除容器。它验证真实数据库权限隔离、一次性登录交易、失败/退出/到期撤销持久化、重试后密文清理、跨 client 拒绝、停用后不能签发凭证、停用后仍可撤销、审计与 outbox 幂等，以及撤销凭证过期拒绝。

## QA 发布验证（2026-11-16）

已发布并滚动更新 QA 的 platform-api、identity-api、BFF，三个 Deployment 均 Ready。业务迁移 013 和 BFF 三张独立表已应用，BFF 改用仅限三表 DML 的 sso_bff_runtime；OAuth client secret 和原加密密钥保持一致。

新增 BFF Service、两个页面 HTTPRoute 和 GKE HealthCheckPolicy；保留现有 Hydra 协议规则。Google HTTP 健康检查仅接受 200，因此 HTTPS 身份服务和 Kubernetes BFF 的健康接口在实际依赖正常时返回 200；本地参考模式保留 204。外部入口不放行健康与内部授权接口。

使用指定 Gateway IP 且校验真实 HTTPS 证书的请求，BFF 首页与账户页返回 200，登录导航经 Hydra 到达统一登录页。真实用户的密码、MFA、刷新和退出仍需验收，不能据此宣布整套 SSO 已通过生产验收。bff.nsstest.com 的正常 DNS 解析仍由 DNS 管理方配置为 136.81.192.149；本次未修改 DNS。

最终检查已确认 bff.nsstest.com 正常 DNS 解析到 136.81.192.149；无需 --resolve，也能经 HTTPS 从 BFF 跳转到统一登录页。

## 认证页面导航

控制台前端使用同源固定路径 /auth/account、/auth/security、/auth/authenticator、/auth/logout，由 BFF 按部署时的公开认证 origin 进行 303 跳转。Kubernetes 模式使用已配置 issuer 的 origin，本地模式使用参考认证 origin。不能由查询参数或转发 Host 指定跳转目标。

GKE 的 BFF HTTPRoute 需要放行 /auth 路径；这是浏览器导航，不是跨域 AJAX。前端生产 bundle 不再包含 auth.localhost:8080，切换部署域名不需要改 React 链接。
