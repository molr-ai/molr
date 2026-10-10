# 插件机制（Plugin API 3）

Runtime 仍为 Go 单体。插件以不可变清单、隔离 HTML、工作区脚本、声明式 HTTP 操作和 Skill 扩展产品，不加载 Go 动态库，不执行安装脚本。用户入口见 [插件使用](../../docs/plugins.md)。不预装搜索插件或验收示例。

## 包与协议

开发包为普通目录，分发使用 ZIP，根目录为 `plugin.json`，其余文件为配套文本。`manifestVersion: 1`、`pluginApi: 3`；旧协议不执行、不自动迁移。页面通信合同独立，仍使用 `apiVersion: 2`。

导入限制 8 MiB、256 个配套文件、每文件 1 MiB、24 个贡献项。ZIP 拒绝链接、重复路径、绝对路径、越界路径及超过展开限制的内容。当前文件为 UTF-8 文本，二进制 CLI 及运行依赖由助理下载到工作区。ID 使用反向域名，`molr.*` 保留；版本为三个无前导零数字段。包存储仍规范化为 `manifest` 和 `files`，SHA-256 固定内容，同 ID/version 不可覆盖；JSON API 可直接提交同一规范结构。

```json
{
  "manifestVersion": 1, "pluginApi": 3,
  "id": "example.notes", "name": "Notes", "description": "Notes CLI", "version": "1.0.0",
  "requirements": [{"phase":"install","kind":"network","description":"Download the CLI","destinations":["downloads.example.invalid"]}],
  "credentials": [{"id":"token","title":"Service token"}],
  "contributes": {"skills":[{"id":"notes","name":"Notes","description":"Install and use Notes","entry":"skills/notes/SKILL.md"}]}
}
```

`requirements` 最多 32 项，phase 为 install/use，kind 为 compute/network/storage，description 说明用途，destinations 为可选目标提示；它们不生成资源或授权。`credentials` 最多 16 项，每项声明 id/title，值由用户通过宿主配置入口提供。打包命令为 `node scripts/pack-plugin.mjs <目录> <输出.zip>`，输出放在源目录之外。

| 扩展 | 合同 |
| --- | --- |
| themes | `id`、`name`、`pages`、`capabilities`；可替换 assistants / plans / resources / workbench，不能替换 settings 或登录；页面是包内 HTML |
| pages | `id`、`title`、`entry`、`capabilities`；通过 `/#/extensions/<plugin-id>/<page-id>` 进入 |
| tools | `id`、`description`、`execution`、`inputSchema`、`capabilities`；仅支持可选的 HTTP 工具；CLI 使用 Skill 与普通 Shell |
| skills | `id`、`name`、`description`、以 SKILL.md 结尾的入口、可选引用的本包 HTTP 工具 ID；正文不超过 64 KiB，由 file_read 按需加载 |
| settings | 顶层字段名到 Setting 的映射；最多 16 项 |
| settingsPage | 可选的个人设置 Page；声明后直接作为配置入口，不同时展示宿主表单 |
| adminSettings / adminSettingsPage | 可选的共享管理员设置字段（最多 16 项）及自定义管理员页面；仅管理员可读写 |
| providers | 顶层 HTTP provider 列表，最多 8 项；由固定宿主执行器解释 |

工具输入仍采用封闭 object Schema，最多 16 个 string / number / integer / boolean 属性，支持 required 和字符串 enum，`additionalProperties: false`。参数最多 16 KiB，字符串最多 8 KiB，整数必须为 JavaScript 安全整数。

## 设置、连接与身份

安装、访问状态和配置分开保存：

- `plugins.installations` / `plugins.packages`：共享安装目录；管理员管理版本、权限、启停、助理绑定。
- `plugins.connections`：共享服务连接，映射到 `connection` 类型资源；管理员管理，普通用户通过用户组资源范围获得使用权。provider 是协议定义，connection 是账号实例。
- `plugins.settings`：个人配置使用真实 `OwnerID + PluginID`；管理员配置使用安装目录 OwnerID 与 `admin:PluginID`，共享且仅管理员可读写。连接 ID 推导 provider。
- `plugins.preferences`：每个真实 OwnerID 的启停偏好与 CAS 版本；默认启用，仅在全局启用且用户组授权后有效，不依赖设置 Schema。
- 用户组 `plugin` 资源引用：ID 为 `plugins.Key(pluginID)`，控制插件页面、主题、配置和工具的访问；工具与连接另有各自范围和额度。插件引用没有配额。

