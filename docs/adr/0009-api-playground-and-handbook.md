# ADR 0009：真实 API Playground 与完整文档导出

状态：已实现；本地验收结果见 validation.md。延续 ADR 0006–0008 的公共 API、BFF 与租户权限边界。

## 交互调用

Playground 作为已登录控制台中的页面，使用当前 context、HttpOnly cookie 和 CSRF，经现有 BFF 调用固定平台 API。没有新的登录旁路、任意目标 URL、调用者 Authorization 或 header 编辑器。GET /platform-openapi.json 只提供同源公共合同副本，不返回凭证。公共和 BFF OpenAPI 同步为 0.7.0；业务操作仍为 26 个，补充所有写请求示例及角色相关 ETag 元数据。

请求目录、参数和示例来自 OpenAPI。租户参数只读，资源 ID 和分页有格式限制；JSON 有 4 KiB 字节上限。If-Match 使用各操作自己的 org/authz pattern。GET 直接发送；写入先生成固定请求快照，用户核对并勾选后才提交。pending 标志与禁用控件防止一次操作并发发送，网络层不自动重试。导航卸载会取消读取并清理内存；写入可能已提交，不能把取消当作回滚。

真实权限、最近 MFA、版本和对象归属仍由 API/主库事务判定。响应面板展示 HTTP 状态、ETag、Request ID、Retry-After 与 JSON；401 进入重新授权，MFA 错误提供身份验证入口，均不重放。响应中的 token/password/secret/CSRF/recovery/verifier 字段、JWT 字符串及 cURL 模板的 proof 会脱敏。一次性邀请凭证继续从专用邀请 UI 获取；OAuth token 不进入页面。没有调用历史、localStorage/sessionStorage 或结果持久化。

Playground 解析再序列化 JSON，适合业务调试；原始重复键、畸形编码等 HTTP 边界仍使用专门测试。显示至多 24,000 字符；大响应、完整集合搜索、多语言 SDK、动态 OAuth client/机器凭证管理另行实现。

## 文档输出

维护源文件、API schema 和 ADR，生成完整 handbook.md 与逐操作 api-reference.md。所有示例用 JSON Schema 校验，生成文件可用 scripts/docs check 检测是否过期。公开 /docs 直接从嵌入的同一 OpenAPI 展示各操作参数、示例、展开后的 schema、成功响应及公共错误说明。

离线 HTML 使用精确锁定的 markdown-it 15.0.3 渲染，禁止原始 HTML，且 CSP 禁止脚本/表单/网络资源；文档中的链接可由用户主动打开，不加载外部 CDN。此依赖只在构建阶段使用，不增加 Go/BFF 的运行时 Node 服务。Mermaid 保留为可读源代码；源图、截图与全部合同随包提供。

ZIP 包包含文档、原始设计输入、截图、OpenAPI、离线 HTML 和逐文件 SHA-256 清单。清单用于验证完整性，不是作者签名。原始文档字节保留，其内嵌操作指令不执行。离线阅读不提供匿名 API 执行；真实调试必须进入运行中的已授权控制台。

验收覆盖真实注册/密码/MFA/Hydra/BFF/API、写入确认/单次发送、ETag/412、普通成员 403、MFA 过期、凭证脱敏、输出转义、导航清理与桌面/手机；离线 HTML 检查目录锚点、无外部请求、无脚本/表单及布局，导出包校验每个摘要。受管 Chromium 禁止 file:// 导航，HTML 测试通过临时 loopback 只读服务加载同一文件，不将该测试描述为云端部署。GCP 部署与运维仍由 SRE 负责。
