# Connect Moonshot / Kimi

Known models on official standard APIs support automatic pricing. See [Automatic pricing](../models.md#automatic-pricing) for model coverage, regional currencies and billing limitations.

Return to the [model connection overview](../models.md). Moonshot platform APIs and Kimi Code subscriptions are separate access products.

## Moonshot standard API

Select Moonshot in "Resources → Models → Connect Provider". The default base address is `https://api.moonshot.cn/v1`; enter that platform's API key. Use matching addresses and credentials for other regions or deployments.

Molr uses OpenAI-compatible Chat Completions. Test, save, refresh models, and configure tools, media capabilities, context, and prices. This preset does not implement Kimi Code login or subscription-specific integration. Models requiring proprietary reasoning fields need consecutive tool-call validation, not just a text reply.

## Kimi Code subscriptions

Molr does not currently provide built-in support for this subscription plan. To use it, see [How to connect subscription plans](../models.md#how-to-connect-subscription-plans).
