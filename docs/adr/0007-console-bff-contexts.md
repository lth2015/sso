# ADR 0007：真实 React 控制台与标签页授权上下文

日期：2026-11-11。状态：M3 本地控制台第一切片。GCP 部署及云端运维继续由 SRE 负责。

## 交付与边界

新增 React 19.3.1 / React DOM 19.3.1、Vite 8.3.7 控制台（格式工具 Prettier 3.9.10），精确版本及 integrity 锁在 npm lockfile。前端生产构建由 Go 嵌入并通过同源 BFF 提供，没有运行 Vite 开发服务器、加载外部 CDN 或收集分析/录屏。提供空间概览/创建、成员停用恢复、组织创建/移动预览与执行/归属、邀请创建接受撤销、审计和自己的会话管理。

控制台使用独立 confidential client `local-console`、secret、AES-GCM key、数据库账号/状态视图及 console 网桥；platform Application/audience 与公共 API 相同。浏览器通过固定认证 origin 的真实 code+S256 PKCE 登录，BFF 校验 state、nonce、issuer、RS256、audience、client、scope 和 MFA。没有浏览器密码换 token 旁路。

OAuth token、refresh、ID token、verifier、nonce 和加密 key 全部留在服务端。React 只接收安全 metadata、CSRF 和业务响应；不使用 localStorage/sessionStorage。邀请码是业务一次性 proof，创建成功时按明确操作只展示一次，关闭/切换页面即清除；只有显式点击才复制到剪贴板。

## 同一控制台的多标签页

同 origin 一个共享 tenant cookie 会使一个标签页的空间切换覆盖另一页，因此每次成功授权生成非凭证 context ID，并设置 `local_console_session_<context>` 的 host-only HttpOnly cookie。URL fragment `#workspace=<context>` 只保存该非秘密标识，不包含 token、code、verifier 或 CSRF；请求必须同时具有对应 cookie。单独复制 context ID 不能认证。

React 从当前标签页读取 context，向 BFF 发送 `X-Console-Context`。CSRF 来自对应 `/session`，仅留页面内存；所有修改（包括 PUT）校验 CSRF 与固定 Origin。每个上下文绑定自己的租户 grant，不以全局“当前租户”覆盖授权。最多保留 20 个 context cookie，再新增时删除最旧 cookie；原服务端状态继续按既有空闲/绝对 TTL 清理。

请求按 context、页面及 generation 绑定；切换时取消旧读取、清空列表/表单/一次性秘密。迟到结果不得写入新空间或新页面。组织/成员权限变化使旧 grant 失效时，页面清空业务表格并要求重新授权；不会把旧 token 原地升级，也不会自动重放先前修改。单独退出当前标签页只撤销该 BFF 会话/grant，其他标签页仍可使用各自有效授权。撤销中央会话则影响绑定它的全部应用上下文，UI 明确区分两个范围。

## BFF 与公共 API

`/bff/v1/...` 的方法/路径白名单从嵌入的公共 OpenAPI 生成，只能转发 18 项业务操作到固定 `platform-api:8083`，Host 固定为平台 API origin。拒绝调用者 Authorization、非规范/非合同路径，不能访问 Admin、Hook 或任意 URL。Cookie、用户提供的转发头等不传给上游；只传服务端 Bearer、Content-Type 与 If-Match。

转发前可以串行 refresh；更新后再次验证 ID/access token 和原租户/grant，且不改变 MFA 时间。业务请求只转发一次，响应失败/结果不确定后不自动重放。响应只允许有界 JSON/problem JSON 与 ETag/request ID 等非秘密头，不转发 Set-Cookie 或上游任意头。公共 API 仍在事务内执行所有 Owner/版本/最近 MFA 资格检查；隐藏管理按钮不构成授权。

BFF SQL 账号不能读取密码、MFA 密文、中央 cookie 摘要、login_aliases 或其他 client token。成员邮箱经公共 API 的 owner 权限返回，BFF 不能直接查询它。网络只允许 console 与 MySQL、公开协议窄代理、platform-api 相连，不能到 core Admin/私有 Hook。核心 API 虽连接 console 网桥，但没有代理/管理旁路。

## 合同与构建

公共 OpenAPI 升至 0.5.0：空间概要新增可读 name（root 名称），owner 成员列表新增已验证 email（无 alias 时 null）；底层仍为 18 项业务操作。普通成员不能读取该列表。新增 `api/console.openapi.yaml` 描述浏览器 cookie/context/CSRF 与 `/session`、OAuth 回调和代理边界，服务通过 `/bff-openapi.yaml` 提供；不能将其当作无 cookie 的公共 Bearer 接口。

`build-console` 使用项目内 npm cache 与 npm ci，先删除自己生成的 index/assets 再构建，保留跟踪的 embed placeholder。源代码 checkout 可以编译普通 Go 测试；实际启动缺少构建产物时 readiness 失败，不能将占位文件作为可用页面。Go/scratch 镜像继续非 root、只读文件系统、无 capabilities。

## 本轮未实现

这是已有业务 API 的真实控制台，不包含任意 scoped RBAC、角色授予/委托、动态第三方 client/机器凭证、MFA 更换/受控恢复、真实邮件供应商和全部事件消费者。本地 HTTP cookie 不是生产 Secure/__Host- 配置；正式域名、TLS、KMS、分布式限流及容量/恢复仍待后续实施验收。
