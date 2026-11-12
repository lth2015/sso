# 实施与验收计划

先提交设计到 master，再进入代码实施。当前已交付可信 SSO、控制台与租户 RBAC 第一切片；各阶段剩余功能在相应验收前均为待实现，不将原文的目标当成事实。

| 阶段 | 代码交付 | 通过门槛 |
| --- | --- | --- |
| M0 | 固定 Hydra/MySQL、本地启动、Go Hook、真实协议测试、运行手册 | code+PKCE/refresh/M2M、ext/aud/JWKS、拒绝/故障、重放测试；未验证项列明 |
| M1 | business migrations、tenant-aware repo、组织/成员、audit/outbox | 组合 FK、并发组织移动、最后 owner、事务回滚与隔离 |
| M2 | 可信登录 UI/API、密码/邮箱/MFA、BFF/SDK、双应用示例 | 真正 SSO、CSRF/state/nonce、租户固定、refresh/退出 |
| M3 | React 控制台、客户端、SP/凭证与补偿 | 一次性 SK、step-up、限权、orphan/pending 拒绝、日志脱敏 |
| M4 | scoped RBAC、授权接口与两类应用适配 | deny 优先、委托边界、资源归属、缓存撤销上限 |
| M5 | GCP IaC、监控、压测、HA/DR、安全恢复 | 真实容量/恢复证据、安全评审、试运行后才承诺 SLA |

M0 不通过不扩写业务功能。选择版本需记录源码引用、许可证、digest 与兼容性；“存在配置项”不等于“所有 grant 安全”。引擎短板、维护状态不明确、Cloud SQL 未验证均单列为剩余风险，禁止自行实现 OAuth 绕过。

每次交付保留：执行命令、测试数量与结果、代码 SHA、环境限制、未运行项。当前可运行证据见 runbooks/m0.md（代码阶段创建）。生产发布另行执行。

## 当前进展

M0 已通过 28 项真实协议检查；本地证明每交易/授权链绑定、刷新/撤销与签名轮换。云端 HA 按用户要求不验证，不记为通过。

M1 已提供组合外键隔离、组织闭包/移动、成员停用恢复、邀请、audit/outbox；M4 第一切片增加租户角色、六种操作权限、deny 优先及 owner 恢复边界。27 项业务数据库检查及回调配置边界检查包含并发最后 owner 保护、邀请创建者权限复查及角色删除后的绑定审计。

M2 已实现邮箱验证设密/恢复、个人空间原子创建、加密通知队列/本地私有邮箱、真实 Hydra 密码/TOTP/一次性恢复码、中央会话、按租户授权、主库 Hook、两个独立 BFF、串行刷新、单应用及全设备退出。BFF 各自使用独立 key、受限 SQL 视图和网桥；Admin/私有 Hook 不可从 BFF 网络访问。当前 Go 验证适配尚未作为多语言 SDK 发布。

公共 API 0.13.0 已提供 43 个业务操作和 OpenAPI 3.1 YAML/JSON，覆盖身份/空间、成员、组织、邀请、审计、自己的会话及角色/绑定管理。外部客户端使用 OAuth code+PKCE、Bearer scope 和平台 audience；写入需最近 MFA，资格/权限在业务事务内复查。普通成员默认拒绝，操作权限可分别委托；角色管理仅 owner。见 ADR 0006/0008。

M3 控制台第一切片已接入真实 React→BFF→公共 API，含多标签页空间隔离、上述管理页面及角色 UI。OAuth token 留在服务端，控件可见性不代替授权。真实浏览器的合同、权限、撤销和 UI 证据见 [验证记录](validation.md)；构建及运行见控制台手册。

后续重点：遗失认证器的受控恢复；动态 client、SP/机器身份及凭证生命周期；组织子树、Application 和资源范围授权；政策快照/审计归档、正式邮件与租户事件消费者。当前租户角色不完成完整 M4，既有 BFF 不替代长期 refresh、完整 step-up 或生产恢复屏障。

继续本地开发与验收；GCP 部署及云端运维由 SRE 负责，不等待云端项目/凭证。生产 TLS/Secure cookie、分布式限流、KMS/轮换、容量/安全/HA/DR/SLO 证据均需另行实施验收，不由本地结果推断。


