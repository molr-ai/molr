# Connect Ollama

Return to the [model connection overview](../models.md). Molr includes a Ollama connection preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Preset | Ollama |
| Default base address | `http://127.0.0.1:11434/v1` |
| Authentication | None by default; enter a Bearer API key if configured on the service |

Run Ollama and prepare a model on a device reachable by Runtime, then select its preset or local discovery result. Molr uses the OpenAI-compatible interface; include `/v1` rather than the native `/api/chat` path.

Follow the [shared connection steps](../models.md#connecting-providers) to test, save, and refresh models. Local addresses refer to the Runtime device. Private / loopback destinations need Runtime network permission; check reachability in container deployments too.

## Capabilities and validation

Administrators manage model downloads and service startup. Configure tools, images, and context based on the actual model and server settings; local discovery does not verify these capabilities. Self-hosting is not automatically free; explicitly enter 0 for a free deployment.

If chat-template or tool-parser errors occur, fix the inference server settings and send a new message to verify. See the [overview](../models.md) for networking, credentials, and budgets.
