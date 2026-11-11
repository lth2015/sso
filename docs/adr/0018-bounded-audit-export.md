# ADR 0018：按当前权限读取审计导出批次

状态：已接受并实施。真实主库、原生 Bearer 和浏览器下载验收通过；证据见 validation.md。

User GET 提供管理/机器两类受控 JSON 批次，每批 1–100 条。audit.read 与普通审计读取一致；显式拒绝、会话/grant/成员/租户版本在主库检查。游标必须属于本空间/同类型，返回单批主库读取时间与 authz_version。GET 不改变权限或写入新的管理动作。

导出版本 sso.audit.batch.v1 使用独立冻结 DTO，只投影受控 ID/action/time 与角色绑定快照；机器事件没有虚构人类 actor。模板不包含原始请求、秘密或目录邮箱。前端下载重新读取资格，校验 schema/namespace/字段与分页关系，取消/切换上下文/迟到响应不保存旧结果，文件名固定生成。

这是审计批次读取/下载，持续归档、签名证明和跨批次冻结快照另行建设。完整平台/BFF/OpenAPI/Playground/UI 同步，演示数据与现有稳定节点保留，不介入 CI/GCP/部署。实际主库、原生 Bearer、BFF/浏览器与故障注入证据记录在 validation.md。
