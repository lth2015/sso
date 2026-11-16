# SSO 自动发布与 QA 部署

参考 yuha 的测试、产物发布、QA 部署顺序，在现有 `publish-images.yml` 内通过 job 依赖执行，避免跨 workflow 误用其他提交的产物。

## 触发与发布范围

- 向 `master` 提交或合并 PR：构建 React、运行 Go 与 BFF 隔离测试、编译四个服务及迁移工具、发布 GHCR 镜像；五个镜像全部成功后，先迁移两个数据库，再更新配置和应用。
- 目标为 `master` 的 PR：测试与构建镜像，不发布、不获取集群凭据、不部署。使用 `pull_request`，不使用 `pull_request_target` 执行 PR 代码。
- 在 `master` 手动运行：同样发布和部署，可用于重试。其他分支手动运行只验证构建。
- 若要改为其他指定分支，需同时修改两个事件的 `branches`、镜像发布条件及 deploy 条件里的 `master`，并同步镜像分支标签和 GitHub Environment 分支规则。

部署管理 `nss-sso`、`sso-bff` 中四个指定 Deployment 的镜像与完整环境变量引用，以及四份业务 ConfigMap。新增 `db-migrate` 镜像在两个命名空间中运行一次性 Job，两个数据库迁移均成功后才允许配置与应用更新。镜像均使用本轮产物中的摘要；应用更新后回读配置、镜像、版本和就绪副本，并验证公网页面与登录跳转。

配置文件、迁移凭据、首次准备、权限和失败处理详见 [QA 配置与迁移](../../deploy/qa/README.md)。

## 首次接入

在 GitHub 仓库创建 `qa` Environment，并配置：

| 类型 | 名称 | 内容 |
| --- | --- | --- |
| Secret | `GCP_WORKLOAD_IDENTITY_PROVIDER` | `projects/321823436276/locations/global/workloadIdentityPools/sso-github/providers/sso-qa` |
| Secret | `GCP_DEPLOY_SERVICE_ACCOUNT` | `sso-github-qa@netstars-ai-projects-qa.iam.gserviceaccount.com` |
| Variable，可选 | `QA_DEPLOY_RUNNER` | 默认 `ubuntu-latest`；也可设置可访问集群的专用 Linux runner 标签，需 Python 3 和支持 Node 24 的 runner 版本 |
| Secret，可选 | `GKE_USE_DNS_ENDPOINT` | GitHub 托管 runner 使用 DNS 入口时设置 `true`，集群必须先启用 DNS 外部访问 |
| Variable，可选 | `GKE_USE_INTERNAL_IP` | 有内网访问路径的 self-hosted runner 可设为 `true`；不能与 DNS 入口同时开启 |
| Variable，可选 | `KUBECTL_VERSION` | 默认 `v1.36.0`，与当前 QA 集群 1.36 版本匹配 |

不再需要 `QA_KUBECONFIG_B64`、`QA_KUBE_CONTEXT` 或服务账号 JSON 私钥。部署 job 单独授予 `id-token: write`，使用 `google-github-actions/auth@v3` 兑换临时身份，再由 `get-gke-credentials@v3` 生成临时 kubeconfig；两个 action 在结束时清理凭据。构建和 PR job 不授予 OIDC 权限。

Provider 只信任仓库 `lth2015/sso`（ID `1452342386`）、所有者 ID `12841814`、`refs/heads/master`、`repo:lth2015@12841814/sso@1452342386:environment:qa` 和该仓库 `publish-images.yml` 工作流。PR 无法满足条件。GitHub `qa` Environment 也应只允许 `master` 部署；全自动部署不要配置必须人工批准。

使用独立 Pool `sso-github`，不更改集群现有的 `netstars-ai-projects-qa.svc.id.goog` Pool。服务账号的自定义 IAM 角色仅提供集群元数据读取、DNS 连接和 API 配额使用权限；Kubernetes Role 授权指定 Deployment、业务 ConfigMap 和迁移 Job，不直接授予 Secret 读取权限。迁移 Job 的可执行范围由额外的准入策略约束。

2026-11-16 已确认：集群公网 IP 启用了固定 IP 白名单，用户已启用 DNS 外部入口（`allowExternalTraffic=true`）。身份联合不会绕过网络限制。GitHub 托管 runner 需先启用受 IAM 控制的 DNS 入口；self-hosted runner 可保留现有网络限制，并使用已有允许的访问路径。DNS 入口的连接权限由 `container.clusters.connect` 控制。

集群必须提前具备四个 Deployment、运行配置、数据库结构以及拉取 GHCR 镜像的权限。镜像发布仍使用仓库 `GITHUB_TOKEN` 的 packages:write 权限；私有镜像的集群拉取凭据由集群侧配置。

参考：[GitHub OIDC 认证](https://github.com/google-github-actions/auth)、[GKE 凭据 Action](https://github.com/google-github-actions/get-gke-credentials)、[GKE DNS 入口](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/latest/network-isolation)。

`deploy/kubernetes/` 仍保持本地忽略，不作为云端流水线输入。初次资源创建与配置变更继续走现有基础设施流程。Hydra、Gateway 和 Secret 值不由本流水线修改。数据库迁移必须与仍运行的旧版应用兼容，不支持自动回滚 DDL。

## 失败与回退

测试或任一镜像发布失败时，不执行部署。部署串行执行，不会取消进行中的部署；获取部署权限之前会拒绝已被分支新提交取代的版本，避免较慢的旧构建覆盖新版本。

数据库迁移、ConfigMap 和四个 Deployment 的更新不是跨资源原子操作；API 错误或 rollout 失败可能留下部分服务已更新。失败会将 Actions 标红；摘要保留更新前镜像及本次镜像。检查工作负载状态与事件后，可逐一将 Deployment 镜像恢复为摘要中的上一版本，再检查 rollout。流水线不自动回退数据库或执行自动回滚。

配置提交到 GitHub、Environment 和集群访问权限就绪后才会实际启用。本地检查无法证明 runner 的网络、凭据和集群权限有效；首次成功部署需在 Actions 中验证。

QA 部署身份的 Kubernetes 授权清单：`deploy/ci/qa-deployer-rbac.json`，由集群管理员一次性应用；流水线不能自行提升权限。

部署完成后回读镜像、observedGeneration 和就绪副本，并检查公网账户页面及 BFF 到身份登录页的 OAuth 跳转；不自动提交账号密码或发送邮件。
