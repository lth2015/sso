# 当前交付验证记录

日期：2026-11-10。代码阶段交付 M0 实验工程与静态 UI 原型；生产功能未实现。

| 检查 | 实际结果 | 边界 |
| --- | --- | --- |
| `scripts/install-go` | 官方 Go 1.27.3 下载、SHA256 验证及版本检查通过 | Linux amd64 |
| `scripts/m0 prepare` | Go 测试通过：1 个表驱动测试、17 个子用例；静态编译和镜像构建通过 | 非生产 Hook |
| 新数据库启动 | 独立 `sso-m0-validation` 项目全新 volume 初始化、Hydra migration 与 readiness 通过 | MySQL 8.4.13，不是 Cloud SQL HA |
| `scripts/m0 test` | 16 项真实协议/数据库集成测试全部通过 | 合成身份/上下文；不等于完整平台验收 |
| `node tests/ui/prototype.cjs` | 桌面/手机、租户切换、弹窗、Escape/焦点、无 JS 错误通过 | 静态交互，不连接身份服务 |

故障测试 11 区分 HTTP 失败和服务端关闭连接，两者都未获得 token；没有将连接关闭描述为标准 OAuth 错误响应或满足延迟目标。故障测试 16 注入真实主库行锁等待，验证所有 grant 在 authority deadline 后无 token。

未执行：Cloud SQL HA/DR、生产多租户数据模型、真实登录/MFA/邮件、完整 React/BFF、签名密钥轮换、logout/remembered consent/prompt=none/max_age、压测、渗透/供应链全面审计、生产部署。详见 ADR 0002。

敏感配置仅存于被忽略的 .local；原始上传文档保持字节一致。Git 提交/推送不是生产发布，也未扩大仓库访问权限。
