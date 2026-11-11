# 当前交付验证记录

日期：2026-11-11（累计本地验收）。本地交付 M0 协议实验、M1 数据仓储、M2 账户生命周期，真实 Hydra 登录/MFA/中央会话与双应用 BFF，公共业务 API，以及真实 React/BFF 管理控制台。GCP 部署及云端运维由 SRE 负责。

| 检查 | 实际结果 | 边界 |
| --- | --- | --- |
| 固定工具链/依赖 | Go 1.27.3；x/crypto v0.58.0、go-oidc v3.21.0、x/oauth2 v0.36.0 及传递依赖锁定在 go.mod/go.sum，校验保持启用 | 未执行完整依赖漏洞审计 |
| 密码单元测试 | 2 项通过，覆盖 Argon2id salt/格式/校验、畸形参数拒绝和输入策略 | 并发容量另在账户测试中验证 |
| 全新数据库启动 | sso-console-validation 项目新 volume，M0/业务 Hydra、001–004 迁移、三个 BFF 受限账号/视图、platform-api、React 构建资产及独立 console client 从零建立通过 | 真实本地 MySQL 8.4.13；验证后仅删除自己创建的临时 volume，恢复默认服务 |
| `scripts/m0 test` | 28 项协议测试全部通过 | 合成身份，真实 Hydra |
| `scripts/m1 test` | 10 项业务数据库测试全部通过 | 真实 MySQL，无 mock |
| `scripts/m2 test` | 12 项账户/HTTP 测试全部通过，其中 11 项连接真实 MySQL、1 项测试限流器容量；此 profile 明确跳过 5 项 SSO 测试 | SSO 由专用 profile 另行执行 |
| `node tests/accounts/browser.cjs` | 实际浏览器注册、私有邮箱投递、验证设密、密码确认、恢复流程通过；桌面/手机截图已生成 | 无 cookie/浏览器存储凭证，fragment 已清除；无外部邮件发送 |
| 账户 OpenAPI | YAML 可解析，四个 POST 合同存在 | 不是正式管理 API 合同 |
| `scripts/sso test` | 5 项真实数据库测试通过，没有跳过 | 绑定/重放/并发激活/过期次数/退出竞态；协议成功另由真实浏览器证明 |
| `scripts/sso network-test` | 2 次隔离网络检查通过（app_a、console 各一次） | 核心 Admin/私有 Hook 的 DNS 与实际 IP 不可达；窄代理拒绝管理路径，允许 discovery |
| `node tests/sso/browser.cjs` | 真实注册 → Hydra → 密码/MFA → code+PKCE → 两个 BFF 不同租户 → 续期/撤销/退出全部通过 | 连接真实 DB、worker、Hydra；fixture 清理；桌面/手机安全截图 |
| 浏览器中实际 token/权限检查 | 两个应用的真实 ID/access token 验证；错误 nonce、篡改签名、alg=none、ID/access 混用与不可信 JWKS 目标拒绝；密码重置后两个实际旧 refresh 均经 Hook 返回 403 | 固定本地 issuer/RS256/audience/client，非自制签名假 fixture |
| BFF 数据库权限 | 三个专用 BFF 账号均验证原表受保护字段被 MySQL 1142/1143 拒绝、另一 client 会话不可见、跨 client 写入被视图 CHECK OPTION 1369 拒绝 | 本地 definer 初始化，生产最小权限账号仍需单独实施 |
| `go vet ./...` 与 `go test -count=1 ./...` | 通过；TOTP 包包含 6 个 RFC 向量及防重放窗口检查 | 普通 Go 测试明确跳过需要服务/fixture 的情况，不能替代上述集成命令 |
| SSO OpenAPI | 11 个路径 YAML 可解析，所有本地引用可解析 | 可信交互合同；公共业务 API 使用独立合同 |
| `scripts/api test` | 18 个业务操作全部实际覆盖，87 个真实 HTTP 响应通过 OpenAPI 校验，成功请求 body 也校验；完整 fixture 清理成功 | 原生 loopback callback、真实 Hydra code+PKCE、真实业务 API/MySQL，无认证 stub |
| 公共 API 授权边界 | read-only scope 禁写、ID token/坏 token/cookie 拒绝、错租户 path/header 拒绝、非 owner 无成员/组织/邀请/审计管理权限 | 组织归属不提权；当前为 owner-only 管理，非完整 scoped RBAC |
| 版本与最近 MFA | 同 org ETag 的两个并发写恰好一项 201、一项 412；缺失 428、畸形前置条件 400；旧认证时间写入 403，重新密码/MFA 后可写 | 真实并发 HTTP/数据库事务；refresh 不更新认证时间 |
| 写入竞态 | 使用真实 access token 验证后递增 fixture User 版本，组织写入及全局新建空间均拒绝；事务内 token 到期也拒绝，无越权组织落库 | 专用 Go profile 从 stdin 读取 fixture，不把 bearer 放入 argv/env/日志 |
| 邀请/成员/会话 | 邀请撤销、邮箱绑定、一次性接受，成员停用恢复后旧 grant 仍拒绝，最后 owner 保护；会话只允许本人撤销，当前会话撤销后 token 被拒绝 | proof 不进列表/审计；无外部邮件投递 |
| 公共 API OpenAPI/参考页 | YAML/JSON 同步、引用可解析、operationId 唯一；实际 `/docs` 18 项操作、桌面/手机布局通过 | 默认 api/openapi.yaml 为公共业务合同，M0 原合同逐字保留到 m0-internal.openapi.yaml |

