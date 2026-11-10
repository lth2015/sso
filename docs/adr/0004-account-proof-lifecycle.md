# ADR 0004：先验证邮箱再设置密码的账户生命周期

状态：本地 M2 切片，尚未形成完整 SSO 登录服务。部署与云端运维由 SRE 负责。

## 注册与密码恢复

注册申请只接收邮箱，返回统一 202；邮箱归属证明成功后才设置密码、创建全局 User、已验证登录别名、原始邮箱 profile、密码凭证、个人 Tenant、Root、Membership 和 owner。挑战消费、身份事件、租户 audit/outbox 与这些对象同事务提交。避免“先由任意申请人替邮箱设置密码，再让邮箱所有者点击确认”的预注册接管问题。已有别名不会被重新注册覆盖密码。

密码用固定格式 Argon2id（64MiB、t=3、p=1、16 字节随机 salt、32 字节输出），通过 Go 官方 x/crypto 实现；固定参数解析拒绝畸形/超成本散列，常数时间比较输出。密码策略 12–128 个 Unicode 码点、最多 512 字节，保留空白与原始 Unicode，不静默修剪。并发哈希池上限 2，容量不足返回 503；不能降低成本换吞吐。

注册挑战有效 24 小时，恢复挑战 15 分钟，均为 256-bit CSPRNG 随机 token，DB 只存 SHA256 摘要、purpose、到期及消费状态。重置挑战还绑定创建时 User 的 security_version；成功后版本递增，旧密码及同版本的其他恢复挑战失效。并发消费只有一笔成功，失败不留下半个个人空间。

注册/恢复请求不通过响应正文区分账户是否存在；未承诺数据库路径严格恒定时间。内部 CheckPassword 对未知账户也执行 dummy Argon2id，并在密码因素通过后重新确认 User 状态/版本。它不是公开登录接口：不能单凭这个返回值创建中央会话、跳过 MFA 或签发 OAuth token。

## 加密通知与本地交付

挑战摘要和 AES-256-GCM 加密通知在同事务创建；AAD 绑定 notification ID、规范化邮箱和 purpose，解密后再核对 payload ID、purpose、recipient、expiry。邮箱地址是受控 PII，不属于凭证；原始 token 不进入审计或明文 outbox。

worker 用 SELECT FOR UPDATE SKIP LOCKED 获取一条待投递记录，仅向私有本地目录交付，不发送外部邮件。文件先写 0600 临时文件、fsync，再原子 link 发布，已存在时仅接受内容完全一致的普通私有文件，防止提交结果不确定时重复/覆盖。目录必须 0700，worker 使用绑定目录实际 UID/GID，所有 capabilities 关闭。交付失败保留加密 payload 并退避重试，过期任务不交付；成功/过期后擦除队列 ciphertext。日志只有受控错误类别，不含路径、邮箱、token 或正文。

本地环境保留 IDENTITY_MAIL_KEY 及其 fingerprint marker，防止重跑时意外更换 key。快照恢复也必须保留它们；真正轮换需要显式解密/重加密流程。本地 key 和邮箱目录不是生产密钥托管或 SMTP 方案。

## HTTP 与 UI 边界

四个匿名 JSON 接口见 api/accounts.openapi.yaml。严格同 Origin、无开放 CORS、拒绝未知字段/多 JSON/body >4KiB/查询参数，统一 no-store、nosniff、no-referrer、CSP。IP 限流只信任 RemoteAddr，账号级限流使用规范化邮箱摘要，map 最大 10,000 项，满时拒绝；同时有全局并发和哈希池保护。它们是本地单进程限流，生产需边缘与分布式保护。

真实账户页面使用同源静态脚本/样式。验证 token 只通过邮件链接 fragment 输入，脚本读取后立即 replaceState 清除；同页 hashchange 也处理，密码不进入 URL、本地/会话存储或 cookie。操作完成后清空密码并要求从应用发起登录，不自动制造 SSO 会话。

## 明确未实现

尚无 Hydra 可信登录交互、中央会话、MFA、BFF、真实邮件供应商、生产账号恢复审批与通知。M0 合成授权仍与此业务 User 权威状态分离；不能据 security_version 递增宣称现有 M0 JWT/refresh 已全部撤销。下一步必须把账户因素接入受 CSRF/Origin/交易状态保护的 Hydra 登录，再实施 MFA 与会话版本检查，而非新增 password-to-token 路由。
