# OpenAI

Known models on official standard APIs support automatic pricing. See [Automatic pricing](../models.md#automatic-pricing) for model coverage, regional currencies and billing limitations.

Administrators can choose an API key or Continue with ChatGPT for an OpenAI provider. Connections remain in the shared instance catalog; user groups control model access.

## API key

Enter an OpenAI API key and endpoint, discover models, and configure reference prices. This authentication method uses Chat Completions; choose models that support it.

## ChatGPT plan

1. Connect the Molr desktop app to the target Runtime and sign in as an administrator.
2. Add an OpenAI provider and select Continue with ChatGPT. Complete account sign-in and plan-usage consent in the system browser.
3. Available models are discovered for the authorized account. Configure reference prices, then select defaults or assign models to user groups as usual.

Initial authorization and reauthorization require the desktop app. The app receives the loopback callback and forwards the authorization code; Runtime exchanges it, validates identity and scopes, and encrypts the credentials. WebUI and background tasks on that Runtime can continue after the app closes. Multiple connections may use different accounts; connecting the same plan again does not increase its allowance.

Connection settings show the account and offer reauthorization, disconnect, and [Manage usage](https://chatgpt.com/settings/usage). Disconnect clears local tokens while retaining the registration for later sign-in. Unconfirmed remote revocation is reported so the administrator can check ChatGPT settings. Disconnect before deleting the connection. If the first code exchange fails, an issued registration is retained as disconnected and can be retried from its connection settings.

Plan requests use the public Responses API with Molr's existing Agent, tools, sandbox, and budget ledger. Call counts, concurrency, and period limits retain their existing rules. Configured prices provide internal budget valuation, not actual OpenAI charges or remaining subscription allowance. Unknown prices still require configuration; token counts do not reveal remaining plan allowance. Usage views and turn details identify subscription reference valuation.

This route requires `store: false`, `stream: true`, and explicit history, and rejects `max_output_tokens`. Output limits contribute to local budget reservations, not a provider-enforced generation cap. Text, configured image capabilities, and Molr function tools are supported; audio and unsupported OpenAI-hosted tools are not enabled. A plan usage limit does not automatically switch accounts or fall back to paid API calls.

The desktop app can authorize local and user-self-hosted Runtimes. Runtime connections retain the existing HTTPS or explicitly enabled LAN HTTP policy; LAN HTTP does not provide transport encryption.

Official guidance: [Open-source integration](https://developers.openai.com/siwc/token-sharing-open-source), [Models and inference](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference), [Preview limitations](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations).
