# Connect Google Gemini

Return to the [model connection overview](../models.md). Molr provides a Google Gemini preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Default base URL | `https://generativelanguage.googleapis.com/v1beta/openai` |
| Protocol | Gemini Chat Completions |
| Authentication | API Key |

Use a Gemini API key from Google AI Studio. This preset connects to Google’s official OpenAI-compatible API and preserves `extra_content.google.thought_signature` on its original tool call, including parallel and sequential calls. It is not a native GenerateContent or Vertex AI connection. Signatures never replay across models or connections. Verify and configure image/audio support, context capacity, and output limits when the compatible catalog does not declare them.

Follow the [connection steps](../models.md#connecting-providers) to test, save, and refresh models. Anthropic appends `/messages`; other presets append `/chat/completions`.

## Capabilities and validation

Connection tests read the catalog without inference. Configure capabilities, context, output limits, and prices, then verify real conversations and complete tool round trips. Unknown prices are not treated as free. Protocol and UI regressions use synthetic servers and do not establish online validation of every model.

[Official API documentation](https://ai.google.dev/gemini-api/docs/openai).