API/文档切片已增加真实 Playground、逐操作运行时参考页、完整手册与离线 ZIP。OpenAPI 0.10.0 为 29 个业务操作，提供请求示例及角色 ETag；交互调试沿用现有 BFF/MFA/主库权限，不扩展业务授权范围。设计见 ADR 0009。


个人安全第一切片已增加 User 范围概览、其他会话批量撤销，以及独立可信域的恢复码重生成（当前密码+新 TOTP、旧码/全部旧会话撤销）。该切片当时公共操作 28 个，可信管理合同分列；认证器更换现已由后续 ADR 0011 实现，遗失设备受控恢复仍未完成。见 ADR 0010。

认证器更换已实现两个验证阶段和取消：独立暂存密文/请求、密码与旧 TOTP、确认新 TOTP 后原子切换并撤销全部旧权限。该切片公共合同 0.9.0 为 28 操作，新增能力在可信浏览器合同，见 ADR 0011。

个人安全事件已接入公共 API、控制台和 Playground。公共合同 0.10.0 增至 29 个业务操作；复用已有身份事件表与索引，按当前 User 查询与时间/ID 游标分页，不提供其他用户或私有 target/payload。此切片不替代独立审计归档、设备/登录失败监测和通知消费者，见 ADR 0012。

周五演示优先冻结一期主线：真实 MFA/双应用 SSO、空间/组织、只读角色、公开 API/Playground 和个人安全记录。通过本地私有演示数据与可重置彩排工具验收；完整 client/机器凭证、细粒度授权、受控恢复与正式通知/归档继续按后续切片实施。Marketstars 的实际接入尚待独立联调，不将两个示范 BFF 的结果记作该业务系统已经接通。见周五演示手卡。

应用接入第一切片已提供本空间已接入应用、只读客户端登记元数据、owner/近期 MFA/对象绑定 ETag 下的停用与恢复，旧 grant/BFF 依据关联版本原子失去权威并保留其他空间/应用。公共合同 0.11.0 为 34 操作；客户端创建/secret 发布轮换和机器身份仍待实施，见 ADR 0013。周五基线固定标签 demo-friday-2026-11-13，对应 5d8b46b，当前演示数据保留并另行复验。

组织生命周期已提供重命名、直属成员分页（organizations.read 与 members.read 同时成立）、移除归属、删除空叶子，公共合同 0.12.0 增至 38 操作。根组织受保护，非空节点不级联，结构删除/真实移除需重新授权；普通重命名仅改显示与 org_version。详见 ADR 0014。

自有应用与公开客户端登记切片：migration 008 提供所有权、pending/active/revoked 管理表；公开 PKCE 固定只读平台范围，无 secret，owner/近期 MFA/客户端 ETag；协议核对匹配后才激活，本地撤销先于远端清理，真实证据见 validation.md。共享应用客户端不能被任意租户改写。confidential/M2M/SP/机器凭证、周期性补偿/对账仍未交付。

## 2026-11-11：机器身份切片 0.14.0

新增 SP、一次性交付 AK/SK、双凭证轮换、独立机器 JWT/resource realm 与机器签发审计。User 管理合同 54 项，机器协议另列。旧阶段文字表示当时进度；当前范围和限制以 machine-credentials.md 与 validation.md 为准。下一步优先完善所有权交接、受控机器对账和外部资源接入体验；private_key_jwt、自定义业务授权、生产运维与 HA 尚未交付。

## 2026-11-11：外部在线验证 SDK 0.15.0

Go 服务端验证器、独立回环身份服务及私有 stdin 输入客户端参考已实现，主 User API 仍 54 项。独立示例自列 OpenAPI，SDK 每次在线主库验证，没有成功权限缓存/业务自动重放。机器自定义业务 ACL、token 缓存/轮换代理、生产运维仍在后续范围。实际验收以 validation.md 为准。

## 2026-11-11：受控审计导出 0.16.0

新增 User/BFF 审计批次读取与 UI JSON 下载，管理/机器来源独立、固定投影、最多 100 条、同空间/来源游标，支持 audit.read 只读委托。User 合同增至 55 项，Playground 可执行 54 项，一次性 SK 创建仍使用专页。实际主库、原生 API、浏览器下载/取消/迟到响应证据见 validation.md / ADR 0018。归档、签名存证及跨批冻结快照仍未实现；后续继续受控对账和邀请生命周期。

## 2026-11-11：邀请凭证更新 0.17.0