M2 数据证据：邮箱证明后才创建 User/凭证/个人 Tenant/Root/Membership/owner；挑战与相关事件同事务；同一挑战及同邮箱不同挑战并发时只有一个账户成功且无孤立 User。恢复递增安全版本，旧密码与旧版本恢复挑战拒绝。

M2 安全证据：不匹配 Origin、未知字段、多 JSON、非法 UTF-8、超限 body、查询参数和过于频繁请求均拒绝；哈希池饱和不消费挑战。通知 AES-GCM 校验元数据，投递原子发布/幂等，文件 0600、目录 0700；失败退避保留密文、过期不投递、成功擦除队列密文。浏览器首次验收发现并修复了目录 UID 不匹配与同页 fragment 导航处理问题。

M0/M1 既有证据仍包括跨租户/授权链隔离、会话/退出与签名轮换、组合 FK、组织闭包与并发移动、最后 owner 保护、审计回滚、邀请一次性/并发/过期和组织归属不隐式提权。

可信 SSO 证据还包括：缺失/错误 CSRF 与其他浏览器请求拒绝，已消费 callback 重放和无绑定 cookie 的 callback 拒绝；多租户静默授权返回 interaction_required；TOTP 时间片和恢复码不能重用；成员恢复不会复活旧版本 grant；取消不影响另一有效会话；仅退出应用 B 不影响 A；全设备退出使 A 失效。token 未进入浏览器 storage，cookie 为 HttpOnly/host-only，MFA 密钥与恢复码未进入截图。账号恢复保留 MFA，不允许凭邮箱证明跳过第二因素。

验收期间修复了本地 BFF 共用账号/key、平面网络可达 Admin 的隔离缺口、中文 seed SQL 字符集，以及浏览器测试 API 客户端不解析 .localhost 的测试工具问题；最终浏览器请求使用 Chromium 的真实 cookie/域名行为。修复后相关检查均重新执行通过。

尚未交付：控制台的动态 client/机器凭证/角色管理、完整 scoped RBAC、MFA 管理/受控恢复、正式多语言 SDK、30 天 refresh、真实邮件供应商与全部租户事件消费者。当前两个 BFF 属于同一 Application，不代表完整跨产品权限已验收。账户页面不直接签发 token；M0 合成身份实验独立，业务密码重置不会撤销 M0 token。生产 TLS/Secure cookie、分布式限流、KMS/轮换、负载/安全/云端 HA/DR/SLO 仍需后续实施验收，不在本地记录为通过。

原始上传文档字节保留；随机本地秘密不进入 Git。源码推送不是生产发布。


公共 API 本轮全新 volume 回归：`scripts/m0 test` 28 项、`scripts/m1 test` 10 项、`scripts/m2 test` 12 项（此 profile 明确跳过 5 项专用 SSO）、`scripts/sso test` 5 项、`network-test` 1 项，以及账户/SSO/API 三个真实浏览器流程全部通过；`go vet ./...`、普通 Go 测试和 spec 同步检查通过。只删除本次创建的 sso-api-validation volume，默认服务恢复。API 参考页最后一次手机样式调整后单独重建并重新通过页面验收；业务代码未再修改。

