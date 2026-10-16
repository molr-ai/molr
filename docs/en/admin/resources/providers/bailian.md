# Connect Bailian

Return to the [model connection overview](../models.md). Molr includes a Bailian connection preset.

## Connection settings

| Setting | Value |
| --- | --- |
| Preset | Bailian |
| Default base address | `https://dashscope.aliyuncs.com/compatible-mode/v1` |
| Authentication | API key |

Select Bailian to use its OpenAI-compatible mode, with an API key for the endpoint's region. Change the base address for other regions. Molr does not implement the native DashScope protocol or assume this preset includes Coding Plan subscription entitlements.

Follow the [shared connection steps](../models.md#connecting-providers) to test, save, and refresh models. Connection tests read the catalog; verify actual inference separately.

## Capabilities and validation

Choose models supporting Chat Completions and tools, then verify media capabilities and context limits. The preset preserves Bailian's identity but does not imply automatic price coverage; configure missing prices manually.

Usage and prices depend on the actual API product; website membership does not substitute for API authorization. See the [overview](../models.md) for networking, credentials, and budgets.
