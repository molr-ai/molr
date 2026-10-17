# 模型接入概览

本页面向管理员，汇总当前接入方式。先连接提供商并配置模型，再通过[用户组](https://molr.ai/zh/docs/admin/administration/users-and-groups/)分配成员可用的模型与工具。各提供商的地址、操作和套餐限制见对应的「接入」页面。

## 支持的接入方式

内置预设表示已有连接入口和协议适配，不代表该厂商所有模型均可用。模型仍需支持所需的工具调用、上下文容量与多模态输入。

| 提供商／服务 | 当前接入方式 | 认证 | 配置说明 |
| --- | --- | --- | --- |
| OpenAI API | 内置预设；Chat Completions | API Key | [接入 OpenAI](https://molr.ai/zh/docs/admin/resources/providers/openai/) |
| OpenRouter | 内置预设；Chat Completions 与推理状态回传 | API Key | [接入 OpenRouter](https://molr.ai/zh/docs/admin/resources/providers/openrouter/) |
| DeepSeek API | 内置预设；DeepSeek 协议及推理状态回传 | API Key | [接入 DeepSeek](https://molr.ai/zh/docs/admin/resources/providers/deepseek/) |
| 阿里云百炼 | 内置预设；OpenAI 兼容接口 | API Key | [接入百炼](https://molr.ai/zh/docs/admin/resources/providers/bailian/) |
| 智谱开放平台 | 内置预设；OpenAI 兼容接口 | API Key | [接入智谱](https://molr.ai/zh/docs/admin/resources/providers/zhipu/) |
| Moonshot 开放平台 | 内置预设；OpenAI 兼容接口 | API Key | [接入 Moonshot / Kimi](https://molr.ai/zh/docs/admin/resources/providers/moonshot/) |
| Ollama | 内置预设；OpenAI 兼容接口、本机发现 | 无认证或 API Key | [接入 Ollama](https://molr.ai/zh/docs/admin/resources/providers/ollama/) |
| llama.cpp | 内置预设；OpenAI 兼容接口、本机发现 | 无认证或 API Key | [接入 llama.cpp](https://molr.ai/zh/docs/admin/resources/providers/llamacpp/) |
| vLLM | 内置预设；OpenAI 兼容接口、本机发现 | 无认证或 API Key | [接入 vLLM](https://molr.ai/zh/docs/admin/resources/providers/vllm/) |
| SGLang | 内置预设；OpenAI 兼容接口、本机发现 | 无认证或 API Key | [接入 SGLang](https://molr.ai/zh/docs/admin/resources/providers/sglang/) |
| Anthropic / Claude | 内置预设；Anthropic Messages | API Key | [接入 Anthropic / Claude](https://molr.ai/zh/docs/admin/resources/providers/anthropic/) |
| Google Gemini | 内置预设；Gemini Chat Completions | API Key | [接入 Google Gemini](https://molr.ai/zh/docs/admin/resources/providers/gemini/) |
| MiniMax | 内置预设；Chat Completions | API Key | [接入 MiniMax](https://molr.ai/zh/docs/admin/resources/providers/minimax/) |
| Volcengine · 火山方舟 | 内置预设；Chat Completions | API Key | [接入 Volcengine · 火山方舟](https://molr.ai/zh/docs/admin/resources/providers/volcengine/) |
| SiliconFlow · 硅基流动 | 内置预设；Chat Completions | API Key | [接入 SiliconFlow · 硅基流动](https://molr.ai/zh/docs/admin/resources/providers/siliconflow/) |
| LM Studio | 内置预设；OpenAI-compatible Chat Completions | 无认证或 API Key | [接入 LM Studio](https://molr.ai/zh/docs/admin/resources/providers/lmstudio/) |
| Z.AI | 内置预设；Chat Completions | API Key | [接入 Z.AI](https://molr.ai/zh/docs/admin/resources/providers/zai/) |
| 自定义服务／网关 | 自定义入口；需满足 Chat Completions 契约 | 无认证或 Bearer API Key | [接入自定义服务](https://molr.ai/zh/docs/admin/resources/providers/custom/) |

[OpenCode](https://molr.ai/zh/docs/admin/resources/providers/opencode/) 的模型发现和多协议适配尚未完整支持，不列为已支持的提供商。

当前没有通用 OAuth 登录、OpenAI Responses 或 Gemini 原生协议入口；Gemini 预设使用官方兼容接口。修改地址不会自动转换协议；自定义入口也不会自动启用 DeepSeek 专属推理状态处理。

## 连接提供商

1. 在「资源 → 模型」右上角更多菜单中选择「连接提供商」。
2. 按提供商说明选择预设，填写服务基础地址和认证信息；界面为 Anthropic 补齐 `/messages`，其他预设补齐 `/chat/completions`。
3. 测试连接并保存，再刷新模型列表。连接测试读取目录，不执行真实推理。
4. 配置模型能力、上下文容量、输出上限与价格，完成用户组分配，再用实际对话和工具调用验证。

点击提供商可在抽屉中查看模型列表；点击模型列表项后向左滑入配置页，标题栏可返回列表。保存或取消也会回到列表；「添加模型」位于抽屉及提供商更多菜单。

一个连接可以包含多个模型。目录无法获取时，先检查目录路径和认证要求；已保存连接可手动添加模型，但不能因此解决协议或授权不匹配。首次引导和连接窗口会探测 Runtime 所在设备的 Ollama、llama.cpp、vLLM、SGLang 与 LM Studio；仅查询接口，不启动服务或下载模型。

## 如何接入订阅套餐

Molr 当前提供标准模型 API 接入，没有内置订阅套餐专用的登录、凭据续期和会话适配。各家套餐的认证协议和允许用途不同，部分还限制第三方客户端、共享使用或后台自动化，不能统一按普通 API Key 处理。

如需使用订阅套餐，可以自行部署支持目标套餐的开源协议转换网关，由网关处理订阅认证与协议转换，再将其提供的 OpenAI 兼容 Chat Completions 地址和密钥通过[自定义服务](https://molr.ai/zh/docs/admin/resources/providers/custom/)接入 Molr。

> [!WARNING]
> 通过网关使用订阅套餐可能违反提供商的服务条款，导致订阅被暂停或账号受到限制；使用前请核实目标套餐是否允许该接入方式及实际用途。

## 价格、凭证与维护

核对能力、上下文容量和价格；明确免费时才填写 0。订阅费和套餐窗口额度不能用 Molr 的 token 价格完整表示，Molr 预算也不能替代厂商限额。外置价格文件与网络许可由部署者维护，见[模型网络与价格配置](https://molr.ai/zh/docs/admin/deployment/configuration/#模型网络与价格配置)。

提供商密钥通过资源配置保存，更换时不回显旧值。删除提供商会清理连接、模型与权限，保留助理和聊天；受影响的助理需重新选择模型。推理、目录查询和连接测试携带 `User-Agent: molr/<版本号>`；开发构建使用 `molr/development`。

连接不会自动授权全部模型。助理的模型选择、预算与授权见[权限与预算](https://molr.ai/zh/docs/user/guides/permissions-and-budget/)。

### 连接被网络配置拒绝

Runtime 直接发送模型请求，受 `network.allowed_private_ips` 限制；沙盒 Shell 使用独立的 `sandbox.networks`，配置沙盒网络不会放行模型地址。私网／回环 IP 未获允许时，部署者需将其加入 `network.allowed_private_ips` 并重启 Runtime。公网 HTTP 地址需改用 HTTPS；云元数据、链路本地等禁止地址不能通过白名单放行。

本机地址指 Runtime 所在机器，不是浏览器设备；容器内回环地址指容器自身。部署步骤见[源码构建](https://molr.ai/zh/docs/admin/deployment/source-build/)与[安装指南](https://molr.ai/zh/docs/admin/deployment/installation/)。
