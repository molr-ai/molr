# Connect SiliconFlow · 硅基流动

Return to the [model connection overview](../models.md). Molr provides a SiliconFlow · 硅基流动 preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Default base URL | `https://api.siliconflow.cn/v1` |
| Protocol | Chat Completions |
| Authentication | API Key |

Use a SiliconFlow API key. Discovery requests `/models?sub_type=chat` to select chat models; inference preserves same-source `reasoning_content`. Tool and image support depend on the hosted model, not the provider identity. Embedding, reranker, and image-generation endpoints are outside this preset.

Follow the [connection steps](../models.md#connecting-providers) to test, save, and refresh models. Anthropic appends `/messages`; other presets append `/chat/completions`.

## Capabilities and validation

Connection tests read the catalog without inference. Configure capabilities, context, output limits, and prices, then verify real conversations and complete tool round trips. Unknown prices are not treated as free. Protocol and UI regressions use synthetic servers and do not establish online validation of every model.

[Official API documentation](https://api-docs.siliconflow.cn/docs/api/models-get).
