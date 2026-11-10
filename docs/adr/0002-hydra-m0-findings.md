# ADR 0002：Hydra 2.3.0 M0 协议适配结果

状态：本地候选验证；不等于生产版本冻结。

## 已核对的制品

- Hydra v2.3.0，源码提交 `ee8c339ddada3a42529c0416897abc32bad03bbb`，Apache-2.0。配置与 Hook 实现以此 tag 为准：[schema](https://github.com/ory/hydra/blob/v2.3.0/.schema/config.schema.json)、[Hook](https://github.com/ory/hydra/blob/v2.3.0/oauth2/token_hook.go)、[license](https://github.com/ory/hydra/blob/v2.3.0/LICENSE)。运行镜像 digest 见 Compose。
- MySQL 镜像固定 digest，当前实际版本 8.4.13；MySQL Community Server GPLv2，容器发行依赖需在生产 SBOM 中另行审查。
- Go 1.27.3：官方当前 stable distribution，安装脚本校验官方 SHA256；go-sql-driver/mysql v1.9.3 与传递依赖固定在 go.mod/go.sum，启用 Go checksum database。未引入 Hydra SDK，实验直接调用固定版本 Admin HTTP API；后续 SDK 另行冻结。

“目前检索到最高 v2 tag”不构成供应商维护承诺。进入生产仍需确认维护周期、依赖漏洞与镜像 SBOM；未宣称已完成全面安全审计。

## 真实适配发现

1. `oauth2.token_hook` 同时支持 URL 与带认证对象。云环境下使用完整 JSON 环境变量；分散的嵌套环境变量未正确产生认证 header，测试发现并修复。Hook 仍要求密钥，不取消调用方认证。
2. 容器内部 `hook`/`mysql` 需追加到 NO_PROXY/no_proxy，保留原有代理配置；外部下载仍通过平台代理和 CA 验证。
3. authorization_code Hook 调用时 GrantedScopes/GrantedAudience 为空，Subject 和 consent session extra 已存在。必须读取可信 consent 步骤绑定的 scope/audience/context，再与权威策略比较。不能把空 grant 默认视为允许，也不能从 token 请求表单推导 tenant。M0 test harness 是唯一生成合成 consent 的适配器；未实现公共 accept 登录 API。
4. Hook 响应的 access_token extra 由 Hydra 放入 JWT `ext`；标准 iss/sub/aud/exp 仍由引擎生成。refresh 保留服务器上下文，用户 token 的 sub 为合成 User ID；M2M sub 为 client_id。
5. access token 配置 5m；真实样本的 exp-iat 可因持久化/秒级取整出现 301 秒。测试明确限制到 301 秒，不使用宽泛时间容差掩盖异常。
6. Hook 不可达时可能返回 OAuth server_error，也可能在服务端 deadline 后直接关闭连接。这两类结果分别视为不可用，均未取得 token；不能将其描述为受控 2 秒内错误响应。DB 锁等待超过 Hook 的 1 秒 deadline 时拒绝发放；可用性与超时总预算仍需生产适配验证。

## 不可误用的实验边界

M0 policies 是合成权威上下文，每 client 固定一个 tenant/context；没有真实 User/Membership/Session 数据模型，不支持生产多标签/多交易和跨租户客户端。观察表只证明 Hook 执行及结果，不等于完整生产审计账本。客户端与策略由本地测试管理，不能对普通调用者开放。

Hydra Admin 在本地为测试绑定 loopback，HTTP/--dev 仅此实验允许。不能部署此 Compose 到生产，不能把 harness 的合成 subject/consent 接受逻辑接到登录 UI。

## 继续推进业务前的门槛

Cloud SQL HA/重连验证、真实每交易/refresh chain 绑定、logout/remembered consent/prompt=none/max_age、签名 key 轮换、镜像漏洞/维护确认仍须完成。当前交付是可运行 M0 实验工程，不声明整个 M0 架构冻结或 M1–M5 完成。不得跳过门槛开始扩写生产业务。
