# 模型后端适配

Runtime 仍是单体 Go 服务。`internal/agent/model` 使用固定的内置协议适配器，不提供动态注册或插件加载。

## 职责

- Agent Loop 使用统一的正文、工具调用、用量和 `ProviderState`，不解析后端字段。
- 模型客户端负责权限、凭证、网络访问、幂等记录和费用结算。
- 协议适配器负责请求编码、响应解析，以及后端专属状态的保存与回传。

目前接入两种协议：

| 协议 | 请求与状态 |
| --- | --- |
| `openai-chat-completions` | 通用 Chat Completions，使用 `max_completion_tokens`、`store: false`，不采纳 DeepSeek 扩展字段 |
| `deepseek-chat-completions` | 使用 `max_tokens`，不发送 OpenAI 的 `store` 参数；处理 `reasoning_content` 与 DeepSeek 缓存用量 |

应用层根据用户选中的 Provider 预设解析协议，不根据模型名称或服务地址猜测。当前资源中该预设字段仍名为 `PricingProvider`；选择 DeepSeek 预设会启用 DeepSeek 协议，其余现有预设使用通用协议。设置手动价格不改变协议选择。自定义预设不是任意后端协议的自动检测器。

## 云端兼容服务预设

百炼（`bailian`）、智谱（`zhipu`）和 Moonshot（`moonshot`）与 WebUI 预设一致，保存、编辑及模型发现保留各自的提供商身份，使用 `openai-chat-completions` 协议与 API Key 认证。这些预设不代表接入厂商原生协议；未知价格仍需配置，不自动视为免费。


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

适配器按已知字段解析状态，不把任意 JSON 合并进请求，避免覆盖角色、正文或工具调用。专属状态计入上下文大小限制，在模型结果缓存和检查点中持久化，不发布到聊天记录。工作台可以查看原始状态和来源。

幂等模型调用同时核对请求摘要和后端来源。即使请求正文相同，更换 Provider 后也不能复用原 Provider 的操作结果。

切换后端时保留通用正文和工具记录，但不承诺不同协议间完整执行状态可迁移。来源不明或之前未记录的推理内容不能补造。未来添加新的后端协议时，应单独实现适配器和契约测试；不能仅通过替换 URL 宣称支持。

DeepSeek 协议依据：[Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/)、[思考模式](https://api-docs.deepseek.com/guides/thinking_mode/)。

预设与协议测试使用可控服务验证请求格式、错误处理及配置保留，不代表所有供应商、模型版本或 GPU 部署均经过在线验收。
