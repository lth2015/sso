# ADR 0020：受控协议登记的只读观察

状态：已接受并实施；真实主库、原生 Hydra/API 与浏览器专项通过，完整证据以 validation.md 为准。

将协议非秘密 profile 的观察与 reconcile/cleanup 写入分开。当前 owner 只读检查自有客户端/凭证，固定私有 Admin GET 前后读取主库资格与资源修订，等待远端时不持有租户锁；失权、修订变化或依赖故障不返回旧结果。只投影受控字段名、namespace/revision/state/time，禁止上游原文、实际值和秘密。

matching 仅表示受控字段一致，不改变 pending/active/revoked/expired，不证明 SK 或消费者可用，也不恢复未知秘密。读取不产生本地/协议写入或审计/outbox；显式写入继续用既有 MFA/ETag。强化单份/完整/有界 JSON 的 Admin 读取，保留未知敏感字段的丢弃投影。

API/BFF/OpenAPI/Playground 与控制台一致；资源/空间变化及取消清除旧观察。模拟 Admin 与真实 Hydra 的结果分开记录；不提供周期性补偿 worker或自动修复。见 protocol-inspection.md / validation.md。

观察正文的 resource_etag 表示本地登记资源的写入前置条件；观察响应不发送 ETag。观察时间/远端字段可变化，不能把资源版本当作整份观察表示的缓存校验值。所有响应继续 no-store。
