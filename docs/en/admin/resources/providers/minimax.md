# Connect MiniMax

Return to the [model connection overview](../models.md). Molr provides a MiniMax preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Default base URL | `https://api.minimax.cn/v1` |
| Protocol | Chat Completions |
| Authentication | API Key |

Connects to MiniMax’s OpenAI-compatible text API, using `reasoning_split: true` to separate thinking from visible text and preserve `reasoning_content` for tool continuations. For another region, use its matching API key and base URL. This preset does not integrate speech, image, or video generation APIs or convert subscription entitlements into standard API credits.

Follow the [connection steps](../models.md#connecting-providers) to test, save, and refresh models. Anthropic appends `/messages`; other presets append `/chat/completions`.

## Capabilities and validation

Connection tests read the catalog without inference. Configure capabilities, context, output limits, and prices, then verify real conversations and complete tool round trips. Unknown prices are not treated as free. Protocol and UI regressions use synthetic servers and do not establish online validation of every model.

[Official API documentation](https://platform.minimax.cn/docs/api-reference/text-openai-api).
