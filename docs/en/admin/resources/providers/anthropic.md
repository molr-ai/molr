# Connect Anthropic / Claude

Return to the [model connection overview](../models.md). Molr provides a Anthropic / Claude preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Default base URL | `https://api.anthropic.com/v1` |
| Protocol | Anthropic Messages |
| Authentication | API Key |

Use an Anthropic API key. Molr sends `x-api-key` and `anthropic-version` to the native Messages API and follows same-origin `/models` pagination. System instructions, tools, results, and images use native content blocks; streamed thinking blocks and signatures replay only to the same backend. Thinking controls, server tools, audio input, and explicit prompt-cache writes are not enabled. Cache reads participate in billing; nonzero cache-write usage requires cost reconciliation because the current tariff cannot represent it.

Follow the [connection steps](../models.md#connecting-providers) to test, save, and refresh models. Anthropic appends `/messages`; other presets append `/chat/completions`.

## Capabilities and validation

Connection tests read the catalog without inference. Configure capabilities, context, output limits, and prices, then verify real conversations and complete tool round trips. Unknown prices are not treated as free. Protocol and UI regressions use synthetic servers and do not establish online validation of every model.

[Official API documentation](https://platform.claude.com/docs/en/api/messages/create).
