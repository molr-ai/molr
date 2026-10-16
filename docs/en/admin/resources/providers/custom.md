# Connect a Custom Service

Return to the [model connection overview](../models.md). Custom connections support model services and gateways that satisfy the current protocol; they do not detect or translate arbitrary vendor protocols.

## Connection settings

Select "Custom service", enter its base address and name, and choose no authentication or an API key. Keys are sent as Bearer tokens; this entry does not support OAuth login or arbitrary custom headers.

For a base address such as `https://models.example.com/v1`, the UI appends `/chat/completions`. Tests and discovery use `/models` under that same base. If the upstream catalog is elsewhere, normalize it through a gateway or add models manually after saving. Diagnose discovery and inference failures separately.

## Required service contract

- Accept OpenAI-compatible Chat Completions with `messages`, tool definitions, `stream: true`, `stream_options.include_usage`, `max_completion_tokens`, and `store: false`.
- Return parseable completions, tool calls, and token usage. Full tool cycles must preserve call IDs, arguments, and corresponding results.
- The generic adapter does not replay DeepSeek or other vendor-specific reasoning fields. Models requiring them or another protocol need dedicated adaptation, not just another URL.
- Discovery needs a `data` array with model `id` values. Verify capabilities, context, and prices separately.

See [model adapters](../../../dev/runtime/model-adapters.md) for protocol and streaming constraints.

## Self-hosted gateways

A gateway translates upstream protocols, normalizes model catalogs, and manages credentials. Exposing a Chat Completions address does not establish correct multi-turn tools, cancellation, usage, or reasoning-state translation. Validate with artificial data before connecting.

Molr sends requests to the configured gateway; that gateway handles upstream credentials, routing, and logs. Administrators must define this trust boundary and avoid giving real credentials to unknown relay services.

## Subscription plans

Molr does not currently provide built-in support for subscription plans. To use them, see [How to connect subscription plans](../models.md#how-to-connect-subscription-plans).
