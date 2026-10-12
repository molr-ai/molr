# Runtime 模块 Public 接口

更新：2026-10-03。状态：单体 Runtime 已接入真实实现；验证范围与适配器限制见 [Runtime README](../../runtime/README.md)。

“Public”指单体中 Go 包的导出接口，不是第三方 SDK，也不表示全部方法开放给客户端。当前包按 IM、Agent、业务和公共支撑分组，使用固定的进程内调用；不加载 Go 动态业务模块；插件通过[受控扩展点](../extensions/plugins.md)提供页面、主题和 HTTP 工具。

## 1. 公共调用规则

- 带 I/O 或业务操作的方法接受 `context.Context`，传播取消与截止时间。
- 身份由 `auth` 或受信任调度产生 `core.Principal`；不能把 HTTP 请求或模型参数解码成可信身份。
- 普通业务接口检查对象归属、令牌范围和当前权限；Agent 执行另绑定助理、执行与操作标识。结构校验不代替这些检查。
- 创建及重要变更使用 `core.Mutation` 幂等键；更新已有对象要求版本匹配。相同键不同参数返回冲突，不能重复产生外部副作用。
- 列表使用有界分页和不透明游标，金额使用整数微单位与明确币种，避免浮点金额和无限结果集。
- 错误使用 `core.Code`，API 映射固定公开消息，不暴露底层错误、凭证、路径或内部堆栈。
- `core.Secret` 默认打印与序列化均脱敏，只有可信适配显式读取。秘密值不出现在 Agent 的模型／工具接口中。

接口注释中的授权、原子性和恢复规则是具体实现必须满足的合同。各模块已接入真实实现；接口存在或测试映射齐全不代表所有执行分支均已验证，验证范围见各模块测试。

## 2. IM

| 包 | 导出接口 | 核心职责 |
| --- | --- | --- |
| `im/conversation` | `Client`、`Publisher`、`Service` | 查询会话／消息、用户发送、助理发布、已读和反馈 |
| 同上 | `Repository`、`Outbox` | 事务内消息持久化与可靠消息创建事件 |
| `im/realtime` | `Feed`、`Subscription`、`Store` | 有权限的实时订阅、提交后唤醒、持续鉴权、持久事件与批量重放 |

用户和助理写入共用 `Message`、`Content` 与仓储。两个入口的区别是身份与业务触发语义，不是两套 IM：发送者不能由请求指定；用户输入可唤醒 Agent，助理发布不应再次唤醒自己。

IM 保存 `MessageCreated` outbox，Agent 调度消费并通过业务绑定找到助理。IM 不依赖 Agent 和业务包；消息里的业务来源只是对象引用。业务状态也可使用可见事件流向客户端推送，内部推理和工具日志不能直接进入该流。

## 3. Agent

| 包 | 导出接口 | 核心职责 |
| --- | --- | --- |
| `agent` | `Runtime`、`Observer`、`ContextBuilder`、`Checkpoints` | 独立 Loop、可见状态、有限上下文和私有检查点 |
| `agent/scheduler` | `Service`、`Control`、`Queue`、`MessageConsumer` | 单助理串行、多助理有界并发、IM 转接、恢复与确认 |
| `agent/model` | `Catalog`、`Client`、`Stream`、`Receipts` | 可用模型、授权调用、流式输出与持久计费证据 |
| `agent/tools` | `Executor` | 查询有权使用的内置工具，执行和取消 |
| `agent/operation` | `Recorder`、`Service`、`Repository` | 意图记录、结果、取消、未知结果核对和去重 |

`event.AssistantEvent` 区分用户消息引用、定时发生点、操作结果与恢复请求。校验禁止混用来源，定时任务不能携带伪造用户消息。模型输入由 Loop 构造，不接受“自动注入整个聊天历史”的参数。

模型流关闭不代表供应商已停止或未计费；调用回执须独立持久化。工具结果不会自动成为消息，发布必须经过 IM 的发送能力。检查点属于内部状态，不能通过状态查询或 SSE 泄露。

工具 `permission_list` 查询已获授权及其当前操作可用性，`permission_request` 创建待用户审批的授权请求，与 Shell 启用状态独立。返回的用户授权范围与运行环境内置工作区分别标识来源，资源根路径不表示宿主根目录。文件工具与 Shell 共用存储权限检查；后者只挂载显式选中的范围，使用与资源身份绑定的稳定逻辑路径。详细契约见 [命令执行环境](../../docs/sandbox/linux.md)。