安装目录最多保留 32 个未删除插件，每个插件最多保留 32 个未删除服务连接；停用的项目仍计入上限。移除释放名额，历史记录、发布版本和调用记录继续保留；列表查询在数据库中排除已删除记录。重新安装同样检查剩余名额；现有项目的更新与幂等重试不重复占用名额。

Setting 字段：`type`、`title`、可选 `description`，支持 string / boolean / number / integer / connection。普通字段必须声明 `default`；字符串支持 `enum`，数值支持 `minimum` / `maximum`。connection 字段声明 `providers` 及可选 `requiredOperations`，不允许默认连接。用户可以显式留空；调用时返回需要配置，不能自动选择另一连接。

读设置返回 `PluginID, Digest, Version, Values, NeedsReview`。保存是完整值对象，省略普通字段采用默认值；省略连接字段清除该选择。服务端校验字段、值、连接权限及版本。配置绑定包摘要；新包会保留可表示的旧值，但置 `NeedsReview`，用户复核保存后生效。并发保存使用 `ExpectedVersion`；个人偏好变化不取消整个插件。

统一入口为 `/#/extensions`，按角色展示安装、移除、组授权、全局／个人启停以及可选配置。个人配置路径 `/#/extensions/<id>/@settings`，管理员配置路径 `/#/extensions/<id>/@admin`；保留字符 `@` 避免与页面贡献 ID 冲突。配置的 Schema 和页面都可省略；无声明不显示入口，个人设置 API 返回 NotFound。自定义页面优先，否则宿主生成表单。管理员配置另展示 provider 声明对应的服务连接。插件页面获得的连接描述不含 API Key 或 CredentialID。凭证输入和二次验证由宿主处理。首版不支持普通成员自行管理 Key。

## 助理工作区与执行

`plugins.instances` 按真实 OwnerID、AssistantID 和 PluginID 保存启用状态、固定包版本和 CAS 版本，独立于共享安装目录的当前页面／HTTP 版本。管理员与成员都只能配置自己的助理；成员仍须获得用户组插件与相关普通工具、沙盒资源授权。

启用实例时将包文件复制到 `/work/.local/plugins/<id>/versions/<version>/package/`，原有文件不覆盖。config/data/cache 位于插件根目录，跨版本保留。复制通过无链接的文件操作并应用工作区额度，不执行安装脚本。CLI 和 Node/Python 等运行依赖由助理通过普通 Shell 在工作区准备，不修改宿主软件环境。

插件 Skill 目录使用 `/plugins/<id>/<version>/<entry>` 虚拟原包路径，通过普通 `file_read` 读取，返回当前包摘要。每次读取校验账号、助理实例、版本、个人启用和组权限。成功读取当前版本才激活正文，失败读取不激活；停用后撤销自动上下文。每个 Skill 不再注册独立读取工具。配套参考文件也可通过同一入口读取；脚本执行使用工作区副本。

移除 `execution.type=shell` 及其独立调度。所有 CLI 调用复用 `shell_execute`、文件工具和 `permission_request`，网络、后台、TTY、配额及撤权都沿用普通工具。沙盒隔离边界是助理，不承诺同一工作区的插件彼此隔离。

版本目录中的 `PACKAGE.md` 记录来源、摘要、目录及状态约定。若同时提供 `scripts/install.sh` 和 `scripts/check.sh`，生成 `prepare.sh`；助理显式通过 Shell 调用它，以该版本目录为当前目录，使用插件根目录 `install.lock` 串行安装。检查成功后写入该版本的 `status.json`，并原子替换根目录 `active.json`。失败保留旧活动版本。中断残留的锁需要检查 Shell 任务后恢复，不自动猜测锁已失效。

`status.json` 使用 `Digest` 和 `Environment`（pending/installing/ready/failed），摘要必须对应实例版本。`config/account.json` 使用 `Account`（unknown/pending/ready/not_required）。界面展示这些文件作为助理最近一次检查的报告，不执行后台探测，不展示其中的任意日志文本。没有通用安装脚本的 Skill 也可按说明安装、检查并报告状态。