本轮修复了测试清理多级组织时自引用 FK 的删除顺序，并为共享 fixture 增加成员闭合集合检查，拒绝清理包含其他账户的空间。最终清理完成后私有 mailbox 中无遗留 JSON，随机本地环境秘密未匹配到 Git 候选文件，原始上传文档不变。公共 API 是可供集成的本地实现与合同，没有部署为互联网服务。


## 2026-11-11：React 控制台与 BFF 上下文

| 检查 | 实际结果 | 边界 |
| --- | --- | --- |
| 构建与依赖 | React/React DOM 19.3.1、Vite 8.3.7 与 Prettier 3.9.10 精确锁定；npm ci、生产构建及 npm audit（含开发依赖）通过，报告 0 个已知漏洞 | 非完整供应链审计；构建产物嵌入 Go，未运行 Vite dev server |
| `scripts/console test` | 真实 React→BFF→公共 API→MySQL/Hydra 流程通过，收尾清理完成 | 两个合成账户、独立 console OAuth client，非认证 stub |
| 多标签页隔离 | 同 origin 两个不同空间 context 同时有效；切换一个不覆盖另一个；延迟的真实旧成员响应被取消/丢弃，不显示到新空间 | context 仅非秘密标识，仍须对应 HttpOnly cookie；复制 context 无法认证 |
| 浏览器安全 | 缺少 CSRF 403、调用者 Bearer 拒绝、错租户 header/path 拒绝、私有 Admin 路径不转发；无 localStorage/sessionStorage、无 JWT 进入 BFF JSON/DOM | local HTTP cookie；生产 TLS/Secure 要求未验收 |
| SQL/key/refresh | console 与两个示范 BFF 独立角色/key；不能读密码、MFA、中央 cookie 摘要、login_aliases、其他 client token；另一 key 不能解密 console 状态；强制 fixture 到期后实际服务端 refresh 成功 | 特权测试只针对生成 fixture，未修改真实账户 |
| UI 管理流程 | 创建空间/组织、邀请一次性展示与接受、非 owner 管理拒绝、组织归属不提权、last-owner、成员停用恢复、移动预览/执行、审计及中央会话撤销通过 | 邀请撤销另由公共 API 集成覆盖；当前 owner-only，非完整 RBAC |
| MFA 与重放 | 超过 300 秒写入被拒绝；重新密码/恢复码验证后可写，旧表单没有自动提交；旧 grant 恢复后仍拒绝 | refresh 不更新 MFA 时间 |
| 输出转义/界面 | 用户提供的 HTML 组织名显示为文本，没有 img 元素执行；桌面/手机无页面横向溢出，安全业务截图已生成 | 非完整 WCAG 认证，表格可在容器内横向滚动 |
| API 合同 | 公共 API 0.5.0 的 name/email 字段通过全部 87 次真实响应/成功请求 schema 校验，仍覆盖 18 项业务操作；新增 console OpenAPI 引用可解析、operationId 唯一并由实际服务读取 | browser cookie/context/CSRF 合同不替代公共 Bearer 合同 |
| 既有回归 | M0 28、M1 10、M2 12、SSO 5、app_a/console 网络 2 次、账户/SSO/API 浏览器、API 文档页，以及 Go vet/普通 Go 测试通过 | M2 profile 的 5 项 SSO 明确跳过，专用 profile 另行通过；普通 Go 跳过真实 fixture 案例 |

修复的实际问题包括：Go mux 通用代理前缀与 SPA GET 路由冲突；测试工具的 Node HTTP 客户端不解析 .localhost；浏览器把 YAML 导航当下载；以及测试需要在组织权限变化后重新取得 grant。最终控制台流程、合同检查和相关回归均重新执行通过。

测试工具最初的异常原文意外带出两个合成账户的临时 cookie。已立即撤销/删除这些账户及其 grant/session/proof，清除原始本地诊断，并改为 loopback+固定 Host、捕获全部测试异步异常，仅输出安全阶段/错误类别；未涉及真实用户。最终 fixture/私有 mailbox 均清理，不将 cookie/token 写入 Git。

全新 sso-console-validation volume 验收后仅删除该临时项目的数据，默认开发服务恢复。最后再以清理后的生成资产执行 prepare/up，确认可重复构建；未部署任何公网/GCP 服务。当前实现与剩余功能以 ADR 0007、console 手册为准。

最终 React/CSS/控制台验收脚本使用锁定的格式工具整理；构建产物随之刷新，已重新构建服务并再次执行控制台完整浏览器验收。格式检查与含开发工具的 npm audit 均通过。
