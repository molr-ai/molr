# Connect OpenCode

Return to the [model connection overview](../models.md). This guide covers cloud Zen / Console and Go, not the local CLI or Server.

## Zen / Console pay-as-you-go API

**API integration is allowed; Molr support is incomplete.** Models use specified Chat Completions, Responses, Anthropic Messages, or Gemini endpoints. Molr currently implements only Chat Completions among these protocols. [Inference API](https://opencode.ai/v2/docs/console/api/inference/)

Chat requests go to `https://opencode.ai/inference/openai/v1/chat/completions`, while discovery uses `https://opencode.ai/inference/v1/models`. Molr derives a sibling `/models` address, so automatic discovery does not match. The full catalog cannot be imported as Chat models. [Models and endpoints](https://opencode.ai/v2/docs/console/models/)

## Go subscriptions

Molr does not currently provide built-in support for this subscription plan. To use it, see [How to connect subscription plans](../models.md#how-to-connect-subscription-plans).
