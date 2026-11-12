# 策略历史 Go SDK：实施约束

基于 0.22.0 / a35845e，复用已有 owner-only 历史读取，新增标准库 Go 方法与本地 stdin CLI kind。当前尚未实现，不增加 API 或授权，服务端/迁移/UI/CI/GCP 保持；完整通过后发布下一客户端节点。

- useraudit.Client.PolicyHistory 使用真实 User Bearer 与预期 tenant/role、limit/after，一次在线读一页。调用方必须当前 owner，audit.read 委托不能替代。调用方不提供任意路径/授权头；HTTPS 可信原点及固定显式本地例外复用原约束。
- 严格固定五字段 envelope、八字段事件、两字段 policy；namespace/格式/动作/时间/int64 版本、规范化六种 permission/allow/deny/最多12条/无重复/顺序、创建前空/删除后空/更新实际不同、ID/游标有界递进全部校验。未知字段/私密值/重复键/额外 JSON/空数组类型异常拒绝，不返回部分页。
- 抽取原 HTTP 受限读取复用，保留无 Cookie/重定向/成功授权缓存、最多五秒/256 KiB/no-store/Age/压缩/UTF-8 与固定错误类别；每次独立主库授权，无自动续页/重试。上下文在读取/解析/输出前复核，失败/取消零结果。
- CLI 新 kind=policy_history，必须另有 role_id；旧 management/machine kind 不接受 role_id，既有输入/输出保持。输入仍单份受限 JSON stdin，不读 argv/env token，不写输入文件；stdout 是一个已授权批次，后续保留由接入方负责。
- 测试区分 TLS/传输/严格 JSON 模型与真实 OAuth/User/API/独立 Go CLI，覆盖 owner/委托/撤销、删除后/分页/scope/tenant/机器 realm/旧 MFA、取消与私密排除。旧两类批次及 CLI 兼容回归，真正 ZIP 解压后空缓存/禁止模块下载编译。

不提供登录、签名存证、长期归档、跨页快照、API 写入或权限/令牌缓存，不宣称包装全部 60 项 API。保留 0.22/周五基线与演示凭证，部署仍由 SRE 负责。
