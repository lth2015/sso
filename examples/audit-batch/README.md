# 本地 User 审计批次 CLI

从可信 OAuth 客户端的内存通过 **stdin** 传入 JSON，不把真实 Bearer 放入参数、环境、文档、聊天或终端历史；此程序不获取/缓存 token，不创建身份，不持久化输入。

```json
{"access_token":"<raw User access token>","tenant_id":"<expected 32-hex tenant>","kind":"management","limit":50}
```

`kind` 为 management、machine（机器签发事件，调用者仍为 User）或 policy_history。前两类需要 audit.read，第三类需要当前 owner，且必须另传 `role_id`；旧两类明确拒绝 role_id，避免误读资源。策略历史输入示例：

```json
{"access_token":"<raw User access token>","tenant_id":"<expected 32-hex tenant>","kind":"policy_history","role_id":"<expected 32-hex role>","limit":20}
```

三类都需要 platform:read。limit 省略/0 默认 50，范围 1–100；可选 after 为上批 next_cursor。输入最多 32 KiB，重复/未知字段、多文档/非法类型拒绝。每次只读一个批次，没有自动翻页/重试；stdout 只输出验证后的受控 JSON，stderr 只有固定错误类别。策略历史格式独立于原冻结 sso.audit.batch.v1，包含受控 before/after，不是 audit.read 导出的附加字段。

在仓库构建 `go build -o /tmp/sso-audit-batch ./examples/audit-batch`，然后由可信进程为它提供 stdin。固定本地 API 入口 `http://api.localhost:8083`，SDK 保留 Host 并拨号回环；这个 CLI 不是可选择任意远程目标的凭证转发器。生产集成使用 [SDK](../../pkg/useraudit/README.md) 的可信 HTTPS 配置。Ctrl-C/SIGTERM 取消当前读取，失败没有部分正常输出。

这是程序化调用参考；OAuth code+PKCE、scope/tenant 选择与受信任的登录交互见 [公共 API](../../docs/runbooks/public-api.md)。真实自动验收 `scripts/console audit-client-test` / `scripts/console audit-policy-client-test` 只创建自己的合成账号/应用，Bearer/SK 只留在进程内存/stdin。
