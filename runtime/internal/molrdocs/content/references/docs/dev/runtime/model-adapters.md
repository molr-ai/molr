# 模型后端适配

Runtime 仍是单体 Go 服务。`internal/agent/model` 使用固定的内置协议适配器，不提供动态注册或插件加载。

## 职责

- Agent Loop 使用统一的正文、工具调用、用量和 `ProviderState`，不解析后端字段。
- 模型客户端负责权限、凭证、网络访问、幂等记录和费用结算。
- 协议适配器负责请求编码、响应解析，以及后端专属状态的保存与回传。

目前接入七种协议：

| 协议 | 请求与状态 |
| --- | --- |
| `anthropic-messages` | 原生 Messages，`x-api-key` 认证，内容块、工具结果、SSE 与签名状态回传 |
| `gemini-chat-completions` | Google 官方兼容接口，保留每个工具调用上的思考签名，不发送 `store` |
| `minimax-chat-completions` | 兼容推理协议，加 `reasoning_split: true` 分离思考与正文 |
| `reasoning-chat-completions` | 火山方舟、硅基流动、Z.AI；使用 `max_tokens`，保留同源 `reasoning_content`，不发送 `store` |
| `openai-chat-completions` | 通用 Chat Completions，使用 `max_completion_tokens`、`store: false`，不采纳 DeepSeek 扩展字段 |
| `openrouter-chat-completions` | 使用 `max_tokens`；保留 `reasoning`／`reasoning_details`，不发送 `store` |
| `deepseek-chat-completions` | 使用 `max_tokens`，不发送 OpenAI 的 `store` 参数；处理 `reasoning_content` 与 DeepSeek 缓存用量 |

应用层根据用户选中的 Provider 预设解析协议，不根据模型名称或服务地址猜测。当前资源中该预设字段仍名为 `PricingProvider`；预设显式选择上表中的协议。设置手动价格不改变协议选择。自定义预设不是任意后端协议的自动检测器。

所有模型推理请求（包括做梦、上下文压缩和重试）、模型列表查询、连接测试及本地模型发现统一携带 `User-Agent: molr/<版本号>`。版本来自 `internal/buildinfo.Version`，与 `molr version` 一致；发行构建通过 `-ldflags -X` 注入，未注入版本的开发构建使用 `molr/development`。

## 模型定位与配置一致性

模型 ID 由提供商资源 ID 与模型名称稳定计算。私有绑定记录保存派生的 `ModelIDs`，通过数组成员索引直接定位所属提供商；创建、发现刷新、手动配置和连接更新均在绑定写入事务中重算，删除绑定时一并删除索引项。派生字段由资源模块维护。

每次模型解析在同一事务快照中读取目标绑定、资源状态和当前用户组引用，再解析当前计价与协议。模型客户端继续执行模型范围授权、预算和网络检查。查询读取量只包含目标提供商，不随无关提供商数量增加。

## 云端兼容服务预设

百炼（`bailian`）、智谱（`zhipu`）和 Moonshot（`moonshot`）与 WebUI 预设一致，保存、编辑及模型发现保留各自的提供商身份，使用 `openai-chat-completions` 协议与 API Key 认证。这些预设不代表接入厂商原生协议；未知价格仍需配置，不自动视为免费。

## Anthropic 与 Gemini

Anthropic 将首条系统指令置于顶层 `system`，将连续同角色内容合并为原生消息。批量工具结果先于补充图片输入。签名思考块保留原顺序，回传前核对来源与上下文中的正文、工具调用一致性。SSE 必须收到完整块、结束原因、输出用量和 `message_stop`；错误或中断不能记为成功。输入总量为普通输入加缓存读取；不主动请求缓存写入，非零缓存写入用量保留为待核对费用。目录按 `after_id` 分页，所有页共限 2 MiB、500 项，读取声明的 `max_input_tokens` 与 `max_tokens`。

