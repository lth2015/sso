# API Playground

Playground 是登录后的真实 API 客户端。它读取与公共服务相同的 OpenAPI 0.9.0，通过控制台 BFF 调用现有 28 个业务操作。它使用真实 Hydra/主库授权，不创建演示身份或伪造 token。

## 启动与使用

```sh
cd /workspace/sso
scripts/console prepare
scripts/console up
# 已验证的本地账户须由可信操作者启用第一方平台。
scripts/console enable --email YOUR_LOCAL_ADDRESS
```

登录控制台、完成密码/MFA、选择有资格的空间，然后进入 **API Playground**。新用户注册及私有邮箱说明见账户手册。Playground 不提供令牌输入框；OAuth token 继续加密保存在 BFF 服务端。

1. 选择接口；页面展示方法、路径、所需 scope 和权限。先用 `GET /v1/me` 检查身份，用空间列表/组织/成员/角色接口查实际 ID。
2. 租户路径固定为当前授权空间；其他资源 ID 使用接口返回的 32 位小写十六进制。列表可填 limit 和 after。
3. 写入 JSON 使用 OpenAPI 示例；组织 parent_id 自动预填当前 root，其他示例值仍须按业务修改。邀请码示例仅表示格式，不是可用凭证。
4. 需要 If-Match 时，点击“读取最新 ETag”或填写刚读取的带双引号版本。此按钮只读取，不提交或重试写入。
5. GET 直接发送；写入先点击“检查写入请求”，核对路径和 JSON，再勾选确认并点击“确认发送一次”。
6. 查看 HTTP 状态、受控 JSON、ETag、Request ID 与 Retry-After。凭证字段和 JWT 会被隐藏；邀请 proof 通过专用邀请界面取得。

## 授权与重试

读操作需要 platform:read；写操作需要 platform:write 和最近 300 秒 MFA，且继续检查当前租户权限。Playground 不绕过 deny、owner/last-owner、跨租户对象和版本规则。普通未授权成员可看到 API 合同，但调用受保护接口会得到实际 403。

403 recent_mfa_required 后重新验证身份；401 后重新确认空间资格。权限变更后旧 grant 失效，refresh 不升级版本。重新授权不会自动重发请求。412 要重新读取并核对修改，428 要提供 If-Match，429 按 Retry-After 等待。网络中断或离开页面时，写入可能已经提交；先查结果再决定是否重试。

## 数据与边界

请求仅转发公共合同中的固定方法/路径，目的地为现有平台 API；没有任意 URL、Authorization 或自定义 header 入口。X-Console-Context 仍须匹配 HttpOnly cookie，写入必须通过 Origin/CSRF。输入/响应只在当前页面内存中，切换页面/空间即清空；不保存调用历史或浏览器 storage。

页面最多展示 24,000 字符的响应，token/password/secret/CSRF/recovery/verifier 字段和 JWT 脱敏。cURL 模板包含 ACCESS_TOKEN 占位符及脱敏请求体，不包含 BFF cookie、CSRF 或 OAuth 令牌。API/schema 示例不代表已创建资源。Playground 将 JSON 解析再序列化，不能用它验证原始重复键/畸形 JSON；这类服务器边界由专门集成测试覆盖。

独立外部客户端仍应实现自己的 state/nonce/PKCE 流程，并调用公共 Bearer API。浏览器 BFF 合同和公共 API 合同分开；`/platform-openapi.json` 是同源只读合同副本，不是匿名业务调用入口。

## 文档与验收

完整接口字段、schema、示例及错误见 [完整 API 参考](api-reference.md)；设计与限制见 [完整手册](handbook.md)。`scripts/console playground-test` 通过真实注册/登录/调用检查读、写确认、ETag、权限拒绝、MFA、响应脱敏、导航清理与手机布局；`node --test tests/playground/model.test.mjs` 检查请求构造、凭证脱敏和单次转发边界。

GCP、公网域名、TLS 和部署由 SRE 负责。本地验收不代表生产发布。
