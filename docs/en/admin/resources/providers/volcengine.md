# Connect Volcengine · 火山方舟

Return to the [model connection overview](../models.md). Molr provides a Volcengine · 火山方舟 preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Default base URL | `https://ark.cn-beijing.volces.com/api/v3` |
| Protocol | Chat Completions |
| Authentication | API Key |

Use an Ark API key and an enabled model ID, or the inference endpoint ID when using an endpoint deployment. The preset uses Chat Completions with same-source `reasoning_content` replay. Catalog visibility and inference permissions may differ; save the connection and add an authorized model manually if discovery is unavailable. Responses and Ark server-side tools are not integrated.

Follow the [connection steps](../models.md#connecting-providers) to test, save, and refresh models. Anthropic appends `/messages`; other presets append `/chat/completions`.

## Capabilities and validation

Connection tests read the catalog without inference. Configure capabilities, context, output limits, and prices, then verify real conversations and complete tool round trips. Unknown prices are not treated as free. Protocol and UI regressions use synthetic servers and do not establish online validation of every model.

[Official API documentation](https://docs.volcengine.com/docs/ark/compatible-with-openai-sdk?lang=zh).