CLI 凭证通过声明的字段写入 `config/secrets/<id>`，每次保存强制 0600、拒绝链接；API、审计和模型参数不返回明文。写入要求所属账号、助理实例与敏感操作验证。CLI 可读取文件，该助理的其他程序也可读取；HTTP vault 凭证仍遵循下文的独立 broker 边界。

清理 API 在实例停用后删除该助理的插件子树，子树内的依赖链接只删除链接本身，不跟随目标。卸载全局插件会停用全部实例但保留工作文件，自己的实例列表在撤权／卸载后仍可读取和清理。已有程序的网络访问由助理资源授权控制，插件停用不等于撤销已有 Shell 能力；清理前应停止相关 CLI 任务。

### HTTP

```json
{
  "id": "lookup", "description": "Look up a value",
  "execution": { "type": "http", "connectionSetting": "backend", "operation": "lookup" },
  "capabilities": ["http.request"],
  "inputSchema": { "type": "object", "properties": { "query": { "type": "string" } }, "required": ["query"], "additionalProperties": false }
}
```

HTTP 工具不绑定 ComputeID。`execution.settingsScope` 省略时，配置字段 `backend` 指向当前用户的连接；设为 `"admin"` 时使用管理员配置的连接与普通字段，插件可完全不声明个人设置。HTTP 请求固定所选范围的设置快照；该范围内必须声明操作引用的全部 setting 字段。工具声明的每个候选 provider 必须实现同名操作，输入合同与工具一致，结果字段类型结构一致。具体路径和字段映射可以不同。同一包版本内切换连接不会改变工具名；工具名仍由插件 ID、包摘要和贡献 ID 派生。

HTTP 工具受全局启用、个人启用、用户组插件授权、工具授权、成员组工具范围／配额、连接组范围／配额、连接总额度共同约束。资源状态和授权在使用时再次检查。切换当前用户连接不改变其他账号的配置。

## HTTP provider 合同

Provider 字段为 `id, name, origin, auth, operations, test?`。

- `origin` 是包内固定 HTTPS origin，不含用户信息、路径、查询或片段；不提供运行时自定义 Base URL。
- `auth.type` 为 `none`、`bearer` 或 `header`；header 模式另声明 `auth.header`。拒绝 Host、Cookie、代理认证及传输控制等敏感头。Key 只能由 broker 注入认证头。
- `operations` 最多 16 项，每项包含 `method, path, inputSchema, query?, body?, result, usage?, errorField?`。method 为 GET 或 POST；固定 path；GET 无 body；每次操作只有一次 HTTP 请求。
- query/body 每个字段是 `{"value": <JSON常量>}`、`{"argument":"参数名"}` 或 `{"setting":"非连接设置名"}`，三选一。引用参数必须在操作 Schema 中声明。常量可包含嵌套 JSON。宿主负责编码，不进行文本插值或脚本求值。query 的最终值必须为标量。
- `test` 可声明 `{operation, arguments, settingsScope?}`，settingsScope 同样可设为 `"admin"`，固定参数必须通过该操作的 Schema。测试只在用户主动点击时发起，同样使用连接配额；测试幂等键复用成功结果，不自动重发未知结果。

Projection 字段为 `type, path?, default?, fields?, items?, maxItems?`。path 为 JSON Pointer；object 用 fields 指定输出字段；array 使用 items 和 1–50 的 maxItems；标量支持 string / number / boolean。最大嵌套深度 4；运行时限制投影节点和文本量。缺失字段需要显式 default，否则失败；类型不匹配失败，不静默造空数据。

请求 body 最多 64 KiB，完整 URL 最多 16 KiB，响应读取最多 1 MiB，投影结果最多 16 KiB；单字符串最多 8 KiB。结果是 `{data, provider, connection_id, usage, duration_ms}`。usage 保留供应商单位，未定价不代表免费。`errorField` 非空时读取业务错误指针，非空／非零错误值转为固定错误，不暴露服务商原始错误正文。

首版不提供 OAuth、动态 URL 抓取、请求链、循环、自动翻页、多服务自动兜底或供应商计价引擎。插件可以把业务 URL 作为固定服务的参数，但 Runtime 不因此获得访问任意 URL 的权限。

