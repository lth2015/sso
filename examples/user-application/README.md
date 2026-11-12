# 独立本地 User 应用演示

使用自有公开客户端，以 OAuth code + S256 PKCE 取得 platform:read；每次页面刷新由服务端调用 SSO /v1/me 在线核对真实 User、空间、会话与应用准入。浏览器不接收 Bearer，服务不访问数据库，不缓存准入决定。

```sh
scripts/demo user-access-prepare
scripts/demo user-access
```

准备命令在已有私有演示数据旁新增独立空间/应用，保留原账号、密码/TOTP/恢复码、主空间与隔离空间。初次准备选择 selected_members 且普通成员未获授权；重复准备只核验，不重置你已修改的策略。未有基线时先按周五手卡准备基线，勿 reset 保留数据。

也可手工在控制台创建自有应用，登记并生效一个回调严格为 `http://127.0.0.1:9876/callback` 的公开客户端，再运行：

```sh
scripts/user-app-demo --client PUBLIC_CLIENT_ID
```

PUBLIC_CLIENT_ID 是非秘密的 32 位客户端登记 ID。没有 token、密码、SK 的命令行/环境/文件输入；端口已占用会停止，不连接或终止别人的进程。打开 http://127.0.0.1:9876，使用独立浏览器配置登录成员；管理员在控制台更改准入后，刷新此页即可看到拒绝。详细演示见 [手卡](../../docs/runbooks/application-access-demo.md)。

仅绑定回环地址，限制 Host、Origin、CSRF、表单大小与固定 SSO/API 目标；不跟随后端重定向，不接受浏览器自带 Authorization。随机 state/verifier 一次性消费、回调严格字段/预期 issuer/scope，登录成功轮换浏览器会话 ID/CSRF，关闭会话隔离迟到回调与验证结果。cookie 为 HttpOnly/SameSite=Lax，30 分钟演示会话；User token 仅进程内存，最多保留五分钟，无自动 refresh。返回 JSON 有大小/UTF-8/固定字段/重复键/完整文档/no-store/Age 边界，不显示原始错误或凭证。

Referrer Policy 使用 same-origin，使同源导航表单保留可核验的 Origin，同时不向跨源 SSO 发送 Referer；拒绝 null/外部 Origin。CSP 仅允许当前应用与两个固定本地 SSO 来源参与表单跳转。应用只申请 platform:read，不申请 openid、不获取或信任 ID token；这是 OAuth User API 接入参考，不是生产 OIDC RP。使用 openid 的接入方仍必须验证 ID token 的签名/issuer/audience/nonce，见公共 API 手册。

「退出当前演示应用」仅丢弃本进程的演示会话/令牌，中央 SSO 会话保持；不提供生产单点退出、令牌刷新、持久会话、横向扩展或 TLS。真正撤销准入由控制台/API 修改独立版本，原签发令牌在 SSO 下一次主库验证时拒绝，重新授予不复活旧令牌。静态已显示的身份资料需要刷新才能清除。

本示例页面是 HTML 表单，不新增公共平台 API。GET / 显示/在线检查，POST /login 发起授权，GET /callback 处理一次回调，POST /check 重新检查，POST /logout 丢弃当前演示会话。Go 单元与竞态检查、真实浏览器 OAuth/准入联动分别见 validation.md；本示例没有证明 marketstars 已实际接入，部署由 SRE 负责。

文档包独立解压后，可在已有 SSO 服务的机器运行 `go run ./examples/user-application --client PUBLIC_CLIENT_ID`；scripts/ 命令属于完整仓库。包内没有私有演示清单或整套本地服务配置。
