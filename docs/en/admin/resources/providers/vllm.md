# Connect vLLM

Return to the [model connection overview](../models.md). Molr includes a vLLM connection preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Preset | vLLM |
| Default base address | `http://127.0.0.1:8000/v1` |
| Authentication | None by default; enter a Bearer API key if configured on the service |

Select the vLLM preset or local discovery result. Use More Configuration for another device or port. Molr uses generic Chat Completions and does not select DeepSeek's dedicated protocol based on the model name.

Follow the [shared connection steps](../models.md#connecting-providers) to test, save, and refresh models. Local addresses refer to the Runtime device. Private / loopback destinations need Runtime network permission; check reachability in container deployments too.

## Capabilities and validation

Configure a suitable chat template and tool parser on the server. Discovery prefers a valid `context_window`, then `max_model_len`; refreshes retain manual overrides. Configure media capabilities and prices, explicitly setting 0 for a free deployment.

If chat-template or tool-parser errors occur, fix the inference server settings and send a new message to verify. See the [overview](../models.md) for networking, credentials, and budgets.
