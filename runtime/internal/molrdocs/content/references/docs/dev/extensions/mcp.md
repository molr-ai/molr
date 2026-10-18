# MCP 接入

`runtime/internal/mcpclient` 使用官方 Go MCP SDK，作为可信工具适配器连接 HTTPS Streamable HTTP 服务。它与插件 HTTP 工具共同进入 `agent/tools` 的扩展入口，复用 operation admission、工具配额、回执、错误处理及取消链路。使用步骤见 [MCP 工具](https://molr.ai/zh/docs/admin/resources/mcp/)。

## 数据与权限

`mcp.connections` 属于共享资源目录，存储连接配置、工具快照和版本；`mcp.mutations` 保存配置写入的幂等摘要；`mcp.calls` 按真实调用者保存操作回执。连接映射为 `connection` 资源，每项工具映射为 `tool` 资源。工具目录元数据包含 `Source`（builtin / plugin / mcp），MCP 项另有 `ConnectionID` 和 `RemoteName`。

连接 ID 使用 `mcpconn_` 前缀。模型工具名为 `mcp_` 加连接 ID、远端名称、描述和 Schema 的摘要；相同定义保留标识，定义变更使旧资源失效。新工具及变更工具默认不启用。Schema 使用 JSON Schema 校验，拒绝引用、参数头映射及超限定义，不静默改变参数合同。

管理 API 验证管理员身份。目录按用户组的连接和工具范围过滤；执行还要求工具启用、连接可用、助理的该工具资源执行授权及当前配额。凭证由现有 vault 保存，Provider 为 `mcp:<connection-id>`；broker 校验连接、专用凭证与 origin。成员视图不包含端点、认证配置和凭证标识。

## 网络与生命周期

每次发现和执行使用独立 SDK client/session，避免共享账号状态。注入 `platform.Guard` 客户端以执行 DNS、私网和拨号检查；请求限定已登记的完整端点，不跟随重定向，不继承环境代理。认证只注入批准的请求头；SDK 日志不输出远端正文。目录发现拒绝包含当前凭证的内容，工具结果递归脱敏。

刷新是有时间、页数和数据量上限的读操作，与业务调用额度分开。一个连接同时只允许一次刷新；写入快照使用版本检查。工具执行前的连接额度预留和 operation admission 位于同一事务。连接总量与用户组额度通过 `quota.CallTotals` 聚合插件和 MCP 调用桶，SQLite 使用对应统计索引。

SDK 重连重试和自动交互请求处理关闭。完整业务错误响应保存为 `tool_error`，成功响应为 `succeeded`；发出调用后未取得可用结果为 `unknown`。相同 operation 成功回执直接复用，失败或未知结果不重新发请求。启动恢复将遗留 prepared/running 调用标为 unknown，保留调用计数并释放并发。

配置更新、停用和删除取消连接在途请求；账号、用户组及工具授权变更重新校验本地在途权限。调用返回前再次检查权限。远端取消与外部副作用不可由本地取消保证。

## HTTP API

| 路由 | 用途 |
| --- | --- |
| GET /api/mcp/connections | 可见连接与工具快照；管理员另见配置和用量 |
| POST /api/mcp/connections/save | 管理员保存；ID?, Name, Endpoint, Auth, Header, APIKey?, Verification?, Enabled, EnabledTools, CallLimit, Concurrent, TimeoutSeconds, Mutation |
| POST /api/mcp/connections/refresh | 管理员测试并刷新；ID, Mutation |
| POST /api/mcp/connections/remove | 管理员删除；ID, Mutation, Verification? |
| GET /api/mcp/connections/{id}/history | 管理员读取最近 100 项安全摘要，不加载参数或结果正文 |

Mutation 包含 IdempotencyKey；修改、刷新、删除另带 ExpectedVersion。凭证更新和删除沿用现有敏感操作验证。普通助理授权使用现有 `/api/grants`，用户组资源范围使用现有管理员接口。MCP 资源配置由连接接口维护，不能通过普通资源修改／断开接口绕过生命周期。

## 验证

`go test ./internal/mcpclient` 使用真实 TLS MCP 测试服务验证发现、Schema、执行、幂等、变更复核、配额、取消、超时和恢复。`MOLR_E2E_MCP=1 MOLR_E2E_SANDBOX_SERVICE=1 npm run test:webui -- mcp-tools.spec.ts tools-catalog.spec.ts` 验证界面、权限、成员隔离和 Agent 调用链。E2E 使用确定性模型 fixture；真实模型验收需另行进行。