Gemini 使用官方 Chat Completions 兼容 API。流式组装保留每个工具调用的 `extra_content.google.thought_signature`，回传时同时匹配后端来源、调用 ID、函数名和参数；不回传任意扩展。二者专属状态参与上下文估算，仍不进入聊天正文。协议依据见 [Anthropic](https://platform.claude.com/docs/en/api/messages/create) 与 [Gemini 签名](https://ai.google.dev/gemini-api/docs/generate-content/thought-signatures)。

MiniMax、火山方舟、硅基流动与 Z.AI 共用兼容推理适配器，只回传同源 `reasoning_content`，不强制全部历史消息具有该字段，也不主动设置厂商思考开关。硅基流动目录添加 `sub_type=chat` 过滤。新增预设不附带自动价格。

LM Studio 使用通用 Chat Completions，默认本机扫描增加 1234 端口；只读 `/api/v0/models` 的类型、加载状态和兼容格式用于识别，不执行模型加载。保存后的模型发现沿用 `/v1/models`。

## OpenRouter

`openrouter` 预设显式选择专用协议，仍复用凭证代理、网络 guard 和预算账本。`/api/v1/models` 按 `architecture.output_modalities` 过滤文本输出模型，读取 `context_length`、`top_provider.max_completion_tokens`、`architecture.input_modalities` 和 `supported_parameters`；不根据名称猜测能力。该预设目录限制为 8 MiB、4,096 项，普通兼容目录继续使用 2 MiB、500 项限制。

`pricing` 中美元／token 十进制字符串以有理数转换为微美元／百万 token，向上取整避免低估；缺失缓存价格使用普通输入费率。非零额外收费项无法在当前账本完整表达时不自动设置价格。发现报价存入 `ProviderPricing`，用户覆盖仍保存在 `Pricing`；刷新和模型配置都保留两者边界。有效自动价格以 `PricingInfo.Source=provider` 返回。

适配器只回传同源状态中的 `reasoning_details`，无结构化块时回传 `reasoning`；原消息中的其他顶层扩展不进入请求。流式组装合并连续且身份一致的文本／摘要与签名片段，独立保留加密块和类型切换顺序，不单凭可能重复的 index 合并。回传状态纳入上下文估算，仍不进入聊天记录。

协议依据：[OpenRouter 快速开始](https://openrouter.ai/docs/quickstart)、[模型目录](https://openrouter.ai/docs/api/api-reference/models/list-all-models-and-their-properties)、[推理状态](https://openrouter.ai/docs/guides/best-practices/reasoning-tokens)。协议、目录刷新及 WebUI 回归使用人工模型服务，在线模型效果需另行验证。

## 本地推理服务预设

vLLM（`vllm`）和 SGLang（`sglang`）使用现有 `openai-chat-completions` 适配器，支持无认证或 Bearer API Key。预设、保存校验和协议选择均保留其独立身份；不会根据部署的模型名切换为 DeepSeek 协议，也不回传后端专属推理状态。

发现模型沿用 `/v1/models`，优先采用合法的 `context_window`，其次采用 vLLM／SGLang 的 `max_model_len`（256–2,000,000）；无合法值时继续使用现有默认值，刷新不覆盖用户配置。网络白名单、凭证与预算规则保持一致，不自动填入零价格。

协议依据：[vLLM OpenAI 兼容服务](https://docs.vllm.ai/en/latest/serving/openai_compatible_server/)、[SGLang OpenAI 兼容 API](https://docs.sglang.ai/basic_usage/openai_api.html)。工具和多模态能力仍取决于模型及服务端配置；此接入不包含原生推理 API 或专属 reasoning 字段适配。

## Chat 消息角色

Runtime 当前只有一条基础系统指令，不拆分成多条。触发事件以 `user` 输入进入模型上下文，保留 `Runtime event:` 来源和事件标识；它是运行触发信息，不是伪造的用户聊天正文。旧检查点中已识别的 system 事件在请求投影时转为 user，原始恢复证据不改写。

自动上下文、控制、恢复、核对、交接和加急通知在 Agent 内直接使用 user；旧检查点中已识别的通知继续执行时迁移 role，历史记录保持原样。OpenAI 兼容和 DeepSeek 适配器仍只保留首条 system，对旧内部 system 通知在原位置转换为 user 作为兼容兜底。**不按相邻 system 的数量合并消息**；通知不能因紧跟基础指令就被并入系统策略。正文、来源标记、工具调用批次、多模态内容及 DeepSeek 推理状态保留；真实权限仍由 Runtime 执行检查。

所引用的 Qwen 聊天模板要求存在 user query，且 system 只能位于开头。这是通用消息布局兼容性，不按模型名称切换协议或增加 Qwen 专用接口。参见 [Qwen 官方模板](https://huggingface.co/Qwen/Qwen3.5-2B/blob/15852e8c16360a2fea060d615a32b45270f8a8fc/chat_template.jinja)。

做梦使用独立的 `system + user` 请求：system 来自固定 Markdown 模板，只包含整理规则和输出格式；user 包含本批次日期、历史证据与已有记忆。不同批次的 system 保持一致，做梦继续复用普通 Agent 的模型客户端、重试与计费链路，不加载普通会话工具或检查点。普通 Agent 的基础 system 同样按 Markdown 分节，动态事件与自动上下文仍使用 user。

## 后端专属状态

`ProviderState` 包含 `Source` 和不透明 JSON `Payload`。来源绑定协议、Provider 资源 ID、模型 ID、模型名称和实际端点。DeepSeek 适配器只有在这些字段完全匹配时才回传推理内容；不同来源的状态不参与请求编码。不存在的推理字段与明确的空字符串分别保存。

适配器按已知字段解析状态，不把任意 JSON 合并进请求，避免覆盖角色、正文或工具调用。专属状态计入上下文大小限制，随标准消息写入上下文存储，由检查点引用，不发布到聊天记录。工作台可以查看原始状态和来源。

幂等模型调用同时核对请求摘要和后端来源。即使请求正文相同，更换 Provider 后也不能复用原 Provider 的操作结果。

切换后端时保留通用正文和工具记录，但不承诺不同协议间完整执行状态可迁移。来源不明或之前未记录的推理内容不能补造。未来添加新的后端协议时，应单独实现适配器和契约测试；不能仅通过替换 URL 宣称支持。

DeepSeek 协议依据：[Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/)、[思考模式](https://api-docs.deepseek.com/guides/thinking_mode/)。

预设与协议测试使用可控服务验证请求格式、错误处理及配置保留，不代表所有供应商、模型版本或 GPU 部署均经过在线验收。

## 流式响应组装

模型客户端将 Chat Completions SSE 组装为统一完成响应，再交给协议适配器。正文、推理内容、拒绝说明以及工具调用的 ID、名称和参数使用增量缓冲，流结束时生成字段值，避免每个小片段都复制此前累积的完整字符串。多个工具按供应商的调用索引分别组装，最终仍要求连续索引；供应商扩展字段的非字符串值替换旧值，后续字符串重新开始累积，null 不覆盖已有内容。

模型客户端保留用量与计时回执，接收总量上限为 8 MiB；原始 HTTP／SSE 响应不写入数据库。流结束标记、请求身份、结束原因、用量和工具索引校验沿用现有边界。这里优化的是响应组装，不把模型内部流直接发布到聊天。已知费用证据与调用身份独立持久化；不保存第二份响应正文或 `Deltas` 恢复缓存。

完整输出尚未进入检查点时进程中断，后续执行允许重新请求模型。每次重新请求使用新的尝试 ID、操作 ID 和预算预留，保留原尝试的费用与未知状态，仍受最大尝试次数和截止时间限制。同一操作 ID 不重放结果，也不再次发送 HTTP 请求。工具操作的幂等与恢复规则不变。

部署配置 `diagnostics.model_responses` 默认 `false`，与 `debug.enabled` 独立。开启后，原始响应写入 `<data_dir>/logs/model-responses/responses.jsonl`，每条记录带调用身份、时间、HTTP 状态、Content-Type、响应体和不完整标记。最多保留当前文件与两份轮转文件，每份不超过 64 MiB；日志目录权限为 0700，文件为 0600，不保存请求头或认证信息。响应正文可能包含输入、输出及供应商返回的敏感内容，应仅在排障时开启。读取失败时记录已收到的内容，超过接收上限时截断并标记；日志失败只写通用服务日志，不阻止执行。关闭开关后停止新增文件记录，已有日志不自动删除。

`go test ./internal/agent/model` 覆盖大量片段、Unicode、交错工具调用、扩展字段替换、异常流、计时与计费证据。`go test ./internal/agent/model -run '^$' -bench BenchmarkChatStreamAssembly -benchmem` 按正文、推理和工具参数分别比较 256、1,024、4,096 个片段的时间与分配量；性能数值依赖机器，不作为固定响应时间承诺。

## ChatGPT Responses

`openai-responses` 用于管理员添加的 ChatGPT 套餐连接。请求固定为公开 `/v1/responses`，函数工具按 `molr` namespace 编码，system 消息转换为 developer，原样保留同源输出顺序、assistant phase 和 encrypted reasoning，使用显式历史和工具结果回放。SSE 必须收到 `response.completed` 才成功；额度错误保留结构化原因，禁止自动重试。

凭据由 Runtime 的 OAuth 适配器和现有 Vault 管理；刷新串行化并在使用新 access token 前原子保存轮换凭据。`PricingInfo.Basis=subscription-reference` 标识内部预算参考计价，预留、结算、组次数及并发限制继续走现有链路。

## 自动价格规则

`internal/pricing/providers.json` 保存带版本的官方价表，外部 `pricing.rules_file` 或 `pricing.rules_url` 完整替换内置规则，两项默认留空且互斥。每个地域条目保存 provider ID、HTTPS origins、标准 API paths、币种和来源；相同 provider 的国内／国际区使用不同条目。模型按完整 ID 与显式别名匹配。固定 `rates` 与分时 `off_peak` / `peak` 互斥。

固定费率可附带 `tiers`：`input_tokens_above` 与 `output_tokens_above` 的非零边界均使用严格大于，0 表示不限制该维度。按输入边界、输出边界递增排列，结算取最后一个满足条件的档位，替换整次调用三项单价。`valid_until` 为排他的 RFC3339 截止时间，到期停止匹配。目录返回基础价与全部档位，预算使用各维度最高费率，模型调用使用开始时固定的规则快照，账单保存实际档位。手动价格覆盖自动档位及预留费率。原币种保留为 USD / CNY。

来源与支持范围见[自动计价](https://molr.ai/zh/docs/admin/resources/models/#自动计价)。`TestProviderPricesMatchExactProductAndCurrency`、`TestTierSnapshotReserveAndPromotionExpiry`、`TestAutomaticPriceTiersCurrencyAndManualOverride` 与 `TestTieredPricingUsesActualUsageAndRetainsReceipt` 覆盖地域、订阅路径排除、过期、快照、分档边界和实际账单。

`pricing.Updater` 在启动时恢复同源缓存并刷新，Runtime 生命周期中每 24 小时检查一次；关闭 Runtime 时取消轮询与进行中的自动请求。`POST /api/models/refresh-pricing` 经管理员与 CSRF 校验后调用同一刷新入口，合并重叠请求，不接收用户指定的源地址。没有外部来源时返回 `Source=builtin`、`Updated=false`，不读取旧来源缓存或发起网络请求。

文件按内容比对，可识别原子替换和相同修改时间的变更。远程仅接受 HTTPS，使用部署网络守卫固定来源，禁止重定向；超时 10 秒，解码后体积最多 1 MiB。仅在内容完整校验并原子保存成功后切换价表及条件请求标识；失败保留此前价表、缓存和 ETag / Last-Modified。缓存存于数据目录 `pricing-cache/<source-hash>.json`，文件权限 0600，来源类型与完整路径／URL 共同决定缓存身份。重启先恢复有效缓存；无缓存或缓存无效时回退内置规则，再尝试配置来源。手动覆盖和已有调用／账单快照不变。

`updater_test.go` 覆盖文件／HTTPS 更新、条件请求、缓存失败、重启恢复、来源隔离、轮询关闭与并发刷新；`TestPricingRefreshHTTPAuthorizationAndRestartCache` 覆盖真实认证下的管理员、CSRF、手动刷新和重启。此机制不实现供应商价格采集或官网发布流程。
