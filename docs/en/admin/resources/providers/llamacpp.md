# Connect llama.cpp

Return to the [model connection overview](../models.md). Molr includes a llama.cpp connection preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Preset | llama.cpp |
| Default base address | `http://127.0.0.1:8080/v1` |
| Authentication | None by default; enter a Bearer API key if configured on the service |

Start llama.cpp's HTTP inference server, then select its preset or local discovery result. Molr connects to its OpenAI-compatible API; it does not load model files, start the server, or select the server's chat template.

Follow the [shared connection steps](../models.md#connecting-providers) to test, save, and refresh models. Local addresses refer to the Runtime device. Private / loopback destinations need Runtime network permission; check reachability in container deployments too.

## Capabilities and validation

Tool calling depends on the model and chat template. Verify calls, arguments, and result continuation, and match context capacity to the server configuration. Configure prices, explicitly setting 0 for a free deployment.

If chat-template or tool-parser errors occur, fix the inference server settings and send a new message to verify. See the [overview](../models.md) for networking, credentials, and budgets.
