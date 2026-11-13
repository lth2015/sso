# 服务镜像打包

CI 将 React 构建结果嵌入 Go BFF，执行 Go 测试并交叉编译 Linux amd64 二进制，随后独立构建四个非 root 镜像。镜像包含系统 CA 证书，运行时无需 Node.js。

| 镜像后缀 | 编译入口 | 容器入口 / 监听端口 |
| --- | --- | --- |
| console-bff | cmd/demo-app | /app；3000 |
| platform-api | cmd/platform-api | /app；8083 |
| identity-api | cmd/identity-api | /app；8080，启用 SSO 时含 8082 Hook |
| event-worker | cmd/event-worker | /app；无 HTTP 监听 |

PR 只测试和构建，不登录、不推送。master 的 push 或手动运行在 master 上发布至 `ghcr.io/<owner>/<repo>-<service>`，标签为完整提交 SHA 和 `master`。其他分支手动运行只验证构建。发布后的各镜像摘要显示在 Actions 摘要中；Kubernetes 推荐引用摘要，master 标签用于开发跟踪。各镜像独立发布，部署前确认同一次运行的四个镜像均成功，避免混用版本。

Hydra 使用经验证的官方镜像 digest，不在此流水线重新打包。生产镜像不包含 protocol-public、demo-admin、M0 hook 或数据库迁移工具；迁移应由独立、受控任务执行。旧 deploy/local 镜像文件仍供本地实验使用。

## 运行限制

这是打包流程改造，不代表生产运行配置已经完成。console-bff 暂时复用 cmd/demo-app，需要 LOCAL_DB_KIND=console 和对应 console client 配置。当前程序仍要求 SSO_MODE=local-accounts，数据库地址、认证 origin、上游地址等包含本地固定配置，BFF 也仍依赖业务库受限视图。上线前必须完成可配置化、独立数据库账号、HTTPS Cookie、网络策略及运行验收。不得仅靠设置 local-accounts 将这些镜像视为已通过生产验收。

CI 中不注入数据库密码、Token 加密密钥或生产配置，这些由部署环境提供。本流程不自动部署 Kubernetes。
