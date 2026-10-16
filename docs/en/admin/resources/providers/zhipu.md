# Connect Zhipu

Return to the [model connection overview](../models.md). Molr includes a Zhipu connection preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Preset | Zhipu |
| Default base address | `https://open.bigmodel.cn/api/paas/v4` |
| Authentication | API key |

Select Zhipu and enter a mainland China platform API key. The preset uses OpenAI-compatible Chat Completions and does not cover every native API or subscription. See [Connect Z.AI](zai.md) for international accounts, addresses, and plan scope; do not interchange them.

Follow the [shared connection steps](../models.md#connecting-providers) to test, save, and refresh models. Connection tests read the catalog; verify actual inference separately.

## Capabilities and validation

Configure actual tool capabilities, context, and prices, and verify continuation after a tool call. The generic adapter does not replay vendor-specific reasoning fields; if a model requires them, a successful connection test does not establish compatibility.

Usage and prices depend on the actual API product; website membership does not substitute for API authorization. See the [overview](../models.md) for networking, credentials, and budgets.
