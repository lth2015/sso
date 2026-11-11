# ADR 0021：User 审计客户端一次读取一个批次

状态：已接受，0.19.0。

提供标准库 Go 客户端和固定本地 CLI，使用调用者已有真实 User Bearer，不新增授权、登录或 API 操作。管理/机器审计使用独立冻结事件类型，固定 namespace/kind/字段/大小/ID 与游标语义校验；每次在线读取主库，没有授权缓存、续页或 SDK 层自动重试。

配置原点来自可信代码，HTTPS 默认；明确的本地例外固定入口/拨号/Host。禁止 Cookie、重定向、原始错误/输入回显，受控 deadline/body/caching/media。CLI 私有 JSON 经 stdin，结果仅一个已授权批次，失败/取消无部分正常输出；不读环境或参数令牌，不写秘密文件。

模拟传输/JSON 边界与真实 OAuth/Bearer/主库委托/撤销/机器 realm 分开验收。版本化批次不是完整归档，SDK 不负责后续数据保存或业务 ACL。见 user-audit-client.md / validation.md。