文件能力为 `file_list`、`file_read`、`file_write`、`file_edit`，对应独立的参数结构和工具定义。省略 `resource_id` 时默认读写当前助理工作区，不需要存储/计算授权，路径为相对路径或 `/work/...`；指定 `resource_id` 时才访问授权的外部存储。工作区身份由服务端确定，并与 Shell 的 `/work` 共享。列目录与读取返回分页/范围和版本；写入要求显式覆盖选择，局部编辑要求版本与精确匹配。文件修改使用临时文件与原子替换，并在提交前重新检查权限；已知提交前拒绝记录为失败，提交结果不确定时仍待核对。

模型响应在适配器解析前持久化完整的状态码与响应体（上限 8 MiB），包括错误响应，不依赖开发工作台。成功响应的原始 assistant 消息作为 `ProviderState` 随上下文与调用缓存保存，保留未知扩展字段；回传时由对应协议适配器选择字段，并核对 Provider、模型及地址身份。保存字段不意味着自动发送字段。DeepSeek 带工具的请求若缺少历史推理数据，会在发送前返回 `model_context_incompatible`，释放本次预留预算并提示重置上下文，不伪造推理或静默删除历史。未指定 Provider 预设时，仅官方 DeepSeek HTTPS origin 可自动识别为 DeepSeek；显式预设始终优先。

## 4. 业务

| 包 | 主要入口 | 行为边界 |
| --- | --- | --- |
| `business/assistant` | 聘请、查询、修改、分组置顶、解雇、返聘、克隆 | 管理助理身份及会话绑定；克隆不复制正在执行的工作 |
| `business/task` | 创建、修订、暂停／继续／取消、验收交付 | 用户控制与执行报告分接口；完成需要真实验收证据 |
| `business/todo` | 新增、查询、修改、删除 | 指派助理不自动启动任务 |
| `business/plan` | 个人／助理安排聚合、执行关联与交接 | 同账号无环依赖；幂等执行；历史快照；显式自动交接 |
| `business/schedule` | 个人日程／助理计划维护 | 时区与错过计划处理明确，触发次数有界 |
| `business/resource` | 连接、描述、查询、检查、断开 | 安全描述与受保护的地址／凭证绑定分开 |
| `business/memory` | 本地 Markdown 共享／私有记忆维护 | 文件内容哈希版本，私有范围绑定助理；遗忘另存摘要哈希与来源，不删除原聊天 |
| `business/settings` | 偏好更新、导出、清理 | 敏感操作要求近期身份验证，新默认值不重置已有预算 |

每个业务包提供 `Service` 与所需 `Repository`；task 另外提供内部 `Execution`。仓储是可信持久化端口，不是给客户端绕过权限的接口；实现由 SQLite 包提供，业务包不导入具体存储或 HTTP 包。

## 5. 公共支撑

| 包 | 主要导出接口／类型 |
| --- | --- |
| `app` | `Application`、`Dependencies.Validate`：生命周期与组合检查 |
| `config` | `Config`、`Defaults`、`Load`、`Validate`：严格 JSON 配置与公网安全配置校验 |
| `api` | `Server`、`Services`、`RateLimiter`、`PublicError`：传输能力组合、限流与安全错误映射 |
| `auth` | `Service`、`Bootstrap`、`BrowserGuard`、`Repository`：认证、会话、令牌、二次验证、CSRF 与本机初始化 |
| `audit` | `Recorder`、`Reader`、`Repository`：无秘密材料的安全事件与受控查询 |
| `credential` | `Manager`、`Broker`、`Repository`：凭证元数据管理和仅可信适配可用的受保护访问 |
| `policy` | `Authorizer`、`Grants`、`Budget`、`Repository`：对象授权、助理权限、原子预算预留与核算 |
| `search` | `Service`、`Maintenance`、`Indexer`：权限过滤后的跨来源检索与版本化索引更新 |
| `quota` | 模型预算、网络账本、调用统计、周期计算；权限与内核限制由原模块执行 |
| `storage/sqlite` | `Database`、`Transactions`、`Repositories`、`Idempotency`、`Restorer`：事务、仓储组合、幂等与离线恢复 |
| `storage/files` | `Service`、`Objects`、`Repository`：授权文件访问、私有对象存储与元数据 |
| `platform` | `Runner`、`Process`、`Locker`、`NetworkGuard`：本机受控执行、实例锁和出站边界 |
| `platform/sandbox` | 固定 Linux Runner：Bubblewrap、seccomp、cgroup、直接授权挂载、执行专属出口；扩展 `platform.Command` 的可信挂载/网络/限额字段，其他平台失败关闭 |
| `core`、`event` | 共享值类型、统一错误及独立 Agent 输入合同 |

