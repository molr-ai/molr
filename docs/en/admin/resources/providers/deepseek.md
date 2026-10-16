# Connect DeepSeek

Return to the [model connection overview](../models.md). Molr includes a DeepSeek connection preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Preset | DeepSeek |
| Default base address | `https://api.deepseek.com/v1` |
| Authentication | API key |

The DeepSeek preset enables its dedicated output parameters, cache usage handling, and reasoning state replay. Keep this preset when changing addresses; a custom connection uses the generic protocol and does not enable these behaviors.

Follow the [shared connection steps](../models.md#connecting-providers) to test, save, and refresh models. Connection tests read the catalog; verify actual inference separately.

## Capabilities and validation

Verify consecutive tool calls and reasoning-state replay. If changing backends produces a context-incompatibility error, reset assistant context as instructed; do not fabricate reasoning fields. Automatic prices come from the current price configuration; manual prices do not change the protocol.

Usage and prices depend on the actual API product; website membership does not substitute for API authorization. See the [overview](../models.md) for networking, credentials, and budgets.
