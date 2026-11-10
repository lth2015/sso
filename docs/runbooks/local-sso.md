# 本地真实 SSO、MFA 与双应用验收

在现有 `/workspace/sso` checkout 工作，无需 worktree。仅开发与本地验收；不部署 GCP，不发送外部邮件。

```sh
cd /workspace/sso
scripts/install-go
scripts/sso prepare
scripts/sso up
scripts/sso test
scripts/sso network-test
node tests/sso/browser.cjs
```

需要 Docker/Compose、Python 3、Node 与 Playwright、Chromium。本次环境提供 Node 24.22.1、Playwright 1.62.1、Chromium 156.0.8078.79；浏览器工具来自云环境运行时，不是应用生产依赖。Go 使用仓库固定工具链；浏览器测试默认 `/usr/bin/chromium`，可用 `CHROMIUM_PATH` 指定安装位置。prepare 编译实际 API、worker、双应用 BFF、窄代理及测试，复用 M0/M1/M2 缓存；up 应用 001–004 checksummed migrations，建立独立协议库、两个固定本地 client、BFF 受限账号与视图，再启动服务。不要修改已应用迁移的内容。

| 本地入口 | 用途 |
| --- | --- |
| auth.localhost:8080/account | 注册、邮箱证明、密码恢复 |
| auth.localhost:8080/login、/consent | 由真实 Hydra 重定向进入，不能自行构造可信 challenge |
| app-a.localhost:3001 | 创作工作台 BFF |
| app-b.localhost:3002 | 研究工作台 BFF |
| auth.localhost:4464 | 业务 OAuth/OIDC public issuer |
| 127.0.0.1:4465 | 仅本地操作者可用的业务 Hydra Admin |
| 127.0.0.1:4444、4445、8081 | 独立 M0 实验，不用于真实账户登录 |

端口只映射宿主 loopback。Chromium 支持 `.localhost`；其他客户端可在本地解析这些域名到 loopback。不要创建公网预览或共享 Admin 端口。宿主可访问 Admin 不等于 BFF 容器可访问，`network-test` 从 app_a 网络同时验证 DNS 名和实际 core IP 均不可达，并验证白名单代理路径。

## 手工体验

通过账户页面请求注册证明；本地通知在 `.local/mailbox` 中，仅查看自己的 fixture/账户，正文含短期凭证，不粘贴到日志或聊天。完成邮箱验证和设密后，可信操作者显式启用示范应用：

```sh
scripts/sso enable --email YOUR_LOCAL_ADDRESS
# 需要测试多租户时额外创建一个演示空间；此项会实际写业务库。
scripts/sso enable --email YOUR_LOCAL_ADDRESS --second-tenant
```

该 CLI 使用真实租户/成员仓储，不能代替密码或 MFA 登录。随后从应用 A 发起登录，设置认证器并安全保存一次性恢复码，选择空间后返回应用。应用 B 可复用中央身份验证，独立选择空间。可体验单应用退出、中央全设备退出、密码恢复后的旧会话拒绝。测试脚本会生成自己的合成账户和空间，完成后清理。

## 检查与调试

`scripts/sso test` 有 5 项真实数据库测试：浏览器/CSRF 绑定与旧密码证明竞态，TOTP/恢复码重放，双请求并发激活，尝试次数/过期，全设备退出阻止在途旧证明。测试本身不伪造 Hydra 成功，真实协议流由浏览器验收覆盖。该命令暂停本项目通知 worker 后运行，结束恢复并返回原失败状态。

浏览器验收创建真实账户、经实际 Hydra 完成 MFA 与 code+PKCE、两个 BFF 的不同租户授权、续期、跨租户头拒绝、成员撤销/恢复后的旧授权拒绝、强制重认证、恢复码单次使用、密码重置后的旧 JWT/实际 refresh 拒绝、静默多租户拒绝、取消、单应用/全设备退出。过程中执行真实签名 token 的 SDK 反例及 MySQL 权限检查；测试 secret 只留进程内存，失败只打印阶段/错误类别。截图在 `docs/ui/sso-*.png`，不拍摄 MFA 种子、恢复码或密码。

全部回归另运行 `scripts/m0 test`、`scripts/m1 test`、`scripts/m2 test`、`node tests/accounts/browser.cjs`。普通 `go test ./...` 明确跳过需要 DB/浏览器 fixture 的测试，不能替代这些命令。

不要输出 expanded Compose config、容器完整 inspect/env、token 响应、mailbox 正文或 SQL 输入。私有配置在忽略的 `.local/m0.env`，密钥指纹在 `.local/sso-key.id` 和 `.local/account-key.id`；保留它们和数据库 volume。缺失/更换既有 key 会拒绝启动，不能用随机新 key 解密旧状态。两个 BFF 现在使用不同 key，升级旧共享 key 的本地会话需重新登录；旧兼容字段保留但不注入 BFF。

停止使用 `scripts/sso down`，保留数据；不要 `down -v`。重启 `scripts/sso up`。源码变更先 `scripts/sso prepare`。新 volume 验收可用独立 `COMPOSE_PROJECT_NAME`，固定宿主端口要求先停止本仓库默认项目；仅清理自己创建的验证 volume，随后恢复默认项目。

生产边界见 [ADR 0005](../adr/0005-trusted-login-mfa-bff.md)。这些示范应用使用同一 Application，完整权限控制台、MFA 管理、正式邮件与生产 TLS/KMS 等仍待后续开发。
