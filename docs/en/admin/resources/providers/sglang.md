# Connect SGLang

Return to the [model connection overview](../models.md). Molr includes a SGLang connection preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Preset | SGLang |
| Default base address | `http://127.0.0.1:30000/v1` |
| Authentication | None by default; enter a Bearer API key if configured on the service |

Select the SGLang preset or local discovery result, and use More Configuration for another deployment. Molr uses OpenAI-compatible Chat Completions; it does not implement SGLang's native inference API or vendor-specific reasoning fields.

Follow the [shared connection steps](../models.md#connecting-providers) to test, save, and refresh models. Local addresses refer to the Runtime device. Private / loopback destinations need Runtime network permission; check reachability in container deployments too.

## Capabilities and validation

Configure a matching chat template and tool parser, then verify consecutive tool calls. Discovery prefers a valid `context_window`, then `max_model_len`; refreshes retain manual overrides. Capabilities depend on deployment, and free models still require explicit zero prices.

If chat-template or tool-parser errors occur, fix the inference server settings and send a new message to verify. See the [overview](../models.md) for networking, credentials, and budgets.
