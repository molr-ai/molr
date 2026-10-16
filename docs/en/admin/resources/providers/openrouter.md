# Connect OpenRouter

Return to the [model connection overview](../models.md).

Select OpenRouter in Connect provider and enter an OpenRouter API key. The default URL is `https://openrouter.ai/api/v1`; change it under More configuration if needed. Connection tests and catalog refreshes read the same-origin model directory, retaining models with text output and their full vendor-prefixed IDs.

The catalog supplies context/output limits and tool, image, and audio input capabilities. Complete input/output/cache-read tariffs are converted to USD per million tokens. An absent cache discount uses the normal input rate. Missing or invalid tariffs, or additional charges the ledger cannot measure, remain unknown and require manual configuration. Catalog prices drive Molr budgets and usage accounting; they are not OpenRouter's final invoice. Refreshes update automatic prices while preserving manual prices and capabilities. Disable custom pricing to restore the catalog tariff.

The dedicated adapter preserves reasoning continuation, including signatures and encrypted blocks across tool calls. State is not replayed after changing provider, endpoint, or model. This integration uses Chat Completions; it does not include OpenRouter Responses, native Messages, routing preferences, or reasoning-effort controls.
