# ADR 0027：自有应用的 User 准入

状态：已接受并实现于 0.25.0；保留 7f59d43 / 0.24.0 与固定周五演示基线，实际验收见 [validation.md](../validation.md)。

在主线新增自有应用的人员准入：all_members（默认）或 selected_members，当前有效 owner 始终可准入，普通成员需要显式应用授权。准入只决定 User 是否能为该应用取得和继续使用授权，不授予六类平台管理权限、机器身份权限或任意业务资源权限，不宣称完成 scoped RBAC。共享/平台管理应用不能经此功能改写；默认策略与旧演示兼容。

012 仅新增四表：应用策略及独立版本、应用成员授权、User grant 绑定的准入版本、与 audit/outbox 共 event 的应用/成员/策略版本上下文。原 001–011/原表结构不改；旧 grant 缺少新绑定按版本 1 解释，默认无策略按 all_members/1。真正策略或成员授权变化递增独立版本，同一应用旧 User grant 失效；恢复准入不复活旧 grant。租户授权版本、应用启停版本和机器凭证版本保持各自语义，不借用机器所依赖的版本计数器。

主库在租户选项、同意提交、code/refresh Hook、GrantCurrent/BFF 和每次平台 API 权威事务检查策略/owner/显式成员与绑定版本。不得仅在 UI 过滤或依赖 JWT 自称准入。新授权将版本绑定与原 grant 同事务写入，当前租户行锁串行化 owner 写与同意/签发；读取不缓存资格。所有相关服务需采用同一新实现后才使用 selected_members，旧二进制不理解此边界，SRE 负责部署。

管理 API 仅当前 owner，读取允许旧 MFA，写强制最近 MFA、绑定 tenant/app 的专用 ETag；读取/写入/成员目标都检查自有应用和当前空间。grant 只接受有效 User/active membership，撤销允许清理已停用成员；owner 隐式准入不能通过撤销普通授权剥夺。无变化不增版本或记审计，实际变更/版本/审计/事务 outbox/上下文整体提交或回滚。未知写入不自动重发。

GET user-access 分页返回固定 policy/成员 ID，不包含邮箱；PUT 同路径修改 mode；GET/PUT/DELETE user-access/members/{member} 分别检查当前准入、显式授权和撤销。专用 ETag 与原应用启停 ETag 不混用；版本用正十进制字符串传输，避免浏览器整数精度问题。检查结果是当次观察，不是令牌/访问票据/缓存授权。普通 audit.read 仍使用原冻结投影，只见动作和应用目标；新增事务上下文不扩入旧导出。

控制台从自有应用进入，选择成员、检查资格，再明确确认模式或单成员授权变更。成员目录需要原 members.read；没有该权限的 owner 可输入 membership ID 并通过专用检查核验，不泄露邮箱。独立分页、严格投影、context/取消/迟到隔离、同步防双发、失败清选择/旧检查结果；User 准入变更不触碰机器凭证。

真实 MySQL/回滚/并发、真实 User code+PKCE/令牌/refresh/当前撤销与机器/其他应用隔离、真实 UI/普通成员拒绝、OpenAPI/Playground/完整演示分别验收。演示准备独立合成应用与成员，不改原演示数据/密码/TOTP/恢复码。无正式邮件、外部业务集成、自动轮换/对账或生产 SLA 承诺；CI/GCP/部署由 SRE 负责。
