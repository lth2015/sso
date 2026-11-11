# 外部业务服务：Go SDK 与完整本地示例

0.15.0 增加可复用的 Go 在线机器身份验证器，以及独立业务服务和一次性客户端示例。当前 0.19.0 的 SSO User 管理合同为 59 项；机器协议/主库身份资源合同单列。业务服务示例有自己的合同，不属于平台管理或 User BFF。

## Go 服务端接入

包路径：`github.com/lth2015/sso/pkg/machineidentity`。接入工程应固定经过审核的提交或模块版本；当前实现与本仓库一起交付，未宣称发布独立包版本。

```go
verifier, err := machineidentity.New(machineidentity.Config{
    Origin: "https://sso-api.example.com", // 由可信部署配置提供
})
if err != nil {
    // 使用固定配置错误信息，不输出秘密。
    return err
}
identity, err := verifier.Verify(ctx, expectedTenantID, expectedApplicationID, rawBearer)
if err != nil {
    // 拒绝当前请求；按下文区分身份拒绝与权威服务不可用。
    return err
}
// 将 identity.ServicePrincipalID / CredentialID 用于自己的业务授权策略。
```

预期 tenant/application 必须来自服务配置或业务资源的可信归属查询。请求携带的 tenant header、未验证 JWT payload 和 Cookie 不提供这个上下文。`rawBearer` 是从入站 Authorization 头提取的令牌，不含 `Bearer ` 前缀。

SDK 每次向固定配置的 SSO 原点调用机器身份资源，检查主库复查后的类型、空间、应用、SP/凭证 ID、scope 和 audience。SDK 不缓存成功权限，返回值只含身份元数据；实际业务权限仍由调用方判断。`machine:invoke` 不授予平台管理或任意业务管理员权限。

默认 HTTPS 校验证书；配置不得带 userinfo、路径、query 或 fragment，令牌和请求参数不能选择主机/JWKS/issuer。默认总超时 3 秒，可配置至最多 5 秒；支持调用方 context 取消。重定向被拒绝，没有 Cookie jar。响应体最多 16 KiB，拒绝重复/未知/遗漏 JSON 字段、错误类型/命名空间、非 no-store 或已缓存响应，不在权威不可用时返回旧身份。SDK 不转发上游错误正文或网络原始错误，也不记录 Bearer。

`Config.Transport` 用于可信自定义 CA/受控测试，其实现由接入服务负责。当前本地入口只有显式 `AllowLocalHTTP: true` 才允许 `http://api.localhost:8083`；默认传输将该固定目标直接连接本机 127.0.0.1，同时保留 API Host，不依赖系统解析 .localhost 或出站代理配置。

## 错误和调用方处理

| SDK 错误 | 含义 | 业务服务示例 |
| --- | --- | --- |
| ErrConfiguration | 固定原点/超时配置不合法 | 启动失败，使用固定日志 |
| ErrInvalidRequest | 目标 ID 或 Bearer 格式不合法 | 401 |
| ErrUnauthenticated | SSO 拒绝身份，如撤销/到期/User realm | 401 |
| ErrForbidden | SSO 明确拒绝 | 403 |
| ErrUnavailable | 超时、限流、连接失败或权威服务故障 | 503 |
| ErrInvalidResponse | 响应格式、缓存标记或预期租户/应用不符 | 503 |
| context 取消/到期 | 调用方停止当前验证 | 终止当前处理；示例使用受控 503 |

可用 `errors.Is` 检查；失败返回零值身份。调用方自行决定对幂等读取的有限重试，SDK 不重放业务写入。为在线资格检查预留可用性与延迟预算；生产运行与网络/TLS配置由 SRE 对应。

## 独立业务服务示例

源码位于 [examples/machine-resource](../examples/machine-resource/main.go)，只绑定 `127.0.0.1:8092`。它使用 SDK 验证入站 Bearer，提供 `GET /whoami`，返回已验证的机器身份；它只示范身份读取。

