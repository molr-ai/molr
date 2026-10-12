# Runtime HTTP 接口

Go 单体 server 使用显式注册的 `/api` 路由。这里不开放本机初始化/恢复、助理发布消息、检查点、工具执行、预算预留/结算和操作回执写入。

## 公共协议边界

客户端当前通过 HTTP/JSON 与 SSE 连接 Runtime；路由实现在 [`runtime/internal/api`](../../runtime/internal/api)，TypeScript DTO、客户端和续传逻辑在 [`@molr/sdk`](../../packages/sdk/README.md)。仓库没有独立的 OpenAPI／JSON Schema 分发包。HTTP 路径使用 `/api`，目前没有独立的客户端协议版本协商；对象的 `Version` / `ExpectedVersion` 用于并发修改检查，不代表协议版本。当前产品客户端是 WebUI；桌面与手机浏览器可连接同一 Runtime，独立 Desktop／Mobile／CLI 客户端不作为已发布能力。

公共接口只开放显式注册的业务能力，不等于全部 Go 模块接口。任务使用当前 `Task` 等领域对象；早期 `Work` 和虚拟 OS 提案不属于当前公共接口，不能据此构造请求。客户端 DTO、内部持久化记录和 Agent 检查点是不同边界，修改其中一种格式时需分别核对客户端兼容性与数据迁移，不能把内部格式当作稳定的网络合同。

认证层从有效会话或访问令牌确定主体，客户端和 Agent 自报身份不构成权限。除明确公开的登录入口外，业务 API、SSE、搜索、附件与产物下载均需认证和对象权限检查；游标与对象 ID 不能代替授权。当前支持多账号：私有业务数据按账号隔离，管理资源目录等操作需管理员，资源执行还受用户组范围、成员额度与助理授权共同约束，详见[多用户与成员配额](../security/users-and-groups.md)。浏览器、令牌、一次性登录链接和敏感操作验证规则见下文；不要将额外身份确认理解为每项管理操作都无条件要求的统一流程。

命令接收、操作执行结果与用户可见消息发布是不同阶段。请求幂等不保证所有外部副作用恰好发生一次，未知结果需要核对；内部调度事件不直接成为聊天消息，助理通过会话工具发布且不能伪装用户。内部队列、工具与恢复机制见 [Runtime 架构](../architecture/overview.md)。Agent 工具不开放账号、凭证或访问令牌管理，模型可见内容不得包含真实凭证、认证头、Cookie 或内部凭证引用，详见[安全边界](../security/security-model.md)。

Runtime 仍是 Go 单体，不加载 Go 动态业务模块；已实现的 [Plugin API 3](../extensions/plugins.md) 支持隔离页面、主题、个人配置、服务连接、声明式 HTTP 工具。插件协议与 [UI/Host 契约](../frontend/themes.md) 各自维护，不能由此推导 HTTP API 已有同样的版本协商或任意后端模块加载能力。

## 请求与传输约定

- JSON 请求拒绝未知字段和尾随内容，并限制大小与处理时间。领域 DTO 对应 Go 公开请求类型的字段名（请求支持 Go JSON 的不区分大小写匹配）；认证、凭证与敏感设置操作使用独立 DTO，下划线字段如 `new_password`、`mfa_code`、`expires_at`、`include_messages`、`object_ids`。秘密值必须是字符串，不接受私有 `Secret` 对象。
- 业务写入 DTO 中的 `Mutation` 携带 `IdempotencyKey`；修改已有对象还需 `ExpectedVersion`。会话已读、认证撤销等接口按各自合同处理。
- 列表支持 `cursor`、`limit` 及模块查询参数；时间筛选使用 RFC3339。
- `POST /api/assistants` 的 `ModelID` 同时表达对所选模型的使用授权：创建助理、会话、该模型的 `read` / `execute` Grant（`Scope.ModelID` 指定模型，`ResourceID` 绑定所属 Provider，调用地址跟随其当前配置）和 Agent Loop 在同一事务提交。失败全部回滚，重复请求不重复授权；撤权后重试原聘请请求或重命名不会恢复权限。其他资源仍需显式授权，克隆与返聘仍只恢复用户选定的权限。
- 模型类 `POST /api/grants` 必须提供有效的 `Scope.ModelID`，且模型属于 `ResourceID` 指定的 Provider；非模型资源不能携带模型范围。不接受未指定模型的 Provider 整体授权。模型目录与推理入口逐模型检查读／执行权限，同一 Provider 的其他模型不会自动获得授权。克隆保留模型范围，撤销只影响对应授权。
- 资源返回的 `Actions` 是当前 Provider 支持的授权子项。模型的 `read` / `execute` 分别为「查看模型／调用模型」，本地存储的 `read` / `write` 为「读取文件／写入文件」。`POST /api/grants` 拒绝资源不支持的动作；助理执行时也重新检查当前能力，旧授权不能越过能力限制。前端按资源类型显示名称，只提供该资源实际声明的选项；切换资源清空不适用的动作与范围。
- WebUI 通过 `POST /api/resources/provider` 原子创建/更新 Provider：传入 `Mutation`、可选 `ID`、`Name`、`Endpoint`、`UseAPIKey`，需要保存或更换 Key 时另传 `APIKey`；敏感操作验证开启时，需用途为 `credential.put` 的 `verification`，关闭时可为空。编辑时 Key 留空表示保留已配置的 Key；`UseAPIKey: false` 取消该连接的认证。新 Key 与资源绑定、版本及审计在同一事务提交，失败一起回滚。`GET /api/resources/{id}/connection` 仅供管理员读取可编辑元数据，不返回密钥。
- `POST /api/resources/delete-provider` 接收 `ID`、`Mutation` 和可选 `verification`，仅管理员可删除模型类 Provider，已断开连接同样支持。敏感操作验证开启时要求用途为 `credential.delete` 的证明。删除连接、绑定／模型目录、未被其他连接引用的凭证，撤销资源授权并清除失效的默认模型，均在同一事务提交；失败回滚，重复请求幂等。助理、聊天、任务和审计历史保留，显式使用已删除模型的助理需重新选择模型，不会自动切换到其他提供商。
- 模型连接以 provider 为资源；私网访问由部署静态配置 `network.allowed_private_ips` 控制，资源 API 不接受网络放行参数。`POST /api/resources/test-connection` 接收 `Provider`、`Binding` 与可选 `APIKey`，返回 `ModelCount`；仅管理员可用，临时 API Key 与已保存 `CredentialID` 二选一，不保存配置、不上传聊天内容，使用部署网络策略。`Binding.Models` 可为空；可选手动模型包含名称、上下文、工具能力和价格，`Pricing: null` 表示未配置价格；显式提供 `Pricing` 三项单价且全零才表示确认免费。`POST /api/resources/refresh-models` 接收 `ID` 与 `Mutation`，在事务外发现模型，再按版本保存结果。`GET /api/models` 返回独立模型 ID、所属 `ResourceID` 和 `Pricing`；模型授权同时绑定资源与 `Scope.ModelID`，未知价格不允许推理。`POST /api/resources/configure-model` 接收 `ID`、`Model` 和 `Mutation`，配置单个模型的上下文、工具能力与可选价格，刷新保留手动配置。发现失败保留已有列表，重复刷新不改变模型 ID，断开 provider 后模型退出可用列表。
- `GET /api/status` 需要认证，返回 `name`（Molr Runtime）、`status`（running）和配置中的 `public_url`，供连接设置展示实际 Runtime 地址。
- 消息列表默认升序；`order=desc` 从最新消息开始，继续使用返回的 `NextCursor` 加载更早消息。游标绑定方向，不能混用。消息包含当前持久化的 `Feedback`（`like` / `dislike` / `none`）。
- `http.requests_per_minute` 控制每个可信来源地址的通用请求限额，默认 600；登录/MFA 完成请求独立使用 `auth.login_attempts_per_minute`（默认 5）。两者都必须为正。
- SSE：`GET /api/events/stream`，事件的 `id` 是绑定所有者及 topic 的不透明游标，可通过 `Last-Event-ID` 重连。服务持续核验会话，撤销或过期会关闭连接。`GET /api/events` 用于有限分页补取。两个入口的 `topic` 参数最多 16 个，要求非空、不重复且每个不超过 128 字节；游标绑定账号和规范化后的主题集合。实时交付由事务提交通知唤醒，断线按游标补读；网络中断边界可能重复投递，客户端应按事件 ID／游标幂等处理。
- 上传：`POST /api/files` 使用原始 body、`Content-Length`、`Content-Type`、`X-File-Name`、`Idempotency-Key`，可选 `X-Task-ID`。非 ASCII 文件名可将 `X-File-Name` 百分号编码并设置 `X-File-Name-Encoding: percent`；旧的未编码头保持兼容。下载仅通过鉴权后的 `/api/files/{id}/content`，使用 attachment、application/octet-stream 与 sandbox CSP，不暴露磁盘路径。
- 本机开发可显式配置 `tls_mode: "local"`：仅数字回环监听，HTTP 回环 URL，不接受代理转发头。开发会话/预登录 Cookie 为 `molr_dev_session` / `molr_dev_login`，保留 HttpOnly/SameSite=Strict，省略 Secure；独立 proof Cookie 为可读的 `molr_dev_csrf`；认证与 CSRF 流程相同。
- HTTPS 直接终止或显式可信代理。Host 必须匹配 PublicURL；代理模式只信任配置网段，验证代理 HTTPS 头并从右向左剥离可信代理地址。跨域来源必须显式配置，不使用通配符。