## 凭证、网络与回执

连接使用现有 credential vault 加密保存专用凭证；凭证 Provider 身份绑定 `plugin-http:<connection-id>`。使用时校验真实调用者、连接所属插件及摘要、连接状态、凭证 ID 和 origin。插件包、HTML、工具参数及模型上下文没有明文 Key；服务端结果对当前 Key 作脱敏。凭证只能发送给管理员批准的接口；远端服务本身仍属于该连接的信任边界。

HTTP 请求复用 `platform.Guard`：实际拨号时解析、检查并直连已检查的地址，禁止自动重定向，不继承环境代理。公网要求 HTTPS；私网仍需要部署者在静态网络配置放行，插件或页面不能自行放宽。生产连接无需 Shell 网络授权，也不会给予 Shell 新的联网权限。

Agent 执行器在现有 operation admission 事务内保存 `plugins.http-calls`：插件摘要、逻辑工具、连接 ID/版本、设置版本、非敏感配置、参数和时间。连接累计调用与并发检查、成员连接额度检查及回执创建在同一事务完成。每个连接必须设置累计调用上限、1–16 并发和 1–60 秒超时。测试计入同一额度，增加上限不清空计数。

执行中的调用使用已固定配置。成功的 operation 重复投递返回原回执；失败或结果未知不会重新解析 provider 并发请求。发出请求后未取得明确响应时标为 unknown；Runtime 重启将遗留 prepared/running 记录改为 unknown。未知结果保留原周期调用次数，本地请求退出即释放并发占用。启动恢复在取得 Runtime 独占锁之后、启动工作线程之前执行。管理员可在调用记录中标记未知结果已阅，但这不再是释放并发的前提，不退款、不重试，也不声称外部请求成功或失败。

连接更新、停用或删除取消该连接在途请求；插件停用、卸载、版本配置取消该插件在途请求；用户禁用／换组及用户组更新重新校验在途权限。取消不保证外部供应商停止处理或不收费。

## 页面桥接

HTML 必须自包含，可将 `@molr/sdk/plugin-host` 打包内联。iframe 使用 opaque origin、`sandbox="allow-scripts"` 和禁止联网的 CSP；父页面 `frame-src blob:` 限制导航。HTML Blob URL 在组件提交后的 effect 中创建，并由同一个 effect 在文档失效、错误、重试或卸载时释放；更换插件引用后不再展示或连接旧文档。配色和语言变化可复用当前文档 URL，同时重新建立 iframe 的宿主上下文。页面无法访问宿主 Cookie、CSRF、Bearer、DOM 或通用 HTTP 代理。

握手检查 frame window、opaque origin、API 2 和单次 MessageChannel。最多四个并发请求。宿主固定注入页面身份，后端重新校验摘要、贡献声明和权限。普通 page/theme/settings 的 settings 方法只操作当前用户配置；admin-settings 的同名方法只操作共享管理员配置，所有调用先验证管理员身份。插件无法自行切换范围。

| SDK 方法 | 页面 capability |
| --- | --- |
| `plans.list()` | `plans.read` |
| `settings.get()` | `settings.read` |
| `settings.update({expectedVersion, values})` | `settings.write` |
| `connections.list()` | `connections.read` |
| `connections.test({connectionId, idempotencyKey})` | `connections.test` |

宿主上下文只有 locale、theme、reducedMotion 和语义样式 token。重建 frame 后临时状态消失，持久配置必须使用 settings。页面方法不能自选 OwnerID、包摘要或其他插件身份。

## 生命周期与 HTTP 路由

添加包、全局能力授权、全局启停和卸载由管理员管理；个人偏好与助理实例由所属用户管理。组或个人撤权拒绝新的原包读取和 HTTP 调用，HTTP 在途调用重新校验授权。导入新版本只暂存，页面／HTTP 切换保留仍适用的能力和绑定，CLI 实例保持独立版本。HTTP 连接摘要变化需复核，个人／管理员配置继续绑定包摘要。重新添加已卸载插件不恢复全局能力权限或实例启用。

连接删除停用资源并删除专用凭证；历史回执保留。普通成员不能调用管理接口。凭证使用、配置变更和手动确认未知结果都有审计；审计不包含请求参数、配置值或明文凭证。

