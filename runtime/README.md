# Molr Runtime

Go 单进程 self-host server，按 **IM / Agent / 业务** 组织，功能固定、统一发布。已经实现 SQLite 事务和持久队列、认证授权、业务模块、独立 Agent Loop、HTTP/SSE 与命令行入口；全部原 TDD 工厂已接入真实实现。

聊天只保存交互历史。真实用户消息经 IM outbox 转为 Agent 输入；定时计划生成独立事件，不伪造用户消息。Agent 首次获得事件引用及自然语言提醒，提示词要求响应前通过 `conversation_read` 读取当前全部未读消息，必要时翻页，再综合处理各条请求；不只读取触发消息或最新一条。通过 `conversation_publish` 发布文本、卡片或附件。基础提示词明确要求所有面向用户的回答、提问、进度、完成通知和错误说明都必须调用 `conversation_publish`；直接输出到 `content`（包括最终回复）对用户不可见，不会自动成为聊天消息。结束需要回复的轮次前应发送并检查发布结果；成功后不重复发送，无需通知的事件可静默结束。

Full Access 可作为计算权限按需开启，同一节点保留普通隔离路径；部署配置、授权操作和语义见 [Full Access](../docs/deployment/sandbox.md#按需授予-full-access)。

用户消息轮次若未尝试 `conversation_publish` 就正常结束，Loop 会追加一次私有提醒，询问是否忘记发送；模型可继续选择静默结束。每轮提醒次数随检查点持久化，继续执行遵守原有步数与预算限制。详见[上下文更新机制](../dev-docs/runtime/AGENT_CONTEXT.md)。

## 完整本地发行包

在仓库根目录执行 `npm run build:release`，静态构建内嵌 WebUI 的 Runtime，并生成架构对应的安装包、校验文件和本地下载清单。使用 `scripts/install.sh --archive …` 安装；支持可信局域网 HTTP、自动 bootstrap、Provider 自动发现和默认 Molr 助理。`molr-server version` 查看版本，`doctor -config …` 检查已认证沙盒能力，`access -config …` 生成一次性登录链接。详细参数、凭证恢复和本地官网分发见[部署指南](../docs/deployment/README.md)。发行包不附带 BusyBox；安装器通过系统依赖检查和可选 apt 安装提供静态 BusyBox。MIT 与第三方声明随包发布并安装到前缀根目录。本次未接入 GitHub 发布流程。

## 构建与维护入口

Go 版本以 [.go-version](.go-version) 为准，依赖由 `go.mod` / `go.sum` 锁定，SQLite 使用纯 Go 驱动。运行用二进制必须显式设置 `CGO_ENABLED=0` 并检查最终静态链接与 `0755` 权限；初始化、HTTPS、systemd 启动、备份恢复和升级统一见 [部署指南](../docs/deployment/README.md)。

```bash
# 在 runtime/ 中构建开发产物
mkdir -p ../.cache/build
CGO_ENABLED=0 GOTOOLCHAIN="go$(cat .go-version)" go build -o ../.cache/build/molr-server ./cmd/molr-server
chmod 0755 ../.cache/build/molr-server
```

WebUI 开发联调见 [应用 README](../apps/webui/README.md)。Dockerfile 提供精简 Runtime 构建，不包含完整 Linux Shell 自动环境。

## 功能模块

- `auth / credential / policy / audit`：Argon2id 密码、TOTP、一次性恢复码、可撤销 Cookie/Token、CSRF、敏感操作再次验证、AES-GCM 凭证加密、授权和整数预算、审计。
- `quota`：模型预算、调用额度统计、执行端 SQLite 网络账本、日历周期与统一用量口径；内核资源限制仍由沙盒执行。
- `im`：会话、用户发送与助理发布、接受/已读、反馈、附件引用、持久 outbox、SSE 游标与实时撤权。
- `business`：聘请、解雇、返聘、克隆、分组置顶；任务与修订；待办；个人日程和助理计划；资源；本地 Markdown 共享/私有记忆（`/global` 与 `/work`，详见 [文件记忆](../dev-docs/runtime/DREAM_MEMORY.md)）；设置、数据导出和清理。
- `agent`：持久队列、每助理串行 Loop、独立私有上下文、模型/工具操作回执、预算预留和结算、唯一当前执行检查点在恢复边界持续保存、轮次结束与作业确认原子提交、跨事件接续及未知结果待核对。
- `search / storage / platform`：授权后多来源检索、可重建投影、真实 SQLite 和嵌套事务、文件暂存/恢复、进程树回收、独占实例锁、解析后 IP 直拨和 SSRF 约束。
- `api / app`：显式 HTTP 路由、大小/超时/速率限制、Host/Origin/TLS/代理校验、SSE、完整装配和退出回收。

公开 Go 接口是单体内部模块的能力边界，不等于 HTTP 接口；HTTP 不持有 Agent 执行、原始凭证读取或任意仓储写入能力。详细结构见[模块设计](../dev-docs/runtime/ARCHITECTURE.md)，接口见[接口合同](../dev-docs/runtime/INTERFACES.md)。

## 固定适配与当前边界

当前提供 `openai-compatible` 模型提供商、本机目录与沙盒适配器，以及独立 Linux Sandbox 的远程存储、计算和网络适配器。每个模型资源表示一个 provider 连接，WebUI 通过 `SaveProvider` 将连接与 API Key 原子保存，支持读取连接元数据、编辑地址和更换 Key；`DeleteProvider` 原子删除连接及独占凭证、撤销权限并清理默认模型，保留助理与聊天历史；`Binding.Models` 统一保存手动配置和自动发现的模型。`POST /api/resources/test-connection` 可在保存前通过模型列表请求测试连通性与凭证，不保存资源、模型或 API Key。连接时无需填写模型；`POST /api/resources/refresh-models` 从固定同源 `/models` 接口刷新列表，模型 ID 独立于资源 ID 并保持稳定。`POST /api/resources/configure-model` 可在连接后配置单个模型；手动配置按模型名覆盖发现结果，刷新不会覆盖手动价格与能力参数。自动发现采用默认上下文/工具参数，优先匹配内置计价；未覆盖的价格标记为未知，不允许作为免费调用计费。助理授权同时绑定 Provider 资源与所选模型的 `Scope.ModelID`，不覆盖该 Provider 的全部模型；付费模型需设置预算。发现请求通过可信凭证代理和网络 guard，禁止重定向与部署配置未允许的私网地址；API Key 加密存储；敏感操作验证默认关闭，开启后保存／更换 Key 与导出才要求再次验证，OAuth 仅预留前端能力模型，尚未接入。

模型适配器请求供应商 SSE 流并采集首 token 与生成计时，再转换成内部 `model.Stream`，同时兼容完整 JSON 响应。`file_read` 已支持按需图片、音频与视频抽帧输入，OpenAI-compatible 和 DeepSeek 适配器均接通图片；不支持的类型仅返回元信息。能力配置、解码依赖与限制见[多模态文件读取](../dev-docs/runtime/MULTIMODAL_FILES.md)。外部结果未知时保留操作记录，不会猜测成功或自动重发。本地调用退出时自动结束费用预留与并发占用；缺少供应商证据时保留未知费用，后续可信回执仍可补结算。

模型调用按逻辑步骤记录独立尝试。临时 429、明确拒绝且未计费的过载错误、可确认未发送的连接失败允许自动重试；额度耗尽、认证和请求错误直接失败。普通 5xx、发送后断线、超时或流中断不能证明上游未执行，保留未知操作并结束预算占用，进入 `needs_review`。已知失败以 `failed` 结束调度任务。

`execution.model_retry` 配置默认如下；`max_attempts` 包含首次请求，设为 `1` 可禁用自动重试。等待遵守 `Retry-After`（秒数或 HTTP 日期），否则使用带随机抖动的指数退避；不会为了满足本地等待上限而提前请求。次数、累计等待、绝对截止时间与输入快照均持久化，重启不重置。等待释放全局 worker，同时保留该助理的执行顺序；停止、重置、加急消息以及授权变更在重发前生效。工具不会因模型重试而重复执行。

```json
{"execution":{"model_retry":{"max_attempts":4,"base_delay":"2s","max_delay":"30s","max_wait":"2m","deadline":"10m"}}}
```

每个尝试绑定独立操作、回执和预算预留。成功按回执结算；已证实未计费记为 released，未知费用记为 unknown，两者均结束本地预留占用，unknown 仍允许迟到回执补结算。开发工作台按同一执行编号展示各次尝试，包含请求/响应、尝试序号、状态、失败原因与下次重试时间；等待中的执行记录为 `retrying`。诊断采集开关不影响重试正确性。历史操作保留原 ID，新尝试使用独立 ID，不改写旧记录。

目录工具使用 Linux `openat2` 强制禁止符号链接和越界路径，拒绝特殊文件、硬链接及 Runtime 自身目录。其他系统或不支持该能力的内核拒绝目录工具操作。通用可信进程接口保留白名单执行；Linux Agent Shell 使用独立的强制沙盒实现，不能回退到宿主进程。`sandbox.enabled` 默认开启。启动时自动准备基础环境、工作目录与执行资源，自动登记本机沙盒；用户不需要填写底层路径或准备独立存储卷。实际隔离探针通过且已授予计算权限时才向模型开放 `shell_execute`；系统不支持时明确显示不可用，可通过显式配置 `false` 关闭。

Linux Shell 已接入 Bubblewrap、seccomp、cgroup v2、直接目录挂载和受控网络出口。部署、配置与真实隔离测试见 [Sandbox 使用说明](../docs/deployment/sandbox.md)，架构与未来 Windows/macOS 边界见 [设计](../dev-docs/runtime/SANDBOX.md)。

任务交付需要可信检查操作的证据，模型声称完成不能直接验收。当前内置工具不提供通用任务验收器。搜索在读取前应用来源、助理和归档范围，按有界批次扫描选中的授权源记录；消息按会话及序号索引遍历。持久化投影可重建，但还没有 FTS/向量检索优化。私有上下文按模型 token 窗口管理，未声明且未手动设置时兜底 256K；默认 80% 自动压缩并保留原始历史。单轮执行默认及引擎最大步数均为 999。模型请求默认超时 10 分钟（含等待响应头和读取响应），Shell 前台最多等待 60 秒，未结束转为后台；命令可用 `timeout_seconds` 设置运行截止时间，实际运行还受计算授权及用户组时长限额约束，不限时须显式授权。

模型最大输出长度按手动 `OutputLimit` → 服务商 `ProviderOutputLimit` → 32K（32,768 tokens）解析，默认值不写入手动设置。模型发现读取 `max_output_tokens`、`max_completion_tokens` 或 `top_provider.max_completion_tokens`，刷新保留手动覆盖；清空覆盖恢复服务商值或默认值。WebUI 模型配置可修改，列表显示生效值与来源。每次请求按模型输出上限发送并预留预算，不依据估算剩余上下文拒绝请求或缩小输出额度；上下文仍在默认 80% 阈值自动压缩，摘要请求另有精简目标且不超过模型上限。自动上下文的版本刷新、来源范围及限制见[上下文更新机制](../dev-docs/runtime/AGENT_CONTEXT.md)。文件导出上限 20 MiB，超限明确失败。

助手每次模型请求前读取当前账号的时区、语言和币种，以 `user_preferences` 自动上下文注入；内容变化时替换旧快照，时间流逝和无关设置修改不会触发更新。时区用于相对日期和新日程，语言用于默认回复语言，币种用于默认金额展示；用户明确要求优先，已有日程时区、原始金额币种和预算计价保持各自含义。缺失时区标记为未知，语言/币种采用产品默认值并标明来源。时间沿用已有事件字段，不注入持续变化的当前时间。

日程支持 RRULE、时区和夏令时；正常触发有一分钟容差。漏执行采用跳过、合并到最新一次，或补最近 N 次（最多 100）；发生点和 Agent 输入一起提交。个人日程不创建 Agent 输入。

## 验证

```bash
make -C runtime check                 # 在仓库根目录执行
make -C runtime test TEST_FLAGS='-race -p 4'
```

测试使用临时 SQLite、文件和本地 HTTP/TLS 模型模拟服务；需要允许创建本机 socket，不需要真实模型 API Key。包括原模块 TDD、真实认证与业务组合、完整消息→工具→回复→重启流程、取消后的费用、文件/网络边界和备份恢复。接口映射检查只证明公开入口都有对应测试，不代表分支覆盖率或独立安全审计。参见[测试说明](tests/README.md)和[安全设计](../dev-docs/runtime/SECURITY.md)。

需要验证真实模型行为或归纳质量时，按[真实模型测试约定](../dev-docs/DEVELOPMENT.md#真实模型测试)，优先使用主环境中已经配置好的本地模型，并从任务 worktree 的独立 Runtime 执行测试。

真实 WebUI 的启动与生产反向代理配置见 [WebUI README](../apps/webui/README.md)。HTTP 通用限流使用 `http.requests_per_minute`（默认 600），登录限流仍独立使用 `auth.login_attempts_per_minute`。

### 出站网络策略

私网访问权限仅由部署者在 `server.json` 中配置，WebUI 与资源 API 无权修改。默认 `network.allowed_private_ips` 为 `["127.0.0.1"]`，允许 Runtime 本机的 IPv4 回环地址，其他私网、共享地址及 IPv6 回环默认禁止。部署者可用空数组禁用本机访问，或显式增加需要的地址：

```json
{ "network": { "allowed_private_ips": ["127.0.0.1", "::1"] } }
```

列表仅接受具体 IP，配置修改后重启生效。地址指 Runtime 所在设备的网络，不是浏览器所在设备。模型发现与推理共用此限制；域名在拨号时解析并逐个校验地址，公共地址要求 HTTPS，元数据与链路本地地址始终禁止。允许某个私网 IP 会允许该 IP 上的服务端口，部署者应仅放行需要的服务设备。资源授权仍限制助理能调用哪些 provider，不会扩大部署级网络权限。

### 内置模型计价

DeepSeek 规则在 [`internal/pricing/deepseek.json`](internal/pricing/deepseek.json) 中维护，启动时默认加载编译进服务的配置。也可以在服务配置中指定 `"pricing": {"rules_file": "/etc/molr/pricing.json"}`，使用外部文件完整替换内置规则。外部配置缺失或无效会阻止启动，避免错误计费。

当前仅覆盖 DeepSeek 官方 API 的 `deepseek-flash`（含配置中列出的两个别名）和 `deepseek-v4-pro`，按官方 USD 价格区分高峰、非高峰及缓存命中。来源和核对版本保存在配置中：[DeepSeek 官方价格](https://api-docs.deepseek.com/quick_start/pricing/)。规则使用整数微美元／百万 tokens，`150000` 表示 $0.15／百万 tokens。历史、未知型号不会按名称前缀猜价；自定义 Provider 和非官方代理地址也不会套用官方价格。

已保存模型无需重新配置即可匹配规则。模型手动价格优先；将 `Model.Pricing` 设为 `null` 可恢复自动计价。未知价格仍会阻止调用，显式填写三项 `0` 才表示免费。预算预留采用高峰价格上界，调用开始时确定本次价格，结算按实际 tokens 向上取整到微美元；账单凭据保留价格及规则版本。DeepSeek 的 `prompt_cache_hit_tokens` 用于缓存命中计费。

规则中的高峰时间以 UTC 计算，中国节假日按 Asia/Shanghai 日期排除。目前日历覆盖 2026 年，来源见配置；其他年份按普通工作日规则保守计算，并通过 `PricingInfo.CalendarKnown=false` 标记，需要更新日历后才能准确应用节假日折扣。

已提供 `Bundle.Pricing.Reload(io.Reader)` 供未来服务端更新流程调用：先校验完整配置，再原子替换；失败保留旧规则，并发调用及已有计价快照不受影响。价格上涨时，发起模型请求前会重新核验已有预算预留。当前没有文件监听器或公网更新接口，修改外部文件后需重启服务；未来更新服务可直接调用 Reload，无需改动 Agent 计费流程。


### 助理预算

聊天与计划执行共享所属助理预算，普通成员还受用户组的累计额度限制。用户操作、周期和未结算费用规则见[助理预算](../docs/resources-and-permissions.md#助理预算)，请求字段与用量响应见 [HTTP API](../dev-docs/runtime/HTTP_API.md)。


### 币种与参考汇率

「用户设置 → 常规」的币种偏好只控制新输入默认值与显示。金额输入旁可选择人民币／美元，预算、手动模型价格和扣费凭据保留原始数值及原币种。费用展示在异币种时同时列出换算值和原值；切换偏好不会改写存储的数据。模型原生币种与预算币种不同也可执行，预算校验使用完整汇率快照并记录版本，整数换算向上取整到微货币单位。

参考汇率存于 [`internal/pricing/exchange.json`](internal/pricing/exchange.json)，初始数据来自 [欧洲央行 2026-09-24 参考汇率](https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml) 的 EUR/USD 与 EUR/CNY 比值。它是带日期的参考配置，不是实时市场报价。部署可通过 `pricing.exchange_rates_file` 指定绝对路径完整覆盖；错误配置阻止启动。`Bundle.Exchange.Reload` 原子更新有效规则，失败保留旧版本，不修改历史原价或已记录的校验快照。目前无自动文件监听或网络抓取，后续服务端更新器可调用 Reload。

### 开发工作台

通过部署配置 `debug.enabled` 开启所有账号的执行采集，默认关闭。WebUI 常规设置「显示开发工作台」只控制入口显示，隐藏入口仍采集，显示后可查看保留范围内的历史。工作台展示真实 Agent 时间线、调用上下文、请求／响应、工具结果与费用，不通过普通聊天事件流公开私有执行数据。入口、采集限制和 API 见 [开发工作台](../dev-docs/runtime/WORKBENCH.md)。

### 助理默认工具

内置工具包含资料更新（`profile_update`）、资源权限、会话与附件、文件（含 `file_search`）、计划及可用时的 Shell。用户组和助理授权决定实际可用项，插件还可增加获授权的扩展工具。未注册旧 `task_*`、`memory_*` 或通用 `search`；原有业务数据和用户界面接口保留。

- `conversation_read` 默认读取最近一页，页内按消息先后正序返回（最新消息在最后），接受 `cursor` 继续向前翻页，或用事件的 `MessageID` 作为 `message_id` 精确读取触发消息；返回附件元数据。
- `conversation_attachment` 将本会话附件原样保存到助理工作区的新文件，不覆盖已有文件，不创建目录。`conversation_publish.attachments` 接受已授权的 `file_id`，或工作区 `path`（可选 `name`、`media_type`），支持二进制附件；受文件大小与配额限制。附件不能跨助理读取。
- `file_list/read/write/edit` 保持原有语义，不增加目录创建能力。`permission_list` 查看已获授权及当前可用状态，`permission_request` 发起用户审批并保存等待进度，`shell_execute` 仍仅在沙盒可用且有计算授权时开放。
- `plan_list`、`plan_read` 和 `plan_manage` 只管理分配给当前助理的计划。计划返回不透明 `PlanID` 和统一 `Version`。`plan_manage` 的 action 为 `create/update/delete/execute`；后三者使用读取到的 `Version` 作为 `expected_version`。`update` 原子更新提供的字段，可同时修改内容、时间、依赖、交接和暂停；未提供的字段保持原值，可选日期用 null 清空，文本用空字符串清空，依赖用空数组清空。底层版本不暴露给模型。定时计划需要 RFC3339 时间和 IANA 时区。`plan_read` 可用当前事件 `TaskID` 作为 `execution_id` 读取关联计划。删除已有执行记录的计划会被拒绝，改用暂停。启动执行只表示排队，不声明完成。
- 所有已尝试的工具调用都会返回具体结果；失败结果包含错误码、安全诊断、操作状态及是否可能已产生副作用。拒绝访问、工具调用取消、超时和结果未知均不直接中断 Agent Loop，模型可以更正请求、改用其他工具或向用户说明。未知副作用会明确提示先检查或核对再重复操作。只有 Runtime 自身的停止／取消、身份失效、预算和步数等执行约束决定是否终止本轮。

### Turn metrics and provider timing

Conversation message metrics are exposed through the authorized Agent observer. The message's trusted execution identity associates every publication with its logical turn; older messages resolve through the publication operation. Metrics are calculated from lifecycle evidence, operations and model receipts on every request. The backend does not cache aggregates. Jobs and operations are selected through owner/assistant/execution indexes; model receipts use the existing operation lookup index. These reads share one transaction snapshot and do not load unrelated execution history. `Finalized` requires a terminal job, a finished Runtime attempt, settled operations, known usage and known charges. Missing historical timing remains null.

OpenAI-compatible and DeepSeek chat requests use SSE with `stream_options.include_usage`; JSON responses remain supported. The transport records request duration, first meaningful generation fragment and first-to-last generation duration before consumption by the Loop. One-fragment responses have no decode-speed sample. Raw HTTP/SSE responses are not stored in the database. Optional rotating file diagnostics are disabled by default (`diagnostics.model_responses`). Parsed messages live in the context store; the workbench projects output details on demand. A result missing from the checkpoint after interruption requires a new model attempt, with separate billing evidence. Stream termination and usage handling follow the [official streaming guide](https://developers.openai.com/api/docs/guides/streaming-responses) and [usage example](https://developers.openai.com/cookbook/examples/how_to_stream_completions). This does not change the rule that only `conversation_publish` sends visible messages.

## 本机与远程沙盒

Runtime 内嵌本机 sandbox_manager；可另行构建 `./cmd/molr-sandbox` 在其他 Linux 机器上提供存储、计算和网络。WebUI 资源页分为模型、工具、沙盒，助理可设置默认沙盒。部署配置、独立服务安全边界和当前限制见 [远程沙盒部署](../docs/deployment/remote-sandbox.md)。

### 实时连接鉴权回归

`go test ./internal/auth ./internal/im/realtime ./internal/api -run 'TestPrincipalWatch|TestAuthorization|TestStreamIdle|TestTDDServerSSE'` 覆盖真实认证与 SQLite 下的 1／10／50 条空闲连接、HTTP 层无重复身份轮询、撤销通知、账号隔离、事务回滚、截止时间续期、并发提交不丢唤醒，以及撤权后关闭连接。计时测试只反映运行测试时的环境；不作为部署延迟保证。

### 后台查询性能回归

`go test ./internal/business/schedule ./internal/business/dream ./internal/business/plan ./internal/persistence/sqlitestore` 覆盖到期边界、跨时区、过期批处理、自动交接去重和提醒消除；存储观测与查询计划断言检查无关历史不会被全量加载。`go test ./internal/business/schedule -run '^$' -bench BenchmarkDueHistory -benchmem` 对比 1 千和 10 万条无关日程下的全量扫描与当前索引查询。具体耗时依赖机器与负载，不作为固定性能承诺。

### 操作幂等查询性能回归

模型和工具操作的幂等检查通过账号与幂等键的复合索引精确读取，最多返回一条操作；创建事务仍负责重复请求去重和参数冲突校验。`go test ./internal/agent/operation ./internal/persistence/sqlitestore` 覆盖账号隔离、并发去重、无关历史读取数量及索引查询计划。`go test ./internal/agent/operation -run '^$' -bench BenchmarkIdempotencyLookupHistory -benchmem` 对比 1 千、1 万和 10 万条历史下，原全量扫描与索引查询的命中、未命中开销；基准仅度量幂等查询，不代表模型推理或完整执行耗时。

### 本轮信息查询性能回归

`go test ./internal/agent ./internal/persistence/sqlitestore` 覆盖本轮统计的动态结算、缺失回执、账号／助理／执行隔离和索引查询计划，存储观测断言无关历史不被全量读取。`go test ./internal/agent -run '^$' -bench '^BenchmarkTurnMetricsHistory$' -benchmem` 在固定本轮调用数量下，对比 1 千、1 万和 10 万条无关执行历史的完整统计查询耗时及内存分配；基准不包含模型推理或浏览器渲染。

### 持久化查询性能回归

认证凭据按摘要查询；执行身份和结算按所属账号、助理及执行 ID 查询；授权快照排除已撤销和过期记录。普通业务列表、认证列表、审计、操作历史及调试运行列表在数据库中分页。附件、依赖和梦境来源使用事务内维护的数组成员索引。保留的全范围遍历及设计边界见[架构说明](../dev-docs/runtime/ARCHITECTURE.md#持久化查询与全量读取边界)。

`go test ./internal/persistence/... ./internal/auth ./internal/app ./tests/architecture ./tests/contractcoverage` 包含查询计划、读取量、游标隔离、数组索引事务一致性和全量读取例外检查。`go test ./internal/auth -run '^$' -bench '^BenchmarkSessionLookupHistory$' -benchmem` 比较 1 千、1 万和 10 万条会话历史下的全量读取与摘要索引查询；测量仅包含存储查询，不代表端到端模型响应时间。

### 调用配额查询性能回归

`go test ./internal/quota ./internal/plugins ./internal/app ./internal/agent/tools ./internal/persistence/...` 覆盖工具和 HTTP 连接的周期计数、跨周期占用、账号／资源隔离、当前操作排除及准入事务回滚；覆盖索引和读取量断言检查统计不解码历史调用。`go test ./internal/quota -run '^$' -bench '^BenchmarkCallQuotaHistory$' -benchmem` 比较 100、1 千和 1 万条本周期历史下的配额计算开销，不包含模型或外部 HTTP 延迟。

### 上下文大小检查性能回归

`go test ./internal/agent ./internal/agent/model` 覆盖上下文 JSON 大小边界、工具参数、服务商状态、多模态元数据、256 MiB 防护、压缩和执行恢复。`go test ./internal/agent -run '^$' -fuzz '^FuzzContextSizeMatchesJSON$' -fuzztime=10s` 对照完整 JSON 编码验证边界；`go test ./internal/agent -run '^$' -bench '^BenchmarkContextSizeCheck$' -benchmem` 测量 1、8、32 MiB 多条输入的检查开销，不包含模型网络调用或检查点持久化。

### 文件搜索 glob 性能回归

`go test ./internal/textfiles` 覆盖 glob 匹配、排除目录、授权及扫描边界；`go test ./internal/app -run 'TestFileSearchWorkspaceGlobalAndExternalScope|TestRemoteSandboxStorageRoutingAuthorizationAndContext'` 验证本地和远程工具接线。`go test ./internal/textfiles -run '^$' -fuzz '^FuzzCompiledGlobMatchesRecursive$' -fuzztime=15s` 对照原递归实现；`go test ./internal/textfiles -run '^$' -bench '^BenchmarkFileSearchGlobFiltering$' -benchmem` 测量 2,000／8,000 个文件、12 条模式且路径均未命中的遍历过滤开销，不包含文件正文读取。

### 附件上传并发

`go test ./internal/storage/files` 覆盖慢上传期间无关文件提交与数据库读取、同对象串行操作、等待取消、并发配额竞争、幂等去重和失败暂存清理。`go test -race ./internal/storage/files -run 'Test(SlowUpload|ObjectWaiters|SameObject|ConcurrentUploads)'` 检查并发路径；竞态检测只用于测试程序，Runtime 验收仍使用 `CGO_ENABLED=0` 静态构建。

### 记忆文件分页

`go test ./internal/business/memory ./internal/business/dream ./internal/search` 验证分页排序与筛选、游标账号隔离、前页删除与外部编辑、读取范围和文件安全限制，并检查搜索／做梦连续读页的累计正文上限。`go test ./internal/business/memory -run '^$' -bench '^BenchmarkMemoryFirstPage$' -benchmem` 测量 100 和 1000 个文件中读取首页 50 条的耗时与分配；不包含模型调用或页面渲染。

### 模型等待期间的输入唤醒

`go test ./internal/agent ./internal/agent/scheduler ./internal/event ./internal/persistence/sqlitestore` 覆盖提交后中断、无事件时不重复读取、账号／助理隔离、查询与等待之间的提交、未来输入到期、事务回滚及暂时读取失败重试。`npm run test:webui -- expedite.spec.ts` 验证桌面／移动端加急、跨标签已读更新和停止后恢复；测试模型先完成出生事件，再阻塞待加急的用户请求。

### 模型定位性能回归

`go test ./internal/business/resource ./internal/app ./internal/persistence/...` 覆盖模型 ID 稳定性、提供商生命周期、索引回滚／重启、共享目录与用户组撤销；读取量测试断言只加载目标配置，查询计划检查成员索引命中。`go test ./internal/app -run '^$' -bench '^BenchmarkModelResolutionProviders$' -benchmem` 比较 10、100 和 1,000 个提供商下定位末尾模型的开销，不包含模型网络响应。

### 模型流式组装性能回归

Chat Completions 的文本与工具字段使用增量缓冲组装，避免长输出的反复字符串复制。`go test ./internal/agent/model -run '^$' -bench BenchmarkChatStreamAssembly -benchmem` 覆盖正文、推理和工具参数的片段数增长；语义和证据校验见 [模型适配](../dev-docs/runtime/MODEL_ADAPTERS.md#流式响应组装)。

### 通知查询性能回归

`go test ./internal/notification ./internal/persistence/...` 覆盖筛选、账号隔离、纳秒及跨时区排序、分页完整性、全局计数和事务回滚；读取量及查询计划断言检查已解决历史不会被全量加载。`go test ./internal/notification -run '^$' -bench '^BenchmarkActiveNotificationsHistory$' -benchmem` 在固定 5 条活动通知下比较 100、1 千和 1 万条已解决历史的列表查询开销，测量不包含浏览器与网络。

### 搜索读取范围回归

`go test ./internal/search ./internal/persistence/sqlitestore` 检查来源与助理过滤在读取阶段生效、归档与账号隔离、分页排序、授权撤销和查询计划。`go test ./internal/search -run '^$' -bench BenchmarkSearchAssistantHistory -benchmem` 比较 1 千与 1 万条无关消息历史下的助理名称搜索；无关消息不会被加载，具体耗时取决于运行环境。

### 工作台保留检查

`go test ./internal/debugtrace ./internal/persistence/... ./tests/contractcoverage` 覆盖保留数量下限、运行中记录保护、容量上限、账号和梦境隔离、精确排序、统计事务语义及分批读取上限。`go test ./internal/debugtrace -run '^$' -bench '^BenchmarkTraceRetentionCheckHistory$' -benchmem` 测量 100、1000 和 10000 条历史下无须删除记录的保留检查；不包含模型推理和页面渲染。`npm run test:webui -- workbench-retention.spec.ts` 验证设置持久化、轨迹清理提示与成员权限。

### 诊断脱敏性能回归

`go test ./internal/debugtrace` 覆盖采集隔离、凭证移除、嵌套 JSON、并发采集和负载限制；保留优化前实现作为测试对照，逐字节比较脱敏内容与状态。`go test ./internal/debugtrace -run '^$' -fuzz '^FuzzSanitizeMatchesReference$' -fuzztime=10s` 扩展边界输入；`go test ./internal/debugtrace -run '^$' -bench '^BenchmarkTraceSanitization$' -benchmem` 测量 50、500 和 2,000 条合成模型输入的脱敏开销，不包含持久化和模型网络调用。