## 路由清单

2026-10-03 按 `runtime/internal/api/*_routes.go` 的注册项核对，含沙盒动作路由。新增或调整路由时同步本表；路径存在不表示当前身份有权访问，部署能力开关仍然生效。认证和通用字段见本文，专项行为见[插件](../extensions/plugins.md#生命周期与-http-路由)、[做梦](../runtime/dream-memory.md#界面与验证)、[工作台](../runtime/workbench.md#runtime-实现)、[成员配额](../security/users-and-groups.md)及[资源额度](../security/resource-quotas.md)。

| 方法 | 路径 |
| --- | --- |
| DELETE | `/api/auth/login-links/{id}` |
| DELETE | `/api/auth/sessions/{id}` |
| DELETE | `/api/auth/tokens/{id}` |
| DELETE | `/api/files/{id}` |
| GET | `/api/account` |
| GET | `/api/account/quotas` |
| GET | `/api/admin/catalog` |
| GET | `/api/admin/groups` |
| GET | `/api/admin/usage` |
| GET | `/api/admin/users` |
| GET | `/api/assistants` |
| GET | `/api/assistants/activity` |
| GET | `/api/assistants/groups` |
| GET | `/api/assistants/{id}` |
| GET | `/api/assistants/{id}/capabilities` |
| GET | `/api/assistants/{id}/shell-tasks` |
| GET | `/api/assistants/{id}/status` |
| GET | `/api/audit` |
| GET | `/api/auth/csrf` |
| GET | `/api/auth/login-links` |
| GET | `/api/auth/me` |
| GET | `/api/auth/security` |
| GET | `/api/auth/sessions` |
| GET | `/api/auth/tokens` |
| GET | `/api/conversations` |
| GET | `/api/conversations/{id}` |
| GET | `/api/conversations/{id}/messages` |
| GET | `/api/conversations/{id}/messages/{message}/metrics` |
| GET | `/api/credentials` |
| GET | `/api/credentials/{id}` |
| GET | `/api/debug/assistants/{id}/checkpoint` |
| GET | `/api/debug/runs` |
| GET | `/api/debug/runs/{id}` |
| GET | `/api/debug/runs/{id}/steps/{step}` |
| GET | `/api/debug/stream` |
| GET | `/api/debug/trajectory` |
| GET | `/api/dreams` |
| GET | `/api/dreams/config` |
| GET | `/api/dreams/conflicts` |
| GET | `/api/dreams/runs/{id}` |
| GET | `/api/dreams/windows` |
| GET | `/api/events` |
| GET | `/api/events/stream` |
| GET | `/api/files/{id}` |
| GET | `/api/files/{id}/content` |
| GET | `/api/grants` |
| GET | `/api/jobs` |
| GET | `/api/memories` |
| GET | `/api/memories/{id}` |
| GET | `/api/models` |
| GET | `/api/operations` |
| GET | `/api/operations/{id}` |
| GET | `/api/permission-requests` |
| GET | `/api/permission-requests/{id}` |
| GET | `/api/plans` |
| GET | `/api/plans/events` |
| GET | `/api/plugins` |
| GET | `/api/resource-usage` |
| GET | `/api/resources` |
| GET | `/api/resources/{id}` |
| GET | `/api/resources/{id}/connection` |
| GET | `/api/sandbox` |
| GET | `/api/sandboxes` |
| GET | `/api/sandboxes/{id}/resources` |
| GET | `/api/schedules` |
| GET | `/api/schedules/{id}` |
| GET | `/api/search` |
| GET | `/api/search/rebuild/{id}` |
| GET | `/api/settings` |
| GET | `/api/settings/currency` |
| GET | `/api/settings/exports/{id}` |
| GET | `/api/status` |
| GET | `/api/tasks` |
| GET | `/api/tasks/{id}` |
| GET | `/api/tasks/{id}/revisions` |
| GET | `/api/todos` |
| GET | `/api/todos/{id}` |
| GET | `/api/tools` |
| GET | `/api/usage` |
| POST | `/api/account/profile` |
| POST | `/api/admin/groups` |
| POST | `/api/admin/groups/delete` |
| POST | `/api/admin/users` |
| POST | `/api/admin/users/update` |
| POST | `/api/assistants` |
| POST | `/api/assistants/clone` |
| POST | `/api/assistants/dismiss` |
| POST | `/api/assistants/groups` |
| POST | `/api/assistants/groups/delete` |
| POST | `/api/assistants/groups/update` |
| POST | `/api/assistants/place` |
| POST | `/api/assistants/rehire` |
| POST | `/api/assistants/update` |
| POST | `/api/assistants/{id}/compress` |
| POST | `/api/assistants/{id}/expedite` |
| POST | `/api/assistants/{id}/shell-tasks` |
| POST | `/api/assistants/{id}/stop` |
| POST | `/api/auth/login` |
| POST | `/api/auth/sessions/revoke-others` |
| POST | `/api/auth/login-links` |
| POST | `/api/auth/login-links/preview` |
| POST | `/api/auth/login-links/redeem` |
| POST | `/api/auth/logout` |
| POST | `/api/auth/mfa/complete` |
| POST | `/api/auth/mfa/confirm` |
| POST | `/api/auth/mfa/disable` |
| POST | `/api/auth/mfa/enroll` |
| POST | `/api/auth/password` |
| POST | `/api/auth/reauthenticate` |
| POST | `/api/auth/security` |
| POST | `/api/auth/tokens` |
| POST | `/api/conversations/feedback` |
| POST | `/api/conversations/messages` |
| POST | `/api/conversations/read` |
| POST | `/api/credentials` |
| POST | `/api/credentials/delete` |
| POST | `/api/debug/assistants/{id}/context/reset` |
| POST | `/api/dreams/config` |
| POST | `/api/dreams/run` |
| POST | `/api/dreams/undo` |
| POST | `/api/files` |
| POST | `/api/grants` |
| POST | `/api/grants/revoke` |
| POST | `/api/grants/update` |
| POST | `/api/jobs/{id}/cancel` |
| POST | `/api/memories` |
| POST | `/api/memories/delete` |
| POST | `/api/memories/update` |
| POST | `/api/operations/reconcile` |
| POST | `/api/operations/{id}/cancel` |
| POST | `/api/permission-requests/resolve` |
| POST | `/api/plans/configure` |
| POST | `/api/plans/execute` |
| POST | `/api/plugins/configure` |
| POST | `/api/plugins/preference` |
| POST | `/api/plugins/install` |
| POST | `/api/plugins/remove` |
| POST | `/api/plugins/ui` |
| POST | `/api/plugins/settings/read` |
| POST | `/api/plugins/settings/save` |
| POST | `/api/plugins/connections/list` |
| POST | `/api/plugins/connections/save` |
| POST | `/api/plugins/connections/remove` |
| POST | `/api/plugins/connections/test` |
| POST | `/api/plugins/connections/history` |
| POST | `/api/plugins/connections/acknowledge` |
| POST | `/api/resources` |
| POST | `/api/resources/configure-model` |
| POST | `/api/resources/delete-provider` |
| POST | `/api/resources/disconnect` |
| POST | `/api/resources/provider` |
| POST | `/api/resources/refresh-models` |
| POST | `/api/resources/test-connection` |
| POST | `/api/resources/update` |
| POST | `/api/resources/{id}/check` |
| POST | `/api/sandboxes/connect` |
| POST | `/api/sandboxes/disconnect` |
| POST | `/api/sandboxes/rename` |
| POST | `/api/sandboxes/resource` |
| POST | `/api/sandboxes/test` |
| POST | `/api/schedules` |
| POST | `/api/schedules/delete` |
| POST | `/api/schedules/update` |
| POST | `/api/search` |
| POST | `/api/search/rebuild` |
| POST | `/api/settings` |
| POST | `/api/settings/cleanup` |
| POST | `/api/settings/export` |
| POST | `/api/tasks` |
| POST | `/api/tasks/cancel` |
| POST | `/api/tasks/pause` |
| POST | `/api/tasks/resume` |
| POST | `/api/tasks/revise` |
| POST | `/api/todos` |
| POST | `/api/todos/delete` |
| POST | `/api/todos/update` |

HTTP 限流在事务内读取并更新持久计数，通过的请求占用当前窗口额度；额度已耗尽时返回 429 和 `Retry-After`，不重写未变化的计数，也不延长窗口。并发请求共享事务计数，重启保留窗口；存储失败时拒绝放行。

## 认证与浏览器流程

1. `GET /api/auth/csrf` 获取 `csrf_token`，server 同时设置独立的预登录 Cookie。
2. `POST /api/auth/login` 发送 JSON `username`、`password`，携带 Cookie、`Origin` 与 `X-CSRF-Token`。启用 MFA 时返回 `challenge`，再调用 `/api/auth/mfa/complete`。
3. HTTPS 登录成功设置 `__Host-molr_session`：Secure、HttpOnly、Path=/、SameSite=Strict。JSON 返回会话元数据与新的独立 `csrf_token`，不会返回浏览器会话令牌。同时设置可读的 `__Host-molr_csrf`（Secure、Path=/、SameSite=Strict），用于刷新或新标签页恢复请求头；它与会话凭证不同，校验仍绑定服务端保存的会话摘要。退出时一并清除。
局域网 HTTP（`tls_mode: lan`）使用 `molr_lan_session_p<端口>`、`molr_lan_csrf_p<端口>`、`molr_lan_login_p<端口>`，不设置 Secure，仍保留 HttpOnly 会话、SameSite、来源与 CSRF 校验；本机 local 模式保留开发 Cookie 名称。

4. Cookie 认证的修改请求检查 CSRF 与来源。原生客户端可使用 `Authorization: Bearer …`，禁止在 URL 中携带凭证；同时提供 Cookie 和 Bearer 会被拒绝。
5. `GET /api/auth/security` 向当前所有者会话返回 `MFAEnabled`、`ReauthenticationEnabled`、`UnprotectedPurposes` 与 `Version`。敏感操作验证默认关闭，`credential.put`、`credential.delete` 和 `export` 无需重复密码，仍要求有效所有者会话及 CSRF。开启后前后端统一要求用途绑定的验证证明，启用 MFA 时另需验证码。`POST /api/auth/security` 接收 `enabled`、`expected_version` 和用途为 `security.update` 的 `verification`。密码修改、MFA 管理、令牌创建及清理数据始终需要验证。需要验证时调用 `/api/auth/reauthenticate`，取得单次、短期、绑定会话与用途的 `verification`。令牌仅在创建成功时返回一次明文 `value`。

本文的“所有者”指当前账号对其私人对象的归属，不等于最初初始化账号或管理员角色；共享资源配置另需管理员。身份由 HTTP 认证注入，业务请求不能提供 Principal、Owner 身份或执行身份。每个业务模块继续核验当前权限。错误返回固定 `code`、`message`、`request_id`，以及可选的 `reason`。公开原因码只允许已审查的 `network_private_address_denied`、`network_http_not_allowed` 和 `network_destination_prohibited`；客户端映射为本地化说明，未知原因回退到 `code` 提示，不公开底层异常或字段。

验证器绑定先通过用途为 `mfa.enroll` 的身份确认，得到短期、绑定当前会话的 enrollment 和设置密钥。`POST /api/auth/mfa/confirm` 只需 `enrollment_id` 和 `code`；完成绑定后返回一次性恢复码，无需再次输入密码。

### 设备会话

- `POST /api/auth/login` 和 `/api/auth/login-links/redeem` 接受 `device_id`、`client_type`、`device_name`。device_id 为 32–128 位 URL-safe 随机字符串，客户端按实例与服务地址保存；省略时创建独立临时会话。client_type 支持 `web`、`ios`、`android`、`desktop`、`cli`，只用于展示。device_name 最长 128 字节，不接受控制字符。
- 同一账号与 device_id 重复认证，在事务中替换原会话，返回新的 SessionID、Cookie 和 CSRF；保留 CreatedAt，更新 LastLoginAt、LastSeenAt 和 ExpiresAt。未提供 device_id 的 CLI 会话独立创建并在结束时注销。
- `POST /api/auth/mfa/complete` 必须携带开始登录时的 device_id；设备名称与类型采用 challenge 绑定的值。较旧 challenge 完成时若设备已有新会话，返回 409，需重新开始登录。
- `GET /api/auth/sessions` 仅返回当前有效会话，包含 ClientType、DeviceName、LastLoginAt、LastSeenAt、ExpiresAt 和 Current；按最近活动倒序分页。WebUI 将当前设备置顶。`POST /api/auth/sessions/revoke-others` 撤销当前账号除请求会话外的所有有效会话，要求登录会话与 CSRF，并记录审计。

### 登录链接

- `POST /api/auth/login-links`：已登录所有者提供 `name`、`target_path`、`expires_in`（60–86400 秒）和用途为 `login_link.create` 的身份验证证明。默认 UI 为 1 小时，另外提供 15 分钟、24 小时。返回 `link` 元数据和仅本次可见的 `value`；数据库只保存 Token 摘要。
- `GET /api/auth/login-links` 返回分页元数据，包括 `UsedAt`、`RevokedAt`、`ExpiresAt` 和 `PendingMFAUntil`（仅等待 MFA 时有值）；不返回凭证。`DELETE /api/auth/login-links/{id}` 撤销链接，也使其待完成的 MFA challenge 失效，不影响已签发的会话。
- WebUI 使用 `#/login?token=…`，读取后立即从地址栏清除，Token 不写入 localStorage/sessionStorage，不放进 HTTP URL。页面取得预登录 CSRF 后，POST 到 `/api/auth/login-links/preview`，请求体只含 `token`。预览只返回真实 `expires_at`，不消费链接、不签发会话；无效、过期、已使用或已撤销的链接返回 401。该接口与登录接口使用相同的 Origin、预登录 CSRF、限流和 no-store 保护。确认页显示一次性使用说明与到期时间；只有用户点击确认后才重新取得预登录 CSRF，并 POST 到 `/api/auth/login-links/redeem`，提交 `token`、`device_id`、`client_type` 和 `device_name`。兑换时重新验证状态，防止预览后被撤销或过期的链接登录。
- 兑换复用登录的 Origin、CSRF、限流和 HttpOnly Cookie 规则。服务器将 Token 消费与签发会话/MFA challenge 放在同一事务中；并发兑换只成功一次。启用 MFA 时仍调用现有 `/api/auth/mfa/complete`，错误验证码不会签发会话。
- 目标由创建时服务器保存，限定为已知站内路由；不接受 URL 中的跳转覆盖。链接有效期与会话有效期独立。修改密码、本机账号恢复、备份恢复均使旧链接失效。
- 链接无效、已使用、已撤销或过期统一返回 `unauthenticated`，页面提示获取新链接或使用密码登录。请求响应禁止缓存，审计记录仅含链接 ID，不含原始 Token。

## 账号及用户组管理

`GET /api/auth/me` 保留 Principal 字段并增加 `User`（显示名称、Avatar、组、Administrator、Disabled、Version）。用户只能读取自己的身份。

| 路由 | 权限与用途 |
| --- | --- |
| `GET /api/account` | 当前账号资料 |
| `POST /api/account/profile` | 更新自己的 DisplayName 与 Avatar，Mutation.ExpectedVersion 乐观锁；Avatar 省略/null 保留，空字符串清除，支持 `preset:<id>` 或不超过 128 KiB、尺寸不超过 256×256 的 PNG/JPEG data URL。图片解码后重编码以去除元数据，拒绝外部 URL 和 SVG |
| `GET /api/account/quotas` | 自己的组配置、累计 `Summary`、各资源当前额度占用 `Resources` 和 `AsOf` |
| `GET /api/admin/users` | 管理员分页列出账号元信息 |
| `POST /api/admin/users` | 管理员创建用户，Username / DisplayName / Password / GroupID / Mutation |
| `POST /api/admin/users/update` | 管理员修改组、名称或停用状态，ID / DisplayName / GroupID / Disabled / Mutation |
| `GET /api/admin/groups` | 管理员列出用户组 |
| `POST /api/admin/groups` | 管理员保存 Group 与 Mutation；Resources 仅接受现有 model/tool/sandbox 引用及配额 |
| `POST /api/admin/groups/delete` | 管理员删除空的自定义组，ID 与 Mutation.ExpectedVersion |
| `GET /api/admin/catalog` | 管理员读取可引用目录和沙盒配额支持情况 |
| `GET /api/admin/usage` | 管理员读取累计统计，不能读取其他用户的详细业务数据 |

身份与计量边界见[多用户与成员配额](../security/users-and-groups.md)。

### 用量统计响应

`GET /api/account/quotas` 的 `Summary` 与 `GET /api/admin/usage` 返回的 `Users` 中对应成员使用同一累计统计：`ModelCalls`、`ToolCalls`、`ConnectionCalls`、`Spent`、`Reserved`、`AttachmentBytes`、`Active`、`Unknown`。金额按原币种返回；附件只统计 Ready 文件。`Active` / `Unknown` 为模型和工具操作的占用／未知数，HTTP 工具已包含在工具操作中，不重复相加。管理员接口仅输出统计和账号标识，不返回调用参数、回执正文或私人业务内容。

上下文用量：`GET /api/account/quotas` 的 `ContextStorage` 只统计当前账号引用可达的对象，账号内去重；`GET /api/admin/usage` 返回 `{Users, ContextStorage}`，其中 `ContextStorage` 统计跨账号去重后的全部已存对象（含待回收对象），与成员筛选无关。两者字段为 `Bytes`（对象 JSON 正文字节数，含消息、共享定义及调试详情，不含索引、引用元数据和数据库空闲空间）和 `Messages`（唯一消息节点数）。统计在同一读取事务中完成；共享对象分别计入引用它的各账号，账号数值不能相加代替全局数值。接口只返回汇总数字，不返回消息、节点 ID 或其他账号的引用。

个人 `Resources` 包含 `Kind`、`ID`、`Name`、`Calls`、`Active`、`Unknown`、`Carried`、`Spent`、`Reserved`、`PeriodStart`、`PeriodEnd`。模型／工具／连接的统计与准入一致，当前周期以外仍在执行的调用继续计入占用，并以 `Carried` 单列。`Unknown` 表示本地调用已结束但结果或费用未知，不计入 `Active` / `Reserved`，调用次数仅计入原周期；它不是 `Active` 的子集。`PeriodEnd=null` 表示累计周期；沙盒实际用量仍由 `/api/resource-usage` 获取，不将其配置限额当作用量。

## 设置

### 界面主题

界面主题：`Settings.UITheme` 保存 `id@version`（例如 `molr.test@1.0.0`），与控制深浅模式的 `Settings.Theme` 分离。默认 `molr.official@1.0.0`，按 Owner 保存并使用设置版本检查；旧客户端省略或传空字段时保留已有主题。Runtime 仅校验标识格式，不加载 UI 代码；可用主题由客户端内置目录和 Runtime 已启用、已授权的插件主题共同决定；插件接口见 [插件机制](../extensions/plugins.md#生命周期与-http-路由)。

### 币种与汇率

币种与汇率：`Settings.Currency` 支持 `USD` / `CNY`，用于新金额输入的默认币种和展示币种。预算 `Budget.Limit.Currency` 与手动模型价格 `Model.Pricing.Currency` 独立保存输入时选择的原币种，编辑时沿用原币种。改变显示偏好不会重写预算、价格、预留或结算。配置手动模型价格时必须显式传币种，三项 token 价格共用该币种。

`GET /api/settings/currency` 返回当前 `Currency`、`Timezone` 与 `Rates`（`version`、`as_of`、`source_url`、`units`）。`units` 是同一基准下的整数比例，用 `amount * units[target] / units[source]` 换算，保留六位小数。服务端预算检查向上取整；只支持明确配置的币种，拒绝缺失汇率与溢出。

`Usage.Spent` / `Reserved` 汇总为当前展示币种，同时通过 `OriginalSpent` / `OriginalReserved` 返回按原币种分组的金额，`Exchange` 标注此次汇总的汇率快照。WebUI 将原币种小计换算汇总为用户偏好币种，只显示合计，不并列原币金额；固定两位小数，不足一分向上取整，换算金额带 `≈`。偏好或汇率不可用时显示 `—`。这只影响展示，金额输入和后端记账保留原始精度。预算比较采用同一次汇率快照，预留记录中的 `Reserved` 和结算 `Settled` 始终保留原币种；`BudgetEstimate` 与 `Exchange` 保存预算校验时的换算结果和版本。后续规则更新不修改这些凭据。

### 插件显示

插件显示：`Settings.PluginsVisible` 为账号级布尔偏好，默认 `false`，通过现有 `GET /api/settings` 和设置更新接口读写。仅控制 WebUI 展示，不改变插件安装、启停、授权或工具执行。

## 模型与首次配置

### 首次使用与本机 Provider 发现

- `GET /api/setup`：管理员读取 `{ Required, AssistantID }`。只有本机 `bootstrap` 初始化的账号有待完成状态，普通 `init` 和已有账号不被强制引导。
- `POST /api/setup`：已登录管理员提交 `ModelID`、`Language`（zh/en）、`Timezone`、`Budget`（AssistantBudget）和可选 `Pricing`（ModelPricing）。未知价格必须明确提交，0 表示免费。设置偏好、配置价格、创建默认 Molr 助理和完成状态在同一数据库事务中提交；重复请求返回原 AssistantID，助理删除后不重新进入引导。复用 Hire 的 Born 事件，首次问候走正常模型预算链路。
- `GET /api/resources/discover-local`：管理员触发有限本机探测，返回 `Name`、`Preset`、`Endpoint`、`State`、`Models`、`ResourceID` 数组。State 为 ready、empty、authentication_required；ResourceID 非空表示已有相同地址的连接。结果不自动保存，不自动授权；选择后沿用 provider/refresh-models 接口。

探测候选固定为回环 11434、8080、8000、30000；有限并发、短超时，拒绝重定向，并沿用部署的实际连接地址限制。仅查询模型元信息，不发送推理请求。通用模型列表的工具能力配置仍是可覆盖的兼容默认值，不能作为执行已通过的证明。

内嵌 WebUI 的静态资源无需登录，但业务 API 继续要求认证；未知 `/api/` 路径不回退网页。没有 `webui` build tag 的开发构建只提供构建提示页。

### 模型计价

模型计价补充：`GET /api/models` 返回有效 `Pricing`，并通过 `PricingInfo` 标明 `Source`（`builtin` / `manual`）、可用的自动价格 `Automatic`、`RuleID`、`Version`、`Period`（`peak` / `off_peak`）、`SourceURL` 和 `CalendarKnown`。`configure-model` 的 `Model.Pricing=null` 表示使用匹配的内置规则；不存在规则时仍为未知价格。保存 Provider 可传 `PricingProvider`（`deepseek` / `openai` / `ollama` / `custom`），连接详情返回该字段；为空时根据官方地址识别，`custom` 禁用自动规则。目前仅内置 DeepSeek，且仅匹配配置明确列出的官方地址与模型标识。

## 助理与计划

### 助理头像

助理对象增加可选的 `Avatar` 字符串。创建助理可省略；更新时省略或传 `null` 保留原值，传空字符串清除。未设置时 UI 使用名称首字符，克隆时继承原头像。

支持预设 ID（`preset:` 前缀加 `robot`、`cat`、`fox`、`bear`、`rabbit`、`panda`、`owl`、`frog`、`penguin`、`whale`、`sprout`、`star`），或 PNG/JPEG 的 Base64 data URL。上传头像包含完整 data URL 不超过 128 KiB、宽高均不超过 256 px；Runtime 解码验证并重编码，移除元数据。拒绝外部 URL、SVG、无效图片及未知预设。缩略图随助理身份保存、备份，不创建独立附件。

WebUI 支持上传 10 MB / 4000 万像素以内的 PNG、JPEG、WebP，浏览器居中裁剪并压缩为 256 × 256 JPEG 后提交；原图不上传。12 款预设为本项目原创 SVG，源码位于 `packages/ui/src/components/AssistantAvatar.tsx`，无第三方头像素材或库的许可证依赖。

### 助理自改资料工具

内置 `profile_update` 接收可选 `name`、`avatar` 和必填 `expected_version`；至少提供名称或头像。头像对象的 `type` 为 `preset`（`id` 使用上述预设目录）、`file`（`path`，可选 `sandbox_id` / `resource_id`，与 `file_read` 路径规则一致）或 `clear`。省略字段保持不变，拒绝 `null`、未知参数、外部 URL 和 Base64。工具对象由执行身份确定，不接收 `assistant_id`。

`assistant.Service.UpdateSelfProfile` 校验当前助理身份并在事务中校验版本，只写 `Name` / `Avatar`，同时保存幂等回执和 `assistant.profile` 实时事件。图片读取支持本地及远程沙盒，使用原有读取授权、边界路径与符号链接检查；PNG/JPEG/WebP 限 10 MiB / 4000 万像素，由 Runtime 居中裁剪、缩放、重编码为 256 × 256 JPEG，再执行现有头像校验。处理图片不需要模型具备视觉输入能力。

成功回执为 `{Changed, Profile: {Name, Avatar: {Type, ID?, Digest?}, Version}}`；头像摘要类型为 `initial` / `preset` / `image`，不返回图片载荷。相同内容不增加版本或发重复事件。版本冲突返回 `conflict`、`side_effects_possible=false` 和 `error.current_profile`，不自动重试。工具成功回执通过现有 operation 链路重放。

所有者更新接口仍保持 owner-only；聘请、克隆及更新的名称统一去首尾空白，最多 256 个 Unicode 字符，拒绝空名和控制字符。用户手动更改名称或头像也发布 `assistant.profile`。WebUI 收到事件刷新资料；未编辑表单同步新值，有草稿的表单保留原版本，提交冲突后重新打开核对。此功能不增加公开 HTTP 写入接口或新的数据库字段。

### 助理等待原因

`GET /api/assistants/{id}/status` 的 `WaitingReason` 返回稳定的原因码：`model_not_configured`、`model_unavailable`、`model_not_authorized`、`model_pricing_missing`、`budget_exceeded`、`model_authentication_failed`、`model_rate_limited`、`model_service_unavailable`、`network_access_denied`、`context_limit`、`unsupported_attachment`、`step_limit` 等。未细分的错误保留原有错误 code，不返回异常文本、供应商响应或凭证。原有 HTTP 状态和调度错误分类仍由 code 决定。

WebUI 按原因显示对应说明及操作入口：模型权限、选择模型、模型价格、提供商连接、当前助理的预算设置。已有泛化的 `failed_precondition` 状态可根据当前模型目录明确显示价格缺失、模型不可用或未设置助理预算；只读取配置，不重放消息、不自动调用模型。

### 助理列表活动摘要

`GET /api/assistants/activity` 返回当前账号活跃助理的分页摘要，使用通用 `limit`（默认 50，最大 200）与 `cursor`，返回 `Items`、`NextCursor`。每项包含 `AssistantID`、`ConversationID`、`Dreaming` 和可空的 `Preview: { Kind, Text, CreatedAt }`。`CreatedAt` 为该条消息的创建时间，客户端用于在置顶与未置顶助理内分别按最新消息倒序排列；无消息时 `Preview` 为 `null`。文本合并空白后保留前 80 个 Unicode 字符，超出部分以省略号表示；卡片取标题或摘要，空文本由客户端按类型显示占位文案。摘要使用固定大小的字符缓冲区，确认超长后停止遍历，不为整条消息构造归一化副本；尾部空白不计为额外内容。解雇助理不包含在结果中。每页批量读取已授权会话的最新消息引用和对应消息，不逐个读取历史列表。

该接口沿用助理与会话服务的账号归属检查。执行状态按本页明确授权的助理键批量投影，只读取做梦判断所需字段，每页检查一次运行环境；不加载上下文、加急或中断详情。消息只查询各会话最新一条，不遍历历史。当前聊天的完整状态仍使用 `/api/assistants/{id}/status`。

### 助理能力信息

助理能力信息：`GET /api/assistants/{id}/capabilities` 只允许具有对应助理读取权限的所有者访问，不依赖开发工作台开关，也不创建助理执行身份。返回 Runtime 已安装的 `Tools`（定义、参数、所需操作、当前 `Available` / `Reason`、匹配的存储 `ResourceIDs`）及 `Skills`。存储工具的条件结合资源类型、状态、有效授权、只读范围和过期时间计算；具体资源与路径仍在执行时重新授权，不能把能力展示当作调用许可。当前支持助理工作区 `.agents/skills/*/SKILL.md` 的发现与按需上下文加载，`Skills.Supported=true`；`Items` 返回工作区内可发现的 Skills。Skill 是工作流程指引，不是额外执行器，也不授予权限。

### 助理预算

助理预算：聘请与更新助理均需传 `Budget: {"Limit":{"Currency":"USD","Micros":1000000},"Period":"daily"}`，周期仅支持 `daily` / `weekly`。额度非负，零明确表示仅允许免费调用；没有全局默认额度。任务不接受 `Budget`，设置不接受 `DefaultTaskBudget`。克隆继承额度和周期，各助理独立记账，返聘保留原助理账本。

`GET /api/usage?assistant_id=<id>&current_period=true` 返回当前周期的 `Limit`、`Spent`、`Reserved`、`PeriodStart`、`PeriodEnd` 和历史费用未知操作。当前周期查询必须指定助理，不能同时传 `from` / `until`；未指定当前周期时仍可查询历史费用。重置按设置中的时区每日零点／每周一零点计算，包含夏令时变化。跨周期仍在执行的调用持续占用预留额度；本地调用退出后 unknown 不再占用预留，仍允许后续可信回执结算。结算计入调用原所属周期；历史记录不清空，调整上限或周期也不清空账本。未设置有效助理预算时返回 `budget_exceeded` 与 `assistant_budget_missing` 原因。

### 近期计划事件

`GET /api/plans` 在同一读事务中加载账号内的待办与日程，按批次读取可见计划及其直接依赖的关联记录、执行历史。批次限定账号和计划引用，不读取已删除计划或无关计划的执行；不可见依赖只用于就绪判断，不加入返回列表。

`GET /api/plans/events?from=<RFC3339>&limit=50&scope=all&kind=all&state=all&cursor=…` 返回 `{Items: [{Key, At, Plan}], NextCursor}`。`limit` 为 1–200，默认 50；`scope` 为 `all`、`me` 或助理 ID；`kind` 为 `all|item|event|automatic`；`state` 为 `all|open|done`。`from` 必填且包含时区偏移。

每项代表一次发生，按发生时间、计划引用稳定排序。事件投影只保留最早的 `limit + 1` 个候选，再生成当前页及后续游标；计划来源读取仍随账号计划数量增长。游标绑定所有者、起点和筛选条件，不允许跨查询使用。同一时刻不同计划不会因分页遗漏；重复计划的每次发生不会折叠成系列。投影复用通用重复规则，包含跨年等稀疏安排，不受月历日期范围限制，也不会推进调度游标、创建任务或触发执行。已完成筛选仅检查对应发生的执行记录，不能把整个系列标为完成。原 `GET /api/plans?from=…&to=…` 继续用于月历和计划详情。

### 手动上下文压缩

`POST /api/assistants/{id}/compress` 接受 `{ "Mutation": { "IdempotencyKey": "..." } }`，要求所有者身份及助理写权限。返回 `{ "EventID": "...", "State": "queued" }` 表示已接受请求，不代表压缩完成；相同幂等键返回原接收回执，不同键的重叠请求合并。固定创建 `after_step` 事件，无客户端投递方式参数。实际处理沿用模型授权、预算及压缩校验；持久停用／暂停的助理拒绝请求。

状态接口新增可选 `Compression: { EventID, State, Reason? }`。只在 `State=running` 时展示“正在压缩上下文”，完成后按原生命周期状态展示；失败结果提供原因码。通过既有 `assistant.state` SSE 刷新，不公开摘要或私有上下文。

## 授权

### 授权申请审批

- `GET /api/permission-requests/{id}`：所有者读取单个真实申请供聊天卡片与审批表单使用，校验对象所有权。
- `GET /api/permission-requests?assistant_id=<id>`：所有者读取该助理最近最多 200 项申请，待审批优先，返回数组。
- `POST /api/permission-requests/resolve`：`Mutation`（幂等键及预期版本）、`RequestID`、`Decision: approve|reject`。批准还需 `ResourceID`、`Actions`、`Scope` 和可选 `ExpiresAt`；普通计算、网络和受管工作区授权还需 `Constraints`，组合审批字段见[资源额度](../security/resource-quotas.md#数据与审批)；拒绝不得携带授权字段。所有者可修改预填的资源、操作、范围与授权时间，最终值仍由 Grants 服务校验资源归属、支持的操作、范围和部署限制。
- 状态为 `pending / approved / rejected`，记录 `GrantID`、实际批准的 `Granted` 快照、创建和决定时间以及版本。申请本身不等于授权；批准与拒绝均产生决定上下文和恢复事件，旧停止边界与任务终态仍被尊重。
- SSE `permission.request` 只包含助理引用，客户端重新读取申请与权限列表；请求创建还会通过正常 `message.created` 发布助理聊天卡片，申请和卡片一起持久化。

工具参数、等待及恢复语义见 [授权请求](file-tools.md#授权请求)。

### 权限编辑

权限编辑：`POST /api/grants/update` 接受 `GrantID`、`Actions`、`Scope`、`ExpiresAt` 及带 `ExpectedVersion` 的 `Mutation`。授权对象和资源不可变；可修改该资源内的模型范围或存储路径。操作需要所有者管理权限，验证资源支持的操作和新范围，原子更新版本并写入审计；不能恢复已撤销授权。

### Full Access 权限字段

WebUI 采用「资源类型：沙盒 → 选择沙盒 → 权限类型：存储／计算／网络／完全访问」的展示层级，完全访问不嵌在计算额度表单中。API 仍以节点计算资源 ID 与 `Scope.FullAccess` 表达独立授权；前端切换目标时重置无关的路径、操作和额度，服务端继续校验最终请求。

- 沙盒配置：`sandbox.allow_full_access`（默认 false），无节点模式字段。`GET /api/sandbox`、沙盒列表的 Info 提供 `Capabilities.FullAccess`，其余五项能力仍只描述普通隔离路径。普通成员列表按组委派隐藏不可授予的 Full Access 能力。
- 创建／更新授权与审批：`Scope.FullAccess: true` 仅用于计算资源的 `Actions: ["execute"]`；不能携带 `Constraints`、`Temporary`、路径、模型或只读范围。普通计算授权仍需配额，两者可独立存在。
- 模型 `permission_request` 与组合子申请：`full_access: true`，不得附带额度；仍需到期时间。`permission_list` 保留可申请与已授权的 Full Access 元数据。
- 模型命令／文件调用：`full_access: true` 选择直接路径，每次重新校验授权；省略时继续普通路径。Shell 任务返回 `FullAccess`；十个任务的并发上限仅适用于普通隔离任务。
- 用户组沙盒引用：`Resources[].AllowFullAccess`，默认 false，仅 `Kind: "sandbox"` 可设置。管理员目录返回 `FullAccessAvailable`；撤回组委派同时使已有授权失效。
- 远程协议版本 7，URL 仍为 `/v2/sandbox/`，可信请求携带 `FullAccess`。两端分别检查部署能力与授权租约，不依赖旧缓存自动提权。

### 计算权限中的 GPU

`SandboxInfo.GPUs` 返回 `{ID, Name, Vendor, MemoryBytes, Available, Reason}` 数组；`AuthorizationCeilings.GPUDeviceIDs` 为部署与用户组允许的设备范围。`Constraints.compute.gpu` 可选，结构为 `{"device_ids":["GPU-00000000-0000-0000-0000-000000000001"]}`；缺省或空列表无 GPU 权限。基础／临时授权及 `permission_request` 使用同一字段。`shell_execute` 不新增 GPU 参数，按 `compute_id` 的生效授权注入；任务详情 `GPUDeviceIDs` 记录启动设备。远程沙盒协议版本为 7。

## Agent Shell 沙盒

`GET /api/sandbox` 要求账号身份和读取权限，返回 `Enabled`、`Capabilities`；仅管理员可见部署允许连接的 `Networks` 与 `Directories`，普通成员收到空目录列表；不返回 helper、rootfs、cgroup 或私有数据目录，不开放修改部署隔离配置或直接创建命令的 HTTP 接口；既有 Shell 任务的读写与停止见下文。

启用后，`POST /api/resources` 支持 `Kind: "compute", Provider: "local-sandbox", Binding: {}` 和 `Kind: "network", Provider: "sandbox-network", Binding: {"SandboxTarget": "部署目标名"}`。它们支持 `execute` 授权；目标不能由 WebUI 自行增加。存储仍使用 `local-directory`，默认按存储资源和助理授权直接挂载；部署者可通过 `sandbox.restrict_directories` 启用额外目录白名单。

内置 `permission_list` 是与 Shell 开关独立的授权与可申请权限查询，返回 `Resources`（`Grants` 中的有效授权 ID、操作、范围和有效期，资源内路径或模型范围、各操作可用性与原因、稳定 `ShellPath`）、`Requestable`（可申请资源的安全描述，不是授权）和 `BuiltInStorage`（Runtime 提供的助理工作区与本次执行临时目录）。网络资源在 `Resources` 与 `Requestable` 中提供 `Network`：当前目标地址、端口、协议及排除项，系统限制、回环访问方法和执行网络限额；离线或来源丢失时 `Known=false`，不按名称猜测范围。资源路径 `/` 不是宿主根目录。文件工具与 Shell 共用存储授权及部署目录限制，Shell 仍需额外计算授权；查询不是授权凭证。`shell_execute` 使用现有 Agent 工具调用链和操作回执，不直接发布聊天消息。助理信息页通过原有 capabilities 接口显示工具状态。部署条件与语义见 [Sandbox 使用说明](../../docs/sandbox/linux.md)。

Runtime 启动时自动准备并探测命令环境，自动登记本机计算资源，不自动给助理授权。`Capabilities.DiskProtection` 当前为 `file_limit_and_free_space_guard`，表示单文件硬上限与磁盘低水位保护，不代表目录总容量硬配额。

外观偏好：`Settings.Theme` 保持 `system` / `light` / `dark`；`Settings.Palette` 支持 `forest` / `ocean` / `iris` / `sand` / `rose` / `monochrome`。旧记录读取时默认 `forest`，旧客户端省略或传空配色字段时保留已有选择，未知值返回 `invalid_argument`。更新仍使用设置版本检查，不修改其他业务偏好。

### Shell 任务管理

- `GET /api/assistants/{id}/shell-tasks`：当前所有者读取该助理的所有运行任务及最近 50 条历史记录，返回 `Items` 和 `Limit: 10`。模式为 `foreground/background/tty`，状态为 `running/completed/failed/stopped/interrupted`。
- `POST /api/assistants/{id}/shell-tasks`：`action` 为 `read/write/interrupt/stop/stop_all/resize/list`。单任务操作使用 `task_id`；读取可传 `cursor`、`stream`，返回 `Output`、`NextCursor`、`Path`；结束任务还返回 `Finished`（输出捕获已结束）和 `OutputComplete`（捕获是否完整）。`Finished=true` 且当前流读空后可停止读取，不能将截断导致的 `OutputComplete=false` 当作任务仍在执行。读取是只读操作，客户端不应据 POST 方法触发业务数据失效。TTY 输入传 `data`（最多 8 KiB），resize 传 `rows/cols`（1–500）。所有者会话仍需 CSRF，跨所有者或助理访问拒绝。
- Shell 任务首次创建及状态变化在同一事务中追加 `shell.tasks` SSE，仅包含所属助理引用，不包含命令和输出；客户端据此刷新对应任务列表，保留低频校准。
- 模型使用相同语义的 `shell_task` 工具，只能管理自身任务。`shell_execute` 新增 `tty/background/timeout_seconds`，前台等待最多 60 秒。可选 `forward_ports` 声明最多 4 个 TCP 端口。创建回执的 `Task.Forwards` 及任务列表 `Forwards` 返回 `Protocol/Port/Address/URL/State`，状态为 `waiting/ready/closed`；地址统一位于 Runtime 机器，HTTP URL 只是便利链接。创建回执包含任务及通知说明；完成和运行提醒使用 `shell_task_finished` / `shell_tasks_running` 事件，结构化信息位于 `Details`，不伪造聊天消息。

## 路径记忆接口

记忆无专用 ID，以当前账号下的 `Scope: assistant|global`、`AssistantID`、相对 `Path` 定位。全局范围的 AssistantID 为空。Path 为 `MEMORY.md` 或 `.agents/memory/` 下语义文件路径，创建必须提供；同名不覆盖。文件 `Version` 是完整字节 SHA-256，更新、删除使用 `Mutation.ExpectedVersion`，旧版本返回 409。

`GET /api/memories` 支持 `scope`、`assistant_id`、`state`、`q` 和分页参数。结果按作用域、助理 ID、路径排序；`NextCursor` 绑定账号及筛选条件，续页时保持这些条件，可调整页大小。游标记录逻辑文件位置，文件外部编辑即时生效，已读位置之前新增的文件需重新从首页查看。

`GET /api/memories/file?scope=…&assistant_id=…&path=…` 读取文件，`GET /api/memories/directory` 浏览目录。`POST /api/memories`、`/update`、`/delete` 管理文件，删除的 `Forget: true` 显式启用遗忘。`/mkdir` 创建目录，`/move` 接受 ToPath、Directory 和原版本，`/rmdir` 只删除空目录。所有写请求携带 Mutation。旧 `/api/memories/{id}` 不存在。详细约束见 [文件记忆](../runtime/dream-memory.md)。

全局搜索的记忆命中通过 `Memory: {Scope, AssistantID, Path}` 返回引用。设置清理通过 `memory_files` 传记忆文件数组；其他业务对象继续使用 object_ids。Dream 候选采用 Action、Path、ExpectedVersion 和可选 Replaces 文件引用，返回记录以 MemoryFiles、MemoryVersions 记录路径与提交版本。

## 微信聊天入口

以下接口要求当前账号具有目标助理的读／写权限；POST 使用原有认证、CSRF 和 Origin 检查。状态中不返回微信 ID、令牌、回复凭据或游标。渠道绑定后，普通 `POST /api/conversations/messages` 返回 `failed_precondition`；只读记录和其他管理接口保持原授权。

| 方法和路径 | 请求 | 返回 |
| --- | --- | --- |
| `GET /api/assistants/{id}/weixin` | 助理 ID | `Channel`（`webui` / `weixin`）、`State`、连接及最近入站时间、`LastError`、最近 30 条 `Deliveries` |
| `POST /api/weixin/login` | `AssistantID` | `ID`、`Status`、`QRCode`（需本地编码为二维码的内容）、`ExpiresAt` |
| `POST /api/weixin/poll` | `AssistantID`、登录 `ID`、可选 `VerifyCode` | 登录状态；等待请求最多约 25 秒，前端继续轮询 |
| `POST /api/weixin/cancel` | `AssistantID`、登录 `ID` | `null`；只取消扫码，不删除已存在绑定 |
| `POST /api/weixin/disconnect` | `AssistantID` | `null`；删除绑定、取消待发送回复、恢复 WebUI 输入 |

登录状态包括 `wait`、`scaned`、`need_verifycode`、`verify_code_blocked`、`expired`、`conflict`、`confirmed` 和 `already_connected`；已连接响应无需二维码，现有绑定及队列保留。重定向由后端校验并处理。绑定状态包括 `connected`、`reconnecting`、`login_required` 和 `paused`。投递状态包括 `pending`、`sending`、`accepted`、`waiting`、`failed`、`unknown` 和 `canceled`。`accepted` 不表示用户已读。文字分段与各附件分别占一条投递记录；`Error=attachment_failed` 表示消息发送前的文件读取或上传失败，最终发送接口无法确认时仍为 `unknown`。绑定与投递变化通过 `weixin.changed` 事件使界面重新读取状态。

## 系统提醒状态

- `GET /api/notifications`：当前账号通知，支持 `filter=unread|pending|active|all`、`assistant_id`、`kind`、`limit`、`cursor`。`pending`／`active` 均表示未忽略且未解决，`unread` 额外要求未读。返回 `Items`、`NextCursor`、当前账号全局 `UnreadCount`、`PendingCount`。游标绑定账号和查询条件。
- `GET /api/notifications/{id}`：读取当前账号的指定通知。
- `POST /api/notifications/update`：`{ "IDs": ["通知ID"], "Action": "read" | "dismiss" }`，一次最多 100 个 ID，整批事务提交。重复确认无额外效果；忽略同时记录已读，不改变业务源状态。接口沿用会话、Origin 和 CSRF 校验，不能创建通知或客户端标记已解决。

记录包含 `Kind`、`Code`、`Source`、`AssistantID`、`Label`、`Severity`、创建／更新时间、`ReadAt`、`DismissedAt`、`ResolvedAt`、`Version`。来源变化通过 `notification.changed` SSE 事件通知刷新；断线期间的通知可经列表恢复。

### 会话事件的通知引用

会话 SSE 事件新增可选 `ConversationID`，包括 `message.created`、`message.feedback`、`message.accepted`、`message.read` 和 `conversation.read`。它是安全对象引用，不携带消息正文；旧事件可没有该字段。客户端按会话及 `Object.ID` 调用现有 `GET /api/conversations/{id}/messages?message_id=…`，读取权限仍由 Runtime 校验。通知仅消费新助理消息，不能把任意事件或查询刷新视为新消息。

## 调试工作台

调试工作台：`GET /api/status` 增加布尔字段 `workbench_available`，`Settings.WorkbenchVisible` 只控制用户界面入口显示，不影响采集和调试接口权限。采集只受部署配置 `debug.enabled` 控制；部署未开启时拒绝保存显示值 `true`。专用调试接口及记录边界见 [开发工作台](../runtime/workbench.md)，检查点和完整请求不进入普通状态／聊天 SSE。

保留策略：`GET /api/debug/retention` 需要所有者及 `debug.read`，返回 `Policy`（`Version`、`MaxRuns`）和当前账号的 `Runs`，不受个人显示开关影响。`GET /api/admin/debug/retention` 返回生效策略、`Configured`（`MaxRuns` 为整数或 null）和 `Defaults`；`POST` 请求为 `{Policy: {MaxRuns}, Mutation: {IdempotencyKey, ExpectedVersion}}`，两者均需管理员及 `admin` 作用域。条数范围 1–10000；null 或省略恢复部署默认值。保存持久化并通知客户端，在后续写入及执行结束时按条数清理最早的已完成执行；运行中执行保留到结束，做梦轨迹也参与条数计算。不按时间或总容量清理、不再产生 `capacity_reached`，单条负载仍可能标记 `too_large`。详情采用[上下文存储](../runtime/context-storage.md)的独立引用，历史删除不能破坏当前执行状态。

上下文重置：`POST /api/debug/assistants/{id}/context/reset`，请求为 `{ "Mutation": { "IdempotencyKey": "..." }, "ExpectedSequence": 0 }`。`ExpectedSequence` 使用最新检查点的序号，无检查点时为 0。需要所有者身份、助理写权限与工作台启用状态；令牌还需 `debug.read` 和 `debug.write`。重置取消旧执行、清空私有上下文并递增序号，保留聊天、记忆、权限和账本，不自动请求模型。版本冲突返回 409，重复提交同一幂等键不会再次重置。

## 版本与发布更新

`GET /api/status` 的 `build` 返回 Runtime 的 `version`、`commit`、`built_at`、`os`、`arch`、`embedded_webui`。

- `GET /api/updates`：登录后读取实例共享快照，不访问发布源。包含 `current`、`latest`、`comparison`、`check`、`auto_check`、`source_configured`、`supported`。`comparison` 为 `unknown/current/available/ahead/uncomparable`；`check` 分别记录检查中、最近尝试、最近成功、下次允许手动检查时间及固定错误码。
- `POST /api/updates/check`：请求 `{ "automatic": false }` 手动检查，`true` 用于关于页自动检查。遵守 CSRF；自动检查间隔至少五分钟、手动至少一分钟，并合并进行中的请求。发布源网络或清单失败保存在快照中，不清除成功缓存。
- `GET /api/updates/installation`：仅管理员读取升级命令所需的安装记录和发布源；缺少匹配记录返回 `null`。不执行安装。

检查服务在 Runtime 生命周期内运行，内存缓存重启后重新建立；请求限定十秒、清单最多 64 KiB。公网 HTTPS、显式私网地址及拨号时 DNS 校验复用平台网络边界，禁止重定向，不携带用户凭证。版本与发布元信息见部署指南；接口不接受任意 URL。

## 验证

API 合同测试使用真实 HTTP server、Auth、CSRF、持久限流、Realtime 与 SQLite，其他模块也采用实际实现；仅固定供应商目录与组合层归属依赖受控。测试覆盖登录 Cookie 与秘密 DTO、二次验证消费、权限能力隔离、可信代理、SSE 撤销、服务启停和失败关闭。
