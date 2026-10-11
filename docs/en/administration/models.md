# Connecting and Managing Models

This page is for administrators. First connect a provider, then assign available models and tools to members via [User Groups](users-and-groups.md).

## Connecting Providers

In the "Resources → Models" section, select "Connect Provider" from the More menu in the top-right corner. Enter the service address and authentication information, test the connection, and then save. A single provider connection can include multiple models.

Presets for Bailian, Zhipu, and Moonshot connect via OpenAI-compatible interfaces. After saving, you can refresh the model list; if model prices are not covered, they must be configured manually. A successful connection only indicates that the interface is accessible; actual inference and tool calls still require support from the selected model.

## Connecting vLLM and SGLang

In "Resources → Models → Connect Provider", select vLLM or SGLang. You can also find them by filtering with "Local". By default, it connects to the device where the Runtime is located: vLLM is `http://127.0.0.1:8000/v1`, and SGLang is `http://127.0.0.1:30000/v1`. Modify the address in "More Configuration" to connect to other deployments; the local address here refers to the machine where the Runtime is located, not the device where the browser is running.

Authentication is not required by default; if the service has API Key enabled, select the corresponding authentication method and enter the key. You can test the connection first. After saving, models in `/v1/models` are automatically fetched. You can refresh the list or edit the connection later. The Runtime must allow the target private/loopback IP via `network.allowed_private_ips`; selecting a preset does not bypass network restrictions.

Both presets use OpenAI-compatible Chat Completions. The server must be configured with a chat template suitable for the model; tool calls for assistants also require enabling the corresponding tool parser on the server side. Capabilities such as images and audio are configured based on the actual model and are not inferred from the model name. The `max_model_len` returned in the model list serves as the context limit; manually overridden values are retained after refreshing. Self-hosted models are not automatically considered free; please configure actual prices before use (free deployments can explicitly set 0).

If the model indicates chat template or tool parsing errors, first verify the server-side template configuration; maintainers can refer to [Model Adaptation Notes](../../../dev-docs/en/runtime/model-adapters.md). After fixing, send a new message to initiate a new call.

## Pricing, Credentials, and Maintenance

Before use, verify model capabilities, context capacity, and pricing; fill in 0 only when explicitly free. Model network permissions and external price files are maintained by the deployer; see [Model Network and Price Configuration](../deployment/configuration.md#model-network-and-pricing-configuration).

Provider keys are saved via resource configuration; old values are not displayed when replacing them. Deleting a provider cleans up connections, related models, and permissions, but retains assistants and chats; invalid models need to be re-selected.

Requests received by the model service carry `User-Agent: molr/<版本号>` for identification in service logs; if the version is not injected in development builds, `molr/development` is used. Connection tests and model list queries also use this identifier.

Connections do not automatically authorize all models. Users select models, set budgets, and grant permissions for their own assistants; see [Permissions and Budgets](../guides/permissions-and-budget.md).

### Connections rejected by network configuration

Runtime sends model discovery and inference requests directly, subject to the top-level `network.allowed_private_ips`. Sandbox Shell uses the separate `sandbox.networks` configuration; allowing sandbox traffic does not allow a model endpoint. For a private or loopback IP that is not allowed, the connection dialog asks the deployment administrator to add the target IP to `network.allowed_private_ips` and restart Runtime. Public HTTP endpoints require HTTPS. Prohibited destinations such as cloud metadata and link-local addresses cannot be allowed through this list.
