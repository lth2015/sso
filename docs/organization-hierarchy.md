# 创建与移动组织：上级选择和移动预览

0.24.0 改进控制台组织编辑，复用已有三个接口，不增加公共 API/权限或迁移。User/BFF 仍 60 项，Playground 可执行 59 项；部署由 SRE 负责。本页描述控制台行为，原生 API 调用者仍按 OpenAPI 的 org ETag、当前权限及最近 MFA 契约操作。

| 现有公共 API | 用途 | 版本与授权 |
| --- | --- | --- |
| GET /v1/tenants/{tenant}/organizations?limit=25&after=… | 上级目录分页 | organizations.read，返回 org ETag |
| POST /v1/tenants/{tenant}/organizations | 创建，body 为 parent_id/name | organizations.write、最近 MFA、If-Match org ETag |
| POST /v1/tenants/{tenant}/organizations/{org}/move | body 为 parent_id/dry_run，预览或实际移动 | organizations.write、最近 MFA、If-Match org ETag；dry_run=false 成功后需重新授权 |

完整请求/响应/错误 schema 见 [公共 OpenAPI](../api/openapi.yaml)，程序调用和 [API Playground](playground.md) 继续使用原契约。

## 选择上级

在组织页选择「创建组织」或非根组织的「移动组织」。上级目录从首批重新读取，每批 25 项，显式「加载更多组织」；与主列表当前页和翻页位置独立，可选择主列表未展示的后页上级。初始选择为空，必须明确选择，创建名称按去除首尾空白后的 128 UTF-8 字节限制检查。

目录只接受固定组织字段、正确根、升序唯一 ID/推进游标与同一 org ETag；中途结构变化或异常清除选项。移动源行携带主列表结构 ETag，首次目录必须相同，避免旧源信息配新目录；若已变化，请关闭并刷新主列表后重新打开。自身及原上级不列为移动目标；下级是否形成循环、移动后最大深度 16，由服务端检查，不根据局部目录推算闭包。

## 预览与提交

选择新上级后点击「预览移动」，实际发送原 POST move 的 dry_run=true，使用本次目录 ETag。只有固定四字段、正确版本和有界子树数量/深度的成功响应才显示计划；随后勾选「我确认按当前预览移动组织及其下级」，才能确认移动。更换上级、加载更多、重新读取、错误或切换空间都清除旧预览/确认。

实际提交绑定同一 parent 和 org ETag，再次由主库核对当前 User/session/grant、organizations.write、最近 MFA、版本、循环与深度；预览不是授权凭证，也不锁定服务器结构。控制台要求预览不改变原生 API 的调用契约，API 没有新增预览票据。创建成功刷新目录；实际移动按原规则递增授权版本，需重新确认空间授权。

## 取消和未知结果

目录读取和无副作用预览期间可取消/关闭；AbortController、请求代数、当前 URL context 和卸载检查防止迟到响应进入重开的窗口或其他空间。提交使用同步标志防双发，写入中禁止关闭/Escape。异常或已提交丢包清除选择/计划，停止当前窗口继续提交；先关闭并刷新实际目录、检查审计再决定下一次操作，没有自动重试或撤销承诺。

读取需要 organizations.read，创建/预览/移动需要 organizations.write 与当前有效授权；已有普通组织操作员可使用，不要求 owner 或 members.read。401 沿用重新授权流程，角色变更后的旧 grant 不因已显示的选项保留权限。SDK、管理审计批次、机器 realm、001–011 和原演示数据保持。

## 本地验收

运行 `scripts/console organization-hierarchy-test` 和 `node --test tests/organization-hierarchy/model.test.mjs`。前者通过真实 React/BFF/API/MySQL/OAuth，覆盖超过 50 个组织的末页创建/两节点子树移动、预览失效、循环/过期版本、取消/换空间迟到、异常目录/预览、已提交丢包不重发、普通组织委托及实时撤销。55 个初始叶节点仅为受保护的新建合成空间中的 SQL 元数据，不宣称 55 次真实注册/创建 API 或负载测试；被测创建和移动均走真实 API。测试按精确注册账号集合核验/清理，拒绝演示或无关账户。

纯模型检查结构版本绑定、安全整数/四字段计划、创建提交版本与名称字节边界；真实套件与兼容回归结果见 [验证记录](validation.md) / [ADR 0026](adr/0026-organization-parent-selection.md)。没有全文搜索、跨页冻结快照、客户端闭包推算、幂等重试或新 API 能力。

界面截图仅含合成组织/身份元数据：[桌面](ui/organization-hierarchy-desktop.png) / [手机](ui/organization-hierarchy-mobile.png)。