HTTP 组合只持有 `conversation.Client`、`agent.Observer`、`model.Catalog`、`scheduler.Control`、`search.Maintenance` 等必要能力；不持有助理发布、回合执行、索引直接写入、本机初始化或原始凭证读取能力。HTTP 使用逐项实现的显式路由，不反射整个 Go `Service` 为 API；参见 [HTTP 接入](http-api.md)。

## 6. 原子性与恢复

`Transactions.Within` 将同一事务内的仓储交给回调。回调失败、取消或提交失败均返回错误，仓储不得逃逸到异步任务，不能在事务内等待模型和外部工具。

必须原子提交的组合包括：

1. 用户消息／助理发布消息 + IM outbox + 可见事件 + 幂等回执。
2. IM outbox 确认 + 去重后的 Agent 输入；助理消息仅确认，不创建新输入。
3. 定时发生点登记 + Agent 输入；个人日程不唤醒助理。
4. 助理检查点 + 作业处理确认；恢复先核对已发出的外部操作。
5. 并发预算预留 + 操作身份；未知费用继续占用预留，不能自动释放为零。

每个可见事件流按提交顺序分配游标；每个客户端独立读取，持续推送时检查会话和权限。文件暂存与 SQL 元数据使用恢复协议协调，不声称跨文件系统原子提交。

## 7. 当前实现与验证

各模块公开接口已接入真实实现，并新增独立的存储中性 `persistence` 与底层 SQLite 适配包，避免业务依赖具体数据库。SQLite 事务通过上下文和绑定 Store 跨模块复用，嵌套写使用 savepoint；模块 JSON 记录存储在私有 keyspace 中。

认证、凭证加密、授权预算、IM、业务、Agent、持久队列、HTTP/SSE、文件及主程序均已实现。原 TDD 工厂全部绑定真实被测实现，另有跨模块业务和完整消息→Agent 显式工具读取/发布→重启测试。`make -C runtime check` 包含全量行为测试；`check-contracts` 只做编译、映射与静态检查。

契约修正包含：凭证访问支持真实 owner 检查，无需伪造 Agent 执行；模型核对参数固定为本地 receipt ID；HTTP 只持有预算查询和操作控制能力；任务唤醒事件携带 TaskID，日程事件携带版本；会话保留单调递增消息高水位；历史费用结算与新副作用授权分开。

模型适配器已读取供应商 SSE 并采集生成计时；聊天仍只展示显式发布消息，不直接转发供应商 token 流。当前没有向量搜索或通用任务验收器；自动上下文压缩已实现，按模型 token 窗口触发并保留原始历史，见 [上下文更新](../runtime/agent-context.md#token-容量与自动压缩)。外部结果未知时保持待核对，不假定零费用或安全重试。完整边界和启动方式见 [Runtime README](../../runtime/README.md)，测试记录见 [测试说明](../../runtime/tests/README.md)。

### 计划与执行关联

`GET /api/plans` 按当前所有者聚合现有 todo/schedule 身份、交接设置与执行历史，支持最多 125 天的发生点投影。`POST /api/plans/configure` 更新带版本的依赖和暂停设置；`POST /api/plans/execute` 开始助理执行或记录个人开始／完成，均校验所有者、前置条件与幂等键。助理执行完成仍需原有执行证据链。

`Task.Plan`、`PlanTitle`、`PlanInstructions`、`PlanVersion`、`OccurrenceAt` 保留来源快照。运行时启动会为旧独立任务创建一次性安排，不改变旧任务 ID。计划元数据存于 `business.plan-links` 并纳入所有者数据导出；自动交接、发生点登记、任务与事件写入处于同一存储事务。

## 通用定时触发

`internal/trigger` 提供 `Rule.Validate`、`First`、`Due`、`Between` 和 `RecordOccurrence`。计算结果为被选中的发生点及下一游标，业务适配器负责持久化游标与效果。发生点标识包含 owner、kind、ID、revision、UTC 时间；同一事务中的重试不得重复产生业务事件。公共层不依赖 Schedule、Plan、Task 或 Agent，计划与做梦的权限、生命周期和交付逻辑分别保留。

## 安装与组件维护

`internal/installation` 由 `molr-setup` 统一执行本体及组件维护。`Maintenance` 为 Runtime 提供只读状态与启动接口，通过独立 systemd 用户服务运行已安装的 setup 二进制，不在 Runtime 进程内安装。全程实例锁由安装器持有；新版安装器通过受控文件描述符接手同一把锁，普通子进程不继承锁。

组件清单固定版本、来源、摘要和体积限制。暂存目录完成校验后提交到不可变安装目录，组件 receipt 独立于本体 receipt。文件操作拒绝链接和路径逃逸，组件及维护目录不能成为助理的普通存储资源。仅受管浏览器计划可以生成只读程序资产挂载；最终启动仍经过原计算、网络和沙盒授权。
