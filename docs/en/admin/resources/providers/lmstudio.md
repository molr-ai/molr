# Connect LM Studio

Return to the [model connection overview](../models.md). Molr provides a LM Studio preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Default base URL | `http://127.0.0.1:1234/v1` |
| Protocol | OpenAI-compatible Chat Completions |
| Authentication | None or API key |

Start LM Studio’s API Server and prepare a tool-capable model. Use no authentication or the API token configured on the server. Local scanning includes port 1234 and identifies LM Studio through read-only `/api/v0/models` metadata; it never starts the server or loads/downloads models. Saved connections discover models through `/v1/models`. Loopback refers to the Runtime machine; remote devices require the appropriate address and Runtime private-IP allowlist.

Follow the [connection steps](../models.md#connecting-providers) to test, save, and refresh models. Anthropic appends `/messages`; other presets append `/chat/completions`.

## Capabilities and validation

Connection tests read the catalog without inference. Configure capabilities, context, output limits, and prices, then verify real conversations and complete tool round trips. Unknown prices are not treated as free. Protocol and UI regressions use synthetic servers and do not establish online validation of every model.

[Official API documentation](https://lmstudio.ai/docs/developer/core/server).
