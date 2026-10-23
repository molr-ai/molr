# Connect Z.AI

Known models on official standard APIs support automatic pricing. See [Automatic pricing](../models.md#automatic-pricing) for model coverage, regional currencies and billing limitations.

Return to the [model connection overview](../models.md). Molr provides a Z.AI preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Default base URL | `https://api.z.ai/api/paas/v4` |
| Protocol | Chat Completions |
| Authentication | API Key |

Select “Z.AI” and enter a standard Z.AI API key. The preset is separate from the Chinese Zhipu platform and preserves Z.AI identity and same-source `reasoning_content` without forcing a model-specific thinking mode. Add models manually after saving if discovery is unavailable. GLM Coding Plan endpoints and subscription entitlements are outside this standard API preset; see the [connection overview](../models.md#how-to-connect-subscription-plans).

Follow the [connection steps](../models.md#connecting-providers) to test, save, and refresh models. Anthropic appends `/messages`; other presets append `/chat/completions`.

## Capabilities and validation

Connection tests read the catalog without inference. Configure capabilities, context, output limits, and prices, then verify real conversations and complete tool round trips. Unknown prices are not treated as free. Protocol and UI regressions use synthetic servers and do not establish online validation of every model.

[Official API documentation](https://docs.z.ai/api-reference/llm/chat-completion).