先在自有应用创建 SP/凭证，安全保存 SK，并取得当前空间与自有应用 ID。替换以下占位值；这些 ID 不是秘密：

```sh
SSO_API_ORIGIN='http://api.localhost:8083' \
SSO_ALLOW_LOCAL_HTTP=true \
SSO_EXPECTED_TENANT_ID='<当前空间32位小写十六进制ID>' \
SSO_EXPECTED_APPLICATION_ID='<自有应用32位小写十六进制ID>' \
go run ./examples/machine-resource
```

`/whoami` 不接受 query/body 或 User Cookie，也不从入站 tenant hint 选择空间。无授权/错误 realm/撤销返回 401；权威故障返回受控 503；所有响应 no-store。进程只在成功取得自己的监听端口后报告就绪，不接管已有服务。

示例运行后，匿名合同下载入口为 `http://127.0.0.1:8092/openapi.yaml` / `.json`；对应仓库文件：[示例 OpenAPI YAML](../api/machine-resource-example.openapi.yaml) / [JSON](../api/machine-resource-example.openapi.json)。合同包含一个业务 GET 与两个匿名元数据 GET。示例错误格式为 `{"error":"..."}`，区别于平台 RFC 9457；完整 schema 在自身合同中。

## 私有输入到机器换取与业务调用

[examples/machine-client.cjs](../examples/machine-client.cjs) 是当前本地环境的一次性服务端客户端：从 stdin 接收三个字符串字段，使用固定 loopback 协议入口执行 Basic AK/SK 换取，然后以 Bearer 调用独立示例的 `/whoami`。成功只输出经过验证的身份元数据；错误仅输出固定阶段/HTTP 状态。它不输出 token/SK，不缓存凭证/令牌，不自动重试业务操作。

将以下结构的真实值通过秘密管理系统注入 stdin，或保存到 Git 忽略的本地私有文件（文件 0600、目录 0700）；示例文本是占位，不是可用凭证：

```json
{
  "access_key": "ak_<credential32>",
  "secret_key": "<首次创建时保存的43字符SK>",
  "application_id": "<自有应用32位ID>"
}
```

已有私有输入文件时，命令行只包含文件路径：

```sh
node examples/machine-client.cjs < .local/machine-client.json
```

不要把真实值填入命令行参数、源码、共享文档或共享终端输出。客户端固定使用当前本机端口 4464 和 8092，不提供任意 URL 配置；它是本地接入参考，生产交换请使用标准 OAuth 客户端和秘密管理设施。Go SDK 的范围是在线验证，尚未提供凭证管理、轮换代理或 token 缓存器。

## 实际验收

运行 `scripts/console machine-sdk-test`。它构建 Go SDK 探针和独立服务，以全新合成账户执行 User PKCE 管理和真实 AK/SK 机器链路，并在业务资源侧验证撤销、SP/应用停用恢复、到期、错误租户、User realm/Cookie 拒绝和匿名合同。秘密只经进程内存/stdin/协议头传递，测试将实际 SK/JWT 与候选文件、SSO 日志、示例进程输出比较，固定演示数据受清理保护。实际结果见 [validation.md](validation.md)。

Go 单元与竞态测试覆盖可信原点、TLS 默认信任、重定向、Cookie、无缓存、严格 JSON、过大/外空间/错误 realm 响应、错误正文排除、输入及取消；HTTP 模拟与真实 Hydra/主库/独立服务证据分别记录。没有执行 GCP/CI/部署或 marketstars 联调。

## 离线文档包

完整导出包含本文、全部合同、SDK/独立服务/客户端最小公开源码和 go.mod/go.sum。解压到独立目录后，可用已有 Go 工具运行 `go test ./pkg/machineidentity ./examples/machine-resource`；这两个包只使用标准库与同包公开合同。文档渲染不访问外部网络，真实业务示例仍需要已运行的本地 SSO。私有 AK/SK 输入与任何恢复资料均不进入导出。
