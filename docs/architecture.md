# 架构与数据边界

状态：实施设计，尚未通过生产验收。基于原始设计 v1；冲突需新增 ADR，不得静默修改原文。

## 系统边界

```mermaid
flowchart LR
  Browser[浏览器] --> BFF[应用/控制台 BFF]
  Browser --> Auth[可信认证 UI]
  Browser --> Hydra[Hydra 公共协议端点]
  Auth --> Identity[Go identity-api]
  Identity --> Admin[内网 Hydra Admin]
  Hydra --> Hook[内网 token-policy-hook]
  Hook --> SQL[(业务 MySQL 主库)]
  Identity --> SQL
  Hydra --> HSQL[(独立 Hydra 库)]
  SQL --> Worker[outbox worker]
  Worker --> Events[Pub/Sub / 审计归档 / 邮件]
  BFF --> API[业务 API + SDK]
```

Hydra 是唯一 token 签发者。浏览器不能调用 Admin 或 Hook。登录页面在固定认证 origin 下运行，品牌配置只能使用受控文案、颜色和经审核资源；控制台与登录域分离。公网 ingress 必须显式允许路径，不能将整个管理服务反向代理到公网。

## 部署与代码所有权

| 单元 | 权威数据/职责 | 接口 | 失败行为 |
| --- | --- | --- | --- |
| identity-api | User、Tenant、Membership、Org、Session、RBAC | 可信交互/管理 API | 资格或事务失败即拒绝 |
| token-policy-hook | 读取主库资格、版本、上下文 | 内网认证 POST | 超时、未知 client、缺上下文全部拒绝 |
| Hydra | 协议状态、code、refresh、签名密钥引用 | OIDC/OAuth + 内网管理 | 禁止业务服务直接改表 |
| event-worker | outbox 投递、补偿、通知 | 幂等消费者 | 按 event_id 去重、版本防乱序 |
| console BFF | 自身会话、CSRF、OIDC callback | 同源 UI API | token 不进入浏览器存储 |
| SDK/资源 API | 资源归属、验签、缓存水位 | 本地校验/高风险权威查询 | 无法证明归属或权限即拒绝 |

Go 模块化单体共享业务模型，但 Hook 单独部署和扩容。缓存不是权威源。数据库、Hydra Admin 不对公网开放。生产使用 Workload Identity、Secret Manager，业务库与协议库分离账号及 migration 权限。

## 领域关系

```mermaid
erDiagram
  USER ||--o{ MEMBERSHIP : joins
  TENANT ||--o{ MEMBERSHIP : contains
  TENANT ||--o{ ORG_NODE : owns
  MEMBERSHIP ||--o{ ROLE_BINDING : receives
  SERVICE_PRINCIPAL ||--o{ ROLE_BINDING : receives
  SERVICE_PRINCIPAL ||--o{ CREDENTIAL : owns
  APPLICATION ||--o{ OAUTH_CLIENT : configures
  TENANT ||--o{ SERVICE_PRINCIPAL : owns
  ROLE ||--o{ ROLE_BINDING : grants
```

User 全局存在；Membership 决定租户资格。租户内外键必须包含 tenant_id；User/Application 等全局引用列入白名单。root 由租户行指向，组织移动锁定 org_version 并事务更新闭包、audit/outbox。禁止移除最后 active owner。

授权上下文绑定 client、subject、tenant、membership/session 或 credential、授权交易与到期时间。refresh 使用原上下文，不接受请求覆写 tid。每个 token 一个业务 audience、一个租户。M2M 通过 Credential registry 解析 tenant，浏览器 hint 不参与最终权限判定。

## 一致性与降级

身份/授权变更、审计、outbox 必须同事务；协议对象创建采用 pending → active 与补偿对账。先提交本地 revoked，再异步撤销协议对象。主库写入权威不明时停止签发，不能读副本继续发 token。已在途 Hook 与禁用存在竞态，普通读的最坏撤销窗口 330 秒，高风险执行前重新查询主库。

部署首选东京三可用区、热备地区待服务可用性核实；单写与 fencing 优先。异步 DR 无法保证安全状态 RPO=0，恢复屏障、旧会话/refresh 清理和旧 AK 冻结是恢复门槛。99.99% 仅为待实测目标。

## 尚未冻结

Hydra 稳定版本、镜像 digest、Go/SDK 版本、MySQL 8.4 兼容性必须由 M0 真实测试冻结。Cloud SQL HA、跨区恢复、许可证维护状态、邮件供应商和域名在后续验证；本地 Docker 成功不能替代这些证据。


## 当前本地实现

实际业务登录现使用独立 Hydra 引擎、业务主库 Hook、中央 MFA 会话和两套 BFF，见 [ADR 0005](adr/0005-trusted-login-mfa-bff.md)。Hook 在每次 code/refresh 签发查询主库；示范资源请求也实时检查 grant，未使用目标架构的 330 秒缓存窗口。两套示范 client 属于同一个 Application，不能据此推断完整跨产品授权已实现。核心、应用 A/B 三网隔离及每应用数据库视图是本地权限边界；云端网络/IAM 方案待 SRE 实施验收。
