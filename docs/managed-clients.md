# 租户自有应用与公开 PKCE 客户端

此功能提供 owner 管理的应用创建、公开客户端登记、协议核对和不可逆撤销。公开客户端适用于桌面/原生场景：authorization_code + S256 PKCE、可刷新、无 client secret。当前固定 `openid offline_access platform:read` 和平台 audience，不支持 client_credentials、自定义 audience/scope 或服务端 secret；这些能力继续分阶段实施。

## 所有权与权限

新增应用由当前空间拥有，使用系统生成的唯一 code，并在创建事务中启用当前空间关联。已有平台/演示应用保持共享登记，不能被任意租户 owner 改写客户端。Application 的 managed 标记表示当前空间拥有它，而非“此空间已启用”。

所有管理操作需 active owner；创建/核对/撤销还需 platform:write、最近 300 秒 MFA。读取需 platform:read。reserved roles.manage 不可通过自定义角色委托；这是第一期客户端管理边界。应用所有权属于空间，不随 creator 是否仍是 owner 改变。每空间最多 20 个自有应用，每应用最多 100 份登记（包括撤销墓碑）。

## 状态机与失败恢复

1. 创建客户端先提交 pending/inactive 登记、随机 32-hex client_id、精确回调及审计/outbox，不调用引擎。pending 在可信登录和签发中均不可用。
2. owner 确认后执行 reconcile：从固定私有 Admin 读取；不存在则创建；随后读取并校验 ID、名称、精确回调、grant/response、scopes、audience、认证方式和所有权 metadata。确认后才在主库事务中激活、增加 client version 和写审计。
3. 任何协议配置/所有权冲突返回 409 protocol_client_conflict，不覆盖、不删除、不收编不匹配对象。协议创建成功但回包丢失或业务提交不明时，保留主库实际状态；先 GET 元数据，明确确认后再次核对，不能自动重发创建。匹配的 pending 协议对象可恢复登记。
4. 撤销先将主库状态改为 revoked/inactive 并递增版本；该客户端所有旧授权立即失去权威，后续 refresh 拒绝，其他客户端及中央登录保留。撤销不可恢复，相同最新版本再撤销为 no-op。
5. 对 revoked 执行 reconcile 仅核对并清理匹配的协议对象，保留撤销墓碑。即使协议对象尚未清理，Hook/资源检查仍拒绝。清理失败不恢复权威；不删除不匹配的对象。

核对与撤销复用同一租户事务锁，整个核对事务有 4 秒 deadline，协议请求另有 3 秒上限，并禁止重定向。普通并发核对不能在撤销后重新激活。协议对象的 absent 响应是本次观察，不能保证此前超时的远端事务永远不会稍后完成；本地 pending/revoked 仍始终拒绝。当前提供显式 owner 核对，定期补偿/对账 worker、告警和长期孤立对象清理仍待实现。

私有 Admin 适配器直接连接其固定内部服务，与外部出站 HTTP 代理分离；浏览器不能提交代理目标。公网 API 不暴露任意 Admin 路径/响应，公开客户端核对仅解码受控非秘密字段，不转发上游 client_secret、注册 token 或错误正文。

## 回调与协议边界

回调是长度不超过 255 的精确 ASCII URI，要求 HTTPS 和明确路径，或使用 `127.0.0.1` / `[::1]` 的 HTTP 回环与固定 1024–65535 端口。禁止 userinfo、fragment、通配符、反斜线、空白和自定义 scheme，不通过 DNS 推断归属，平台不抓取该 URL。当前不支持 RFC 8252 随机回环端口扩展、域名证明或动态配置编辑。

既有可信登录适配器仍复查实际请求 redirect、S256、scope 和 audience；Hook 每次 code/refresh 查询主库 active/版本。引擎注册本身不授予空间成员或 owner 资格，组织归属也不会授予客户端权限。新公开客户端没有 platform:write，不能用自己的 token 再创建客户端或管理平台。

## API 与 UI

公共/BFF OpenAPI 0.13.0 共 43 个业务操作，Playground 和 API 参考使用同一合同。

| 方法/路径（均以 `/v1/tenants/{tenant}` 为前缀） | 用途 |
| --- | --- |
| POST `/applications` | `{name}`，创建自有应用 |
| POST `/applications/{application}/clients` | `{name,redirect_uri}`，提交 pending 公开客户端 |
| GET `/applications/{application}/clients/{client}` | 读取自有客户端元数据与 ETag |
| POST `/applications/{application}/clients/{client}/reconcile` | `{}` + If-Match，核对/激活或核对撤销后的清理 |
| POST `/applications/{application}/clients/{client}/revoke` | `{}` + If-Match，不可逆撤销 |

client path 仅接受此功能生成的 32-hex ID；旧的字符串 client_id 继续在已有只读客户端列表展示，state=external、managed=false。新列表增加 managed/state，应用列表增加 managed；均不泄露密钥。客户端 ETag 为 `"client-<tenant32>-<client32>-<version>"`，绑定空间和不可变客户端身份。缺失 428、非法 400、旧版本/其他对象 412。App 路径、客户端与空间所有权必须一致，不能借用一个合法 ID 管理其他应用。

控制台“应用接入”提供创建应用、自有应用下登记公开客户端、查看/核对/撤销和清理。登记和生命周期均明确确认，协议错误/网络不明后禁用原提交，支持先刷新元数据；关闭或切换空间取消读取，不自动重放写入。Playground 的“读取最新 ETag”需同时填写 application/client ID，并读取对应客户端详情。

创建应用/客户端或局部撤销不增加租户 authz_version，也不批量锁 grant/BFF 行。客户端撤销依靠单调 security_version，使该客户端的旧授权不复活。应用停用/恢复继续使用关联版本及原 ADR 0013 规则。

## 验证与边界

界面截图：[桌面](ui/managed-client-desktop.png)、[手机登记](ui/managed-client-mobile.png)、[手机应用列表](ui/managed-applications-mobile.png)。

运行 `scripts/console clients-test`，配合 `scripts/api test`、`scripts/m1 test`。使用独立合成账户/自有应用/协议对象，清理器核实封闭账户集合及所有权，禁止触碰其他空间或共享客户端。真实协议测试包含新客户端 code/PKCE、refresh、只读 scope、撤销后 access/refresh 拒绝及协议清理；不将注入 HTTP 故障的测试当成真实 Hydra 验证。

新增 migration 008 只添加所有权/状态表，既有迁移校验和保留。周五演示固定标签与数据保留；CI/GCP/生产部署由 SRE 负责。本切片不构成 confidential/M2M、机器密钥轮换、生产 TLS/KMS 或外部 Marketstars 接通的声明。