增加邀请详情与显式更新一次性凭证、资源 ETag、辅助修订版本，以及当前管理者责任与受控确认 UI。User/BFF 合同 57 项，Playground 可执行 56 项；目标邮箱不变，closed 不复活，旧 proof 失效，未知回包需先核对，实际无自动邮件。遗失设备 MFA 恢复、周期补偿/对账、正式邮件与完整归档仍为后续。见 invitation-lifecycle.md / ADR 0019 / validation.md。

## 2026-11-11：协议登记只读核对 0.18.0

已实现 owner 的自有 public client/machine credential 受控字段观察与 UI，User 合同 59 项、Playground 可执行 58 项。读前/读后主库复查、网络等待释放租户锁、固定时间/大小/字段投影与取消防迟到；没有自动修复、SK 恢复或周期补偿 worker。见 protocol-inspection.md / ADR 0020 / validation.md。

## 2026-11-11：User 审计客户端 0.19.0

交付标准库 Go 两类固定审计批次客户端及固定本地 stdin CLI，复用 59 项 User 合同，当前只读委托/撤销/realm 与真实独立进程已验收。SDK 不处理登录、存档或跨批快照；业务授权范围扩展、正式通知、遗失设备恢复与生产运维继续按后续切片实施。见 user-audit-client.md / ADR 0021 / validation.md。

## 2026-11-11：角色绑定选择器分页 0.20.0

复用现有合同，选择器显式加载 25 项，支持第 100 位之后成员及主成员后页直接进入；完整解绑列表、取消/迟到响应、异常清空和被 deny 的 owner 自身恢复入口保持。无新 API 或迁移，角色配额仍 100，不宣称全文搜索/冻结快照或正式 scoped RBAC 已完成。见 role-choice-pagination.md / validation.md。

## 2026-11-11：组织关联分页 0.21.0

组织关联独立加载成员与组织、明确确认、普通委托流程和未知写入结果核对已实现，复用原 59 项 User API/58 项 Playground；不改变授权/迁移或赋予角色。真实专项与文档见 organization-assignment.md / validation.md。创建/移动上级选择、全文搜索和跨页快照仍为后续。

## 2026-11-11：自定义角色策略历史 0.22.0

新增 owner-only 规则前后快照和读取/UI，User 合同 60 项、Playground 可执行 59 项。migration 011 不改原迁移/数据，不推测回填；变更/audit/outbox/快照共事务，旧导出/SDK 兼容。记录仅自启用采集后捕获，未交付签名/WORM/长期全档、正式消费者或 scoped RBAC。见 role-policy-history.md / ADR 0024 / validation.md。

## 2026-11-12：策略历史程序接入 0.23.0

Go User SDK 增加 PolicyHistory、stdin CLI 增加 policy_history/role_id，复用 owner-only 历史 API。固定投影/规范规则/分页/取消与当前授权验证，管理/机器批次格式保持；没有新 API、迁移、UI 或部署变更。见 user-audit-client.md / ADR 0025 / validation.md。后续仍以 scoped RBAC、正式通知、受控遗失设备恢复和对账为待实施能力。

## 2026-11-12：组织上级选择 0.24.0

创建/移动组织复用现有 API，独立分页选择上级；移动确认绑定实际预览/目标/结构版本，处理取消、未知结果与实时授权变化。没有新迁移/权限/服务端或部署变更，60 项 User API 和 59 项 Playground 保持。见 organization-hierarchy.md / ADR 0026 / validation.md。scoped RBAC、正式通知、受控遗失设备恢复和对账仍为待实施能力。

## 2026-11-12：应用成员准入主线 0.25.0

自有应用 all_members/selected_members、显式成员、独立 User grant 版本和真实签发/refresh/当前资源检查，新增五项公共操作/控制台及独立本地 OAuth 应用。User/BFF 65 项、Playground 64 可执行；新增演示空间复用原账号和 TOTP，保留原两空间。此阶段实现应用准入，不代表组织子树/业务资源 scoped RBAC；正式通知、受控遗失设备恢复、周期对账和归档仍需后续。真实验收见 application-user-access.md / ADR 0027 / validation.md。

## 应用准入历史（0.26.0）

新增应用准入历史 API/控制台，复用既有 012 事务记录，User/BFF 66 项、Playground 65 项可执行；无新迁移或准入权威改变。完整 scoped RBAC、恢复、正式通知和归档仍需后续。 见 application-access-history.md / ADR 0028 / validation.md。
