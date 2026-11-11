# User 审计 API 的 Go 客户端

仅标准库。用已通过真实 OAuth code+PKCE 获取的 **User Bearer** 读取一个受控批次；库不获取登录凭证、不处理 Cookie/AK/SK、不替代机器身份验证器。需要 User platform:read 与当前 tenant audit.read，可委托只读审计员。

```go
client, err := useraudit.New(useraudit.Config{Origin: "https://your-trusted-api-origin"})
if err != nil { return err }
batch, err := client.Management(ctx, userBearer, expectedTenantID, useraudit.Page{Limit: 50})
if err != nil { return err }
// 单独保存/处理这个已授权批次；继续读取需显式使用 *batch.NextCursor。
```

`Machine` 方法读取机器签发审计，调用者仍是 User；不会接受机器 Bearer。固定 `sso.audit.batch.v1` 的两类事件类型分开，管理事件有 membership actor，机器事件只有 SP/credential。客户端验证完整/唯一/固定 JSON 字段、namespace/kind、时间/版本、单批数量、升序唯一 ID 与继续游标，拒绝未知秘密字段和部分响应。

Origin 由可信部署代码配置为 HTTPS，不能来自请求参数；禁止 userinfo/path/query/fragment。仅当前本地显式 `AllowLocalHTTP: true` + `http://api.localhost:8083` 可使用 HTTP，默认传输固定拨号 127.0.0.1:8083 并保留 Host。默认总超时三秒，最多五秒；无重定向、Cookie jar、成功授权缓存、SDK 层自动重试或自动翻页。响应最多 256 KiB，必须 JSON/no-store、无非零 Age/压缩；请求取消保留 context 的固定类别，失败返回零批次。自定义 Transport 仅供可信 CA/测试实现，须保留上下文/TLS/隐私约束。

错误只暴露固定类别：配置/输入、未认证、无权限、不存在、依赖不可用、无效响应；不返回原始错误体或令牌。批次中的 ID/action/time 属于调用者被授权读取的审计数据，后续持久化与业务使用由接入方控制。连续批次不是冻结快照、签名证据或完整归档。

本地 CLI 见 [示例](../../examples/audit-batch/README.md)，完整边界及证据见 [接入文档](../../docs/user-audit-client.md) / [验证记录](../../docs/validation.md)。SDK 与真实 API/BFF 互补，跨空间 token 仍需重新授权；没有通用授权权限缓存。
