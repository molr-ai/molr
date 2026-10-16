# Model Connection Overview

This page summarizes model access for administrators. Connect a provider, configure its models, and assign models and tools through [user groups](../administration/users-and-groups.md). Each provider guide contains addresses, setup steps, and plan restrictions.

## Supported connection methods

A built-in preset provides a connection entry and protocol adapter; it does not guarantee support for every model from that vendor. Models must support the required tools, context capacity, and media inputs.

| Provider / service | Current connection method | Authentication | Guide |
| --- | --- | --- | --- |
| OpenAI API | Built-in preset; Chat Completions | API key | [Connect OpenAI](providers/openai.md) |
| OpenRouter | Built-in preset; Chat Completions and reasoning state replay | API key | [Connect OpenRouter](providers/openrouter.md) |
| DeepSeek API | Built-in preset; DeepSeek protocol and reasoning state replay | API key | [Connect DeepSeek](providers/deepseek.md) |
| Alibaba Cloud Bailian | Built-in preset; OpenAI-compatible API | API key | [Connect Bailian](providers/bailian.md) |
| Zhipu platform | Built-in preset; OpenAI-compatible API | API key | [Connect Zhipu](providers/zhipu.md) |
| Moonshot platform | Built-in preset; OpenAI-compatible API | API key | [Connect Moonshot / Kimi](providers/moonshot.md) |
| Ollama | Built-in preset; OpenAI-compatible API and local discovery | None or API key | [Connect Ollama](providers/ollama.md) |
| llama.cpp | Built-in preset; OpenAI-compatible API and local discovery | None or API key | [Connect llama.cpp](providers/llamacpp.md) |
| vLLM | Built-in preset; OpenAI-compatible API and local discovery | None or API key | [Connect vLLM](providers/vllm.md) |
| SGLang | Built-in preset; OpenAI-compatible API and local discovery | None or API key | [Connect SGLang](providers/sglang.md) |
| Anthropic / Claude | Built-in preset; Anthropic Messages | API Key | [Connect Anthropic / Claude](providers/anthropic.md) |
| Google Gemini | Built-in preset; Gemini Chat Completions | API Key | [Connect Google Gemini](providers/gemini.md) |
| MiniMax | Built-in preset; Chat Completions | API Key | [Connect MiniMax](providers/minimax.md) |
| Volcengine · 火山方舟 | Built-in preset; Chat Completions | API Key | [Connect Volcengine · 火山方舟](providers/volcengine.md) |
| SiliconFlow · 硅基流动 | Built-in preset; Chat Completions | API Key | [Connect SiliconFlow · 硅基流动](providers/siliconflow.md) |
| LM Studio | Built-in preset; OpenAI-compatible Chat Completions | None or API key | [Connect LM Studio](providers/lmstudio.md) |
| Z.AI | Built-in preset; Chat Completions | API Key | [Connect Z.AI](providers/zai.md) |
| Custom service / gateway | Custom connection; requires the Chat Completions contract | None or Bearer API key | [Connect a custom service](providers/custom.md) |

[OpenCode](providers/opencode.md) discovery and protocol coverage are incomplete, so it is not listed as a supported provider.

There is no generic OAuth login, OpenAI Responses, or native Gemini connection; the Gemini preset uses Google’s official compatibility API. Changing an address does not convert protocols. A custom connection does not automatically enable DeepSeek-specific reasoning state handling.

## Connecting providers

1. In "Resources → Models", select "Connect Provider" from the top-right More menu.
2. Select the preset and enter the base address and credentials from the provider guide. The UI appends `/messages` for Anthropic and `/chat/completions` for other presets.
3. Test and save the connection, then refresh the model list. Testing reads the catalog without running inference.
4. Configure capabilities, context capacity, output limits, and prices, assign models to groups, and verify with real conversations and tool calls.

One connection can contain multiple models. If discovery fails, check the catalog path and authentication requirements. You can add models manually to a saved connection, but this does not resolve protocol or authorization mismatches. Initial setup and the connection dialog probe Ollama, llama.cpp, vLLM, SGLang, and LM Studio on the Runtime device without starting services or downloading models.

## How to connect subscription plans

Molr currently connects to standard model APIs and has no built-in subscription-specific login, credential renewal, or session integration. Plans differ in authentication protocols and permitted uses; some restrict third-party clients, shared access, or background automation, so they cannot all be handled as ordinary API keys.

To use a subscription, you can deploy an open-source protocol conversion gateway that supports the target plan. Let the gateway handle subscription authentication and protocol conversion, then connect its OpenAI-compatible Chat Completions address and key to Molr through a [custom service](providers/custom.md).

> [!WARNING]
> Using a subscription through a gateway may violate the provider's terms of service and result in subscription suspension or account restrictions. Check whether the plan permits this connection method and your intended use before proceeding.

## Pricing, credentials, and maintenance

Verify capabilities, context capacity, and prices; set 0 only when explicitly free. Token pricing in Molr cannot fully represent subscription fees and allowance windows, and Molr budgets do not replace provider limits. Deployment administrators maintain pricing files and networking; see [model network and pricing configuration](../deployment/configuration.md#model-network-and-pricing-configuration).

Keys are stored through resource settings and old values are not displayed on replacement. Deleting a provider removes its connection, models, and permissions while retaining assistants and chats; affected assistants need another model. Inference, discovery, and connection tests send `User-Agent: molr/<version>`; development builds use `molr/development`.

Connecting does not grant access to every model. See [permissions and budgets](../../user/guides/permissions-and-budget.md) for assistant model selection and authorization.

### Connections rejected by network configuration

Runtime sends model requests directly, subject to `network.allowed_private_ips`. Sandbox Shell uses separate `sandbox.networks` settings; allowing sandbox traffic does not allow a model endpoint. Administrators must add required private / loopback IPs to `network.allowed_private_ips` and restart Runtime. Public HTTP endpoints require HTTPS; prohibited destinations such as cloud metadata and link-local addresses cannot be allowed.

Local addresses refer to the Runtime machine, not the browser device; container loopback addresses refer to the container itself. See [building from source](../deployment/source-build.md) and the [installation guide](../deployment/installation.md).
