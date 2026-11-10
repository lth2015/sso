# 当前交付验证记录

日期：2026-11-10。本地交付 M0 协议实验、静态 UI 原型和 M1 数据仓储切片。GCP 部署/云端运维按用户要求由 SRE 负责，本轮不执行。

| 检查 | 实际结果 | 边界 |
| --- | --- | --- |
| 固定工具链 | 官方 Go 1.27.3 SHA256 验证及版本检查通过 | Linux amd64 |
| `scripts/m1 prepare` | Go policy 测试、静态编译和镜像构建通过 | 普通 Go run 中 DB 集成测试明确跳过，不能替代下一项 |
| 新数据库启动 | 独立 sso-m1-validation 项目全新 volume、M0 上下文迁移、Hydra migration、业务迁移通过 | MySQL 8.4.13，本地容器 |
| `scripts/m0 test` | 28 项真实协议/数据库测试全部通过 | 合成账户，真实 Hydra；不是完整身份平台 |
| `scripts/m1 test` | 10 项真实业务 MySQL 集成测试全部通过 | 无 mock DB；无公开 HTTP 管理入口 |
| 迁移重复执行 | 保留数据与本地凭据的初始化/迁移可重复运行 | 当前幂等 DDL 和 checksum guard |
| UI 原型 | 前次桌面/手机、租户切换、弹窗、Escape/焦点、无 JS 错误检查通过；本轮 UI 未改动 | 静态交互，无真实登录 |

M0 新增证据：同 client 多交易/租户与独立 refresh 链、原子协议绑定、资格/客户端版本和绝对期限、错误 subject/sid、SSO/remembered consent、prompt=none/max_age、退出适配与 consent revoke、签名 key 轮换、refresh 请求 scope 越权拒绝。退出先撤销平台上下文再接受协议 logout；已有 JWT 在 TTL 内仍可验签，不等于实时授权允许。断连测试区分 HTTP 错误和连接关闭，不把无 token 等同于满足延迟目标。

M1 证据：事务性引导、根唯一、组合 FK/跨租户 actor 拒绝、组织 dry-run/闭包/环/根/深度/版本、并发移动、最后 owner/并发暂停、审计 FK 失败的事务回滚、邀请邮箱验证/一次性与并发消费/过期、组织归属不隐式授予角色。

本轮未交付：注册/密码/MFA/邮箱验证服务、邀请邮件发送、公开业务 API/BFF、真实控制台、完整 scoped RBAC、outbox worker，以及 M0 与业务资格的正式集成。生产密钥托管、漏洞/维护审查、负载/安全评审、云端 HA/DR/SLO 验收尚未执行；相关云端部署由 SRE 负责。

原始上传文档字节保留；本地随机秘密不进入 Git。推送代码不是生产发布。