| 路由 | 请求及用途 |
| --- | --- |
| GET /api/plugins | 可见安装；成员只见全局启用且组授权的项，个人停用项仍可见；不返回管理员工具绑定或管理员设置声明；版本目录用于选择助理实例版本。另返回 CatalogID、ToolIDs、UserEnabled、UserVersion |
| POST /api/plugins/install | application/zip 二进制或 JSON Package；仅保存包 |
| POST /api/plugins/import-url | URL；公开 HTTPS ZIP，禁止私网、代理及重定向 |
| GET /api/plugins/instances | 调用者的助理安装记录，包含已卸载／撤权后的保留项 |
| POST /api/plugins/instances/read | ID, AssistantID；实例及最近检查状态 |
| POST /api/plugins/instances/save | ID, AssistantID, Release?, Enabled, ExpectedVersion |
| POST /api/plugins/instances/credential | ID, AssistantID, ExpectedVersion, Field, Value, Verification? |
| POST /api/plugins/instances/clean | ID, AssistantID, ExpectedVersion；实例必须已停用 |
| POST /api/plugins/preference | ID, Digest, Enabled, ExpectedVersion；仅改变调用者个人启停 |
| POST /api/plugins/configure | ID, ExpectedVersion, Enabled, Grants, Bindings, Release? |
| POST /api/plugins/remove | ID, ExpectedVersion |
| POST /api/plugins/settings/read | ID, Digest, Scope?（仅 admin 或省略） |
| POST /api/plugins/settings/save | ID, Digest, Scope?, ExpectedVersion, Values |
| POST /api/plugins/connections/list | PluginID, Digest |
| POST /api/plugins/connections/save | PluginID, Digest, ID?, Name, ProviderID, Enabled, APIKey?, Verification?, Mutation, CallLimit, Concurrent, TimeoutSeconds |
| POST /api/plugins/connections/remove | PluginID, ID, Mutation, Verification? |
| POST /api/plugins/connections/test | PluginID, Digest, ID, Mutation |
| POST /api/plugins/connections/history | PluginID, ID；管理员读取最近 100 项安全调用摘要；查询只提取展示字段和供应商用量，不加载参数、设置及完整结果正文 |
| POST /api/plugins/connections/acknowledge | ConnectionID, OwnerID, ID；管理员确认未知结果 |
| POST /api/plugins/ui | 固定页面引用、Method 及对应方法参数，由 bridge 构造 |

创建连接和测试请求的 Mutation 包含 IdempotencyKey；编辑／删除另带 ExpectedVersion。APIKey 仅写入；connection view 不返回 CredentialID。Bindings 仅管理 HTTP 工具，包含 AssistantID 和 Tools。CLI Skills 通过实例启用。连接用户组额度复用 Calls、Concurrent、Period、Timezone；不配置金额预算或沙盒约束。

## 实现与验证入口

- `runtime/internal/plugins`：ZIP/HTTPS 导入、实例、原包读取、配置、HTTP 连接及回执。
- `agent/tools/executor.go`：HTTP admission 与现有操作事务、回执重用。
- `app/build.go` / `plugin_connections.go`：凭证与网络边界、组级连接配额。
- `packages/sdk/src/plugins.ts`：包 API 3 与实例类型；`plugin-host.ts`：独立的页面通信 API 2。
- `apps/webui/src/plugins`：管理连接、个人配置、设置页 iframe、调用记录。
- `preference_test.go`：个人启停、插件组授权、管理员设置隔离、无个人设置的共享连接执行。
- `http_test.go`：provider 切换快照、认证、个人隔离、CAS、额度、取消与清单拒绝。
- `apps/webui/tests/plugin-http.spec.ts`：真实 TLS provider、页面、个人设置、成员授权和 Agent 链路；fixture 在测试中生成，不发布搜索产品插件。

浏览器专项：`MOLR_E2E_SANDBOX_SERVICE=1 MOLR_E2E_PLUGIN_HTTP=1 npm run test:webui -- plugin-market.spec.ts plugin-http.spec.ts plugins.spec.ts plugin-workspace.spec.ts`。模型 fixture 是确定性协议驱动，验证执行链路，不代表真实模型遵循率或搜索质量。
