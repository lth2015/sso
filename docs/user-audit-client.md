# 外部 User 审计客户端与 CLI

0.19.0 已实现；实际验收见 [验证记录](validation.md)。公共 User API 仍为 59 个操作，批次格式仍为 sso.audit.batch.v1；新增 Go 类型化客户端与本地 stdin CLI，复用真实 User OAuth code+PKCE/Bearer。该库不负责登录、PKCE 回调或签发身份，不能以 M2M SK/机器 Bearer 替代 User 管理权限。

[Go SDK](../pkg/useraudit/README.md) 仅标准库，Management/Machine 两方法每次读取一个 audit-exports 批次。可信原点只接受 HTTPS（显式固定本地回环例外），限定 tenant/类型/数量/游标；无 Cookie、重定向、授权缓存、SDK 层自动重试或自动续页。响应固定字段/完整 JSON/namespace/kind、升序唯一 ID/最后游标、时间/版本、role snapshot 组及机器 action 验证后才返回，不接受额外 secret 字段、缓存或压缩/超大响应。错误体/传输异常不透出私密值，失效/拒绝/取消返回零批次。

[CLI](../examples/audit-batch/README.md) 从受限单份 stdin JSON 读取 raw access_token、tenant_id、kind 及可选 limit/after。固定当前本地入口，token 不经 argv/env/文件；stdout 只输出验证后的一个授权批次。输出含受控身份标识，接入方决定保存位置和保留策略；工具不暗中持久化输入或结果。

当前 API 始终在主库检查 User/session/grant/角色与 audit.read，scope 仍分读写；库不把先前成功作为后续资格。机器事件的 Machine 方法只选择审计来源，仍由 User 调用；业务机器 Bearer 验证使用独立 [machineidentity SDK](external-machine-integration.md)，用途不同。跨空间需要相应 token，连续批次不承诺冻结快照或归档签名。

本地 TLS/畸形/取消模型，以及真实 Hydra/User/审计委托/deny/机器 realm 的验收分别记录在 validation.md。完整文档包包括两类 SDK/示例公开源码，可在解压后的独立目录与空模块缓存、关闭模块下载下编译；没有私有 stdin 样本。
