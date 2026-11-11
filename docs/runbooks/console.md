# React 控制台本地运行与验收

仅本地开发与验收；使用现有 `/workspace/sso` checkout，不需 worktree。公网/GCP 部署由 SRE 负责。

```sh
cd /workspace/sso
scripts/install-go
scripts/console prepare
scripts/console up
scripts/console test
scripts/console rbac-test
scripts/console playground-test
scripts/console security-test
```

准备需要 Docker/Compose、Node >=22.12（本次 24.22.1）、npm、Python/PyYAML/jsonschema、Playwright 和 Chromium。prepare 先用 npm ci 安装精确锁定的 React/Vite，构建静态资产，再编译现有 Go 服务/BFF/测试；不运行前端开发服务器。npm cache 在忽略的 `.local/npm-cache`，不能为安装改 HOME、关闭 integrity/TLS 或升级全局 npm。

控制台本地 origin 为 console.localhost:3003。up 复用账户、Hydra、公共 API 启动链，初始化独立 console key/账号/视图与 OAuth client，检查 `/health/ready` 的真实构建产物。浏览器从控制台发起登录；新注册账户需要可信本地操作者显式启用第一方平台：

```sh
scripts/console enable --email YOUR_LOCAL_ADDRESS
# 需要体验不同空间时，可额外创建一个本地演示团队。
scripts/console enable --email YOUR_LOCAL_ADDRESS --second-tenant
```

注册/本地邮箱/MFA 见账户与 SSO 手册。不能自行构造 subject、跳过 MFA，或把 CLI 作为登录旁路。控制台不发送外部邮件；创建邀请得到的 proof 由调用者安全转交。

## 日常操作

- 空间概览可创建空间、接受邀请、发起重新授权选择空间。每次授权有独立标签页 context；重新选择空间不覆盖其他标签页。
- 导航与按钮按有效权限显示。未授权成员仅有空间/会话入口；成员、组织、邀请、审计能力可分别委托。角色与绑定管理仅 owner 可用；后端仍拒绝伪造请求。
- 组织移动可先预览影响组织数/深度，再明确确认；root/cycle/版本规则由服务端执行。组织关联不会授予角色。
- 敏感写入超过 5 分钟 MFA 时，页面要求重新验证身份。验证成功后重新发起操作，不自动恢复/提交旧表单。
- 成员/组织权限变化后旧授权失效，需重新确认空间；恢复成员不会复活旧授权。
- 邀请凭证只显示一次，关闭即清除；没有自动复制、浏览器持久存储或日志输出。邀请码接受后只能获得成员资格。
- “退出此标签页”只退出当前 BFF context；“撤销会话”影响对应中央会话；“退出所有设备”由统一认证页明确确认。

当前列表每页 50 条并可加载更多。关联成员对话框提供前 100 个成员供选择；大规模成员选择/搜索仍待后续增强。不要将此本地容量当成生产 SLO。业务 API 使用 name/email 字段显示可读资料，UUID 仅在定位记录时展示。

## 验收证据

`console test` 使用两个合成账户、三个浏览器标签页、真实 Hydra/MySQL/API：验证初次密码/TOTP、同 origin 多空间隔离、上下文/CSRF/cookie 边界、独立 key/SQL 角色、服务端自动 refresh、最近 MFA 与无自动重放、一次性邀请/接受、非 Owner 拒绝、组织归属不提权、last-owner、成员停用恢复、移动预览执行、审计与单会话撤销。还将真实旧空间响应延迟到新空间加载后，检查不会显示旧资料，并检查用户输入作为文本转义、无浏览器持久凭证及桌面/手机布局。

测试进程只在内存持有密码/因素/proof，不截图它们。失败只打印安全阶段/错误类别；Playwright 非浏览器客户端不一定解析 `.localhost`，延迟响应测试使用明确 loopback + 原 Host 并捕获异常，禁止打印请求 header/exception 原文。测试清理用闭合集合保护，拒绝删除包含非本轮账户的空间；组织从叶到根删除。

完整回归依次执行 M0/M1/M2/SSO/网络、账户/SSO/API 浏览器和控制台验收。`scripts/sso network-test` 现在分别从 app_a、console 两张隔离网桥检查 Admin/Hook 不可达；数据库角色测试覆盖两个示范 BFF 和 console。

截图仅展示无凭证业务页面：`docs/ui/console-overview.png`、`console-members.png`、`console-audit.png`、`console-mobile.png`。OpenAPI：公共 API 为 `api/openapi.yaml`/JSON；浏览器 BFF 为 `api/console.openapi.yaml`（页面路径 `/bff-openapi.yaml`）。

停止用 `scripts/console down`，保留 DB volume 和私有配置；源码变更后重新 prepare/up。保留 `.local/m0.env`、`sso-key.id`、`account-key.id`；console key/secret/DB 密码独立且受 fingerprint 保护。不要自动 down -v、输出 env/Compose/完整 inspect、cookie/token、MFA 种子/恢复码或 mailbox 内容。详见 [ADR 0007](../adr/0007-console-bff-contexts.md)。


角色与权限页面可创建/编辑/删除租户角色、绑定或解绑成员角色。规则为未设置/允许/拒绝，显式拒绝覆盖其他角色允许；members.read 包含已验证邮箱。owner 绑定额外要求勾选确认，最后 owner 无法移除。先授予新 owner，再移除旧 owner。变更后需重新授权，不自动重放表单。若 owner 拒绝了自己的成员读取权限，角色管理仍提供自己的绑定用于恢复。成员选择目前限前 100 人。

`rbac-test` 使用三个合成账户，通过真实登录/API/BFF 验收默认拒绝、委托读写、deny 优先、撤销、跨租户对象拒绝、owner 恢复/移交及真实 React 角色管理。新增截图：[角色](../ui/rbac-roles.png)、[受委托审计员](../ui/rbac-auditor.png)。设计和 API 细节见 [ADR 0008](../adr/0008-tenant-roles-and-deny.md)。


新增 API Playground 导航，可读取同源 OpenAPI 并调用固定的 34 个业务操作。GET 直接发送，写入预览/确认后发送一次，支持 ETag 读取、HTTP 状态/错误、cURL 模板和凭证字段脱敏；离开页面清空内存并取消在途读取，不自动重发写入。使用及限制见 [Playground 手册](../playground.md)。


安全会话页面已增加个人 MFA/恢复码/会话概览及“撤销其他会话”确认流程；同一中央会话的多个标签页保留。恢复码管理和认证器更换跳转可信认证域，不在 BFF/Playground 输入密码或验证码。认证器更换的两阶段/取消/结果不明处理见 [专门手册](../authenticator-management.md)，验收 `scripts/console authenticator-test`。见 [个人安全](../account-security.md)。

个人安全记录位于安全会话页，20 条分页与刷新，使用当前 User 的公开只读接口；跨空间仍只读本人记录，未公开内部目标标识。验收 `scripts/console events-test`；语义及限制见 [手册](../security-events.md)。
