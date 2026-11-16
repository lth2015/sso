# QA 业务配置与迁移

范围仅为 `nss-sso`、`sso-bff`。`ory-hydra` 和共享 Gateway 不由本流程管理。

## 配置的唯一来源

`configmaps.json` 全量定义四份业务 ConfigMap：identity-api-config、platform-api-config、event-worker-config、sso-bff-config。发布使用带 resourceVersion 的 replace；仓库删除的数据键也会从集群删除，遇到并发修改则失败，不强行覆盖。系统维护的 kube-root-ca.crt 不在管理范围，不执行 namespace 级 prune。

`runtime.json` 全量定义四个受管容器的环境变量引用。流水线在一个 Deployment patch 中同时更新镜像、完整 env 列表和配置校验和；清除旧 envFrom，避免遗留值覆盖新配置。其他容器和 Deployment 字段不在本次管理范围。

ConfigMap 仅允许经过审核的非敏感键。数据库连接标识、用户名、发件人地址及所有密码、令牌、加密密钥放入已有运行时 Secret。GitHub 部署身份不直接读取或写入 Secret。集群 Secret 的真实值不入库、不写日志、不通过命令行参数传递。

一次性管理员准备：

```sh
python3 scripts/prepare-qa-secrets --check
python3 scripts/prepare-qa-secrets --prepare
# 首次迁移现有配置；保持当前镜像，仅切换等价 Secret / ConfigMap 引用并清除敏感配置键：
python3 scripts/prepare-qa-secrets --activate
```

脚本使用明确的 QA context（可通过 QA_KUBE_CONTEXT 指定），先检查全部对象，再在内存中将现有值补充到 Secret；不覆盖已有不同值，也不输出明文。`--prepare` 不重启应用；`--activate` 会滚动更新环境变量，须继续检查 rollout 和 `scripts/verify-qa`。

## 数据库迁移凭据

POC 按用户要求复用应用账号和密码，不创建独立迁移凭据：

| Namespace / 目标库 | 凭据 Secret |
| --- | --- |
| nss-sso / sso_business | identity-api-runtime |
| sso-bff / sso_console | sso-bff-runtime |

两个 Secret 均包含 DB_HOST、DB_PORT、DB_NAME、DB_USER、DB_PASSWORD、DB_TLS_SERVER_NAME。一次性准备脚本从现有环境配置补齐连接字段，不改变账号或密码。

复用账号必须具有对应库内迁移所需的权限；现有 SQL 包含 CREATE TABLE 和外键，需要 CREATE、REFERENCES 等 DDL 权限，以及 schema_migrations 的 SELECT/INSERT/UPDATE 权限。权限不足时迁移失败并阻止后续发布，不自动提权或修改数据库授权。生产环境应拆分迁移身份与应用身份，因为应用账号拥有 DDL 权限会扩大应用被攻破后的影响。

迁移 Job 使用已有 CA Secret：nss-sso/sso-cloudsql-ca、sso-bff/sso-bff-cloudsql-ca；要求校验 CA 和服务器名，不跳过 TLS。私有数据库 hostAliases、镜像拉取引用沿用目标 Deployment 的现有配置。

## 发布顺序与失败边界

1. 测试、编译四个应用及 db-migrate，发布五个摘要固定的镜像。
2. 校验本轮所有产物、部署对象和配置范围。
3. 在两个命名空间分别创建固定名称的 sso-schema-migrate Job，分别执行内嵌的 business / bff SQL。
4. 两个 Job 均成功后，替换四份 ConfigMap，再更新镜像、完整环境变量引用和配置校验和。
5. 等待 rollout，回读镜像、配置及副本，最后要求公网页面及 OAuth 登录跳转连续三次成功。

任一迁移失败或超时，不更新 ConfigMap 或 Deployment。两个库之间没有分布式事务，一个可能成功、另一个失败，因此迁移必须与仍在运行的旧应用兼容。配置替换和应用更新同样不是跨资源原子事务；失败后应审查实际状态，不声称全部自动回退。

执行器复用 schema_migrations(version, checksum)，按顺序只执行未记录的 SQL；已执行文件不得修改。使用同一 MySQL 会话持有命名锁，拒绝校验和不匹配和数据库中存在但当前版本缺失的迁移。SQL 执行前写入 incomplete 标记，DDL 部分失败后阻止自动重试；管理员核对实际表结构并修复后才可清理该标记。MySQL DDL 不支持整个迁移的自动事务回滚，不自动执行 down migration。

两个 Job 不自动重试（backoffLimit=0），有执行时限并保留一天供管理员诊断。部署身份仅查询 Job 状态，不读取 Pod 日志；管理员可查看 Job 日志，其中执行器只输出版本及受控错误，不输出 SQL、密码或连接配置。

## 集群管理员权限初始化

先安装 Job 准入策略，再扩充部署角色：

```sh
kubectl --context=YOUR_QA_CONTEXT apply -f deploy/ci/qa-migration-policy.json
kubectl --context=YOUR_QA_CONTEXT apply -f deploy/ci/qa-deployer-rbac.json
```

原有四个 Deployment 的 patch 权限保留。新增权限限于四份 ConfigMap 的 get/update，以及两个业务 namespace 中迁移 Job 的 create、固定 Job 的 get/delete。Kubernetes RBAC 不能按名称限制 create；准入策略进一步限制为固定 Job 名、指定迁移镜像仓库的摘要、指定凭据和 CA 引用、非 root 只读容器及执行时限，禁止换启动命令、挂其他 Secret、使用主机权限或 API Token。策略只约束 GitHub 部署账号，不影响管理员及其他工作负载。

发布身份仍然能运行获准的迁移镜像并使用应用账号的 DDL 权限；必须继续保护 master、发布工作流和镜像发布权限。没有授予 Secret API 读取权限，不代表获准的工作负载不能使用其挂载凭据。

本地验证：`python3 scripts/reconcile-qa --validate`、`python3 -m unittest discover -s tests/ci -v`、`bash scripts/test-migrations`、`go test ./...`。
