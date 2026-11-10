# 本地 M2 账户验证与密码恢复

使用现有 checkout，无需 worktree。此切片只在本地开发和验收，不涉及 GCP 部署或外部邮件发送。

```sh
cd /workspace/sso
scripts/install-go
scripts/m2 prepare
scripts/m2 up
scripts/m2 test
node tests/accounts/browser.cjs
```

prepare 复用 M0/M1，安装锁定的 x/crypto 依赖，编译账户 API、通知 worker 和集成测试。up 应用当前业务迁移（包括 003 账户与 004 SSO），再启动账户服务和 worker。账户服务宿主端口仅绑定 loopback 的 8080，UI 路径 /account。不要创建公网预览、暴露管理端口，或将 --dev 配置作为生产部署。

m2 test 暂停本项目 mail-worker，防止真实 worker 与集成 fixture 争抢队列，测试结束后恢复；失败状态会保留。12 项测试中 11 项连接真实 MySQL，另 1 项检查限流器容量；普通 go test 未设置 SSO_ACCOUNT_TEST 时会明确跳过 DB 测试。密码包另有 2 项 Argon2id/输入策略测试。必须执行 m2 test 才能认定 DB/HTTP 生命周期通过。

浏览器验收连接真实本地 API、数据库和 worker；使用生成的合成邮箱，读取只属于该 fixture 的私有通知文件，完成验证、确认密码与恢复，并清理自己创建的账户/个人空间/通知。不会输出 proof、密码或加密 key。若失败，只打印阶段和错误类别。截图仅保存操作完成后的无凭证页面到 docs/ui/accounts-desktop.png、accounts-mobile.png。

## 私有通知与持久配置

.local/m0.env 包含新增 IDENTITY_MAIL_KEY；.local/account-key.id 绑定其 fingerprint，避免静默换 key。保留这些文件、原有 DB 密码及 MySQL volume。prepare 只更新自己管理的数值 MAILBOX_UID/GID，并原子保存既有环境值，不输出它们。

.local/mailbox 必须 0700。worker 以该目录的实际 UID/GID 运行并且没有 capabilities，只能写挂载目录；文件 0600，不允许通过放宽权限处理错误。测试 worker 没有 SMTP/外部投递能力。手工调试时邮箱文件含短期链接，应在处理或过期后清理自己的文件，不共享该目录或复制正文到日志。

可用 scripts/m2 mail-once 单次处理当前可交付通知。失败会退避且保留密文；错误日志只有固定类别。账户 API /health/ready 验证 003 迁移，功能就绪仍以集成测试为准。

停止使用 scripts/m2 down，保留 DB/密钥/邮箱；不要 down -v。重启使用 scripts/m2 up，必要时先 prepare 刷新源码对应的镜像。

## 当前范围

已实现注册邮箱证明、原子个人空间引导、Argon2id 密码、恢复挑战、安全版本递增、通知加密/本地可靠落盘、四个 HTTP API 和真实账户页面。账户接口不提供 token 旁路；可信登录、中央会话、MFA 与 BFF 现由 [本地 SSO 手册](local-sso.md) 说明。管理接口仍未公开；M0 合成身份实验继续独立。GCP 部署交由 SRE，对应生产邮箱、密钥托管和分布式限流留作正式集成。
