# Model Backend Adaptation

Runtime remains a monolithic Go service. `internal/agent/model` uses fixed built-in protocol adapters and does not provide dynamic registration or plugin loading.

## Responsibilities

- The Agent Loop uses unified body, tool calls, usage, and `ProviderState`, without parsing backend fields.
- The model client is responsible for permissions, credentials, network access, idempotency records, and cost settlement.
- The protocol adapter is responsible for request encoding, response parsing, and saving and returning backend-specific state.

Currently, seven protocols are integrated:

| Protocol | Request and State |
| --- | --- |
| `anthropic-messages` | Native Messages, `x-api-key` authentication, content blocks, tool results, SSE, and signed state replay |
| `gemini-chat-completions` | Google’s official compatibility API; retains per-call thought signatures and omits `store` |
| `minimax-chat-completions` | Compatible reasoning with `reasoning_split: true` to separate thinking from visible text |
| `reasoning-chat-completions` | Volcengine, SiliconFlow, and Z.AI; uses `max_tokens`, replays same-source `reasoning_content`, omits `store` |
| `openai-chat-completions` | Generic Chat Completions, using `max_completion_tokens` and `store: false`, does not adopt DeepSeek extension fields |
| `openrouter-chat-completions` | Uses `max_tokens`; preserves `reasoning`/`reasoning_details`, without `store` |
| `deepseek-chat-completions` | Uses `max_tokens`, does not send OpenAI's `store` parameter; handles `reasoning_content` and DeepSeek cache usage |

The application layer resolves the protocol based on the Provider preset selected by the user, rather than guessing based on model name or service address. In the current resources, this preset field is still named `PricingProvider`; presets explicitly select one of the protocols above. Setting manual prices does not change protocol selection. Custom presets are not automatic detectors for arbitrary backend protocols.

All model inference requests (including dreaming, context compression, and retries), model list queries, connection tests, and local model discovery uniformly carry `User-Agent: molr/<版本号>`. The version comes from `internal/buildinfo.Version`, consistent with `molr version`; release builds inject it via `-ldflags -X`, while development builds without an injected version use `molr/development`.

## Model Location and Configuration Consistency

The model ID is stably calculated from the provider resource ID and model name. Private binding records save the derived `ModelIDs`, locating the owning provider directly via array member index; creation, discovery refresh, manual configuration, and connection updates all recalculate this within the binding write transaction, and index items are deleted along with the binding when it is removed. Derived fields are maintained by the resource module.

Each model resolution reads the target binding, resource status, and current user group reference within the same transaction snapshot, then resolves the current pricing and protocol. The model client continues to perform model-scope authorization, budget, and network checks. Query read volume only includes the target provider and does not increase with the number of unrelated providers.

## Cloud-Compatible Service Presets

Bailian (`bailian`), Zhipu (`zhipu`), and Moonshot (`moonshot`) are consistent with WebUI presets. Saving, editing, and model discovery preserve their respective provider identities, using the `openai-chat-completions` protocol and API Key authentication. These presets do not represent integration with vendor-native protocols; unknown prices still require configuration and are not automatically considered free.

## Anthropic and Gemini

Anthropic maps the first system instruction to top-level `system` and combines consecutive same-role blocks into native messages. Batched tool results precede supplemental images. Signed thinking blocks retain their order; replay checks source identity and consistency with the context text and tool calls. SSE requires complete blocks, a stop reason, output usage, and `message_stop`; errors and interruptions cannot establish success. Total input includes ordinary input plus cache reads. Cache writes are not requested; nonzero cache-write usage requires cost reconciliation. Model discovery follows `after_id` pagination, bounded at 2 MiB and 500 entries across all pages, and reads declared `max_input_tokens` and `max_tokens`.

Gemini uses the official Chat Completions compatibility API. Stream assembly retains each tool call’s `extra_content.google.thought_signature`; replay matches backend identity, call ID, function name, and arguments, without forwarding arbitrary extensions. Both adapters include continuation state in context estimates and keep it out of chat messages. References: [Anthropic](https://platform.claude.com/docs/en/api/messages/create) and [Gemini signatures](https://ai.google.dev/gemini-api/docs/generate-content/thought-signatures).

MiniMax, Volcengine, SiliconFlow, and Z.AI share a compatible reasoning adapter that replays only same-source `reasoning_content`, without requiring it on every historical message or forcing vendor thinking switches. SiliconFlow discovery filters with `sub_type=chat`. New presets do not include automatic tariffs.

LM Studio uses generic Chat Completions and adds port 1234 to local scanning. Read-only `/api/v0/models` type, load-state, and compatibility metadata identify the service without loading models. Saved connections continue to discover through `/v1/models`.

## OpenRouter

The `openrouter` preset explicitly selects its dedicated protocol while reusing the credential broker, network guard, and budget ledger. `/api/v1/models` retains text-output models via `architecture.output_modalities` and reads `context_length`, `top_provider.max_completion_tokens`, `architecture.input_modalities`, and `supported_parameters`, without guessing from names. Its catalog is bounded at 8 MiB and 4,096 entries; generic compatible catalogs retain the 2 MiB/500-entry bounds.

Decimal USD/token strings in `pricing` are converted with rational arithmetic to micro-USD per million tokens, rounding upward. Missing cache discounts use the ordinary input rate. Nonzero extra charges that cannot be fully represented in the ledger prevent automatic pricing. Discovered tariffs live in `ProviderPricing`; owner overrides remain in `Pricing`. Refresh and configuration preserve this distinction. Effective automatic prices return `PricingInfo.Source=provider`.

The adapter replays only same-source `reasoning_details`, falling back to `reasoning` when structured blocks are absent. Other top-level message extensions never enter requests. Streaming assembly merges consecutive text/summary and signature fragments with matching identities, keeping encrypted blocks separate and preserving type transitions; repeated indices alone do not identify blocks. Replayed state participates in context estimates and stays out of chat history.

References: [OpenRouter quickstart](https://openrouter.ai/docs/quickstart), [model catalog](https://openrouter.ai/docs/api/api-reference/models/list-all-models-and-their-properties), and [reasoning state](https://openrouter.ai/docs/guides/best-practices/reasoning-tokens). Protocol, refresh, and WebUI regressions use synthetic model servers; online model behavior requires separate validation.

## Local Inference Service Presets

vLLM (`vllm`) and SGLang (`sglang`) use the existing `openai-chat-completions` adapter, supporting no authentication or Bearer API Key. Presets, save validation, and protocol selection preserve their independent identities; they do not switch to the DeepSeek protocol based on the deployed model name, nor do they return backend-specific inference state.

Discovered models follow `/v1/models`, prioritizing valid `context_window`, followed by vLLM/SGLang's `max_model_len` (256–2,000,000); if no valid value exists, existing defaults continue to be used, and refreshes do not overwrite user configurations. Network whitelists, credentials, and budget rules remain consistent, and zero prices are not automatically filled in.

Protocol references: [vLLM OpenAI Compatible Service](https://docs.vllm.ai/en/latest/serving/openai_compatible_server/), [SGLang OpenAI Compatible API](https://docs.sglang.ai/basic_usage/openai_api.html). Tool and multimodal capabilities still depend on the model and server-side configuration; this integration does not include native inference APIs or dedicated reasoning field adaptation.

## Chat Message Roles

Runtime currently has only one basic system instruction, which is not split into multiple parts. Trigger events enter the model context as `user` input, preserving `Runtime event:` source and event identifiers; this is runtime trigger information, not forged user chat content. Identified system events in old checkpoints are converted to user during request projection, and original recovery evidence is not rewritten.

Automatic context, control, recovery, verification, handover, and urgent notifications directly use user within the Agent; identified notifications in old checkpoints continue to migrate role during execution, while history records remain unchanged. OpenAI-compatible and DeepSeek adapters still retain only the first system message, converting old internal system notifications to user in their original positions as a compatibility fallback. **Messages are not merged based on the number of adjacent system messages**; notifications cannot be merged into the system policy simply because they follow the basic instruction. Body, source markers, tool call batches, multimodal content, and DeepSeek reasoning state are preserved; real permissions are still enforced by Runtime checks.

The referenced Qwen chat template requires the existence of a user query, and system messages can only be located at the beginning. This is general message layout compatibility, not switching protocols or adding Qwen-specific interfaces based on model name. See [Qwen Official Template](https://huggingface.co/Qwen/Qwen3.5-2B/blob/15852e8c16360a2fea060d615a32b45270f8a8fc/chat_template.jinja).

Dreaming uses an independent `system + user` request: system comes from a fixed Markdown template, containing only organization rules and output format; user contains the date of this batch, historical evidence, and existing memories. System remains consistent across different batches. Dreaming continues to reuse the model client, retry, and billing chain of the normal Agent, without loading normal session tools or checkpoints. The basic system of the normal Agent is also sectioned by Markdown, while dynamic events and automatic context still use user.

## Backend-Specific State

`ProviderState` contains `Source` and opaque JSON `Payload`. The source binds the protocol, Provider resource ID, model ID, model name, and actual endpoint. The DeepSeek adapter only returns reasoning content when these fields match completely; state from different sources does not participate in request encoding. Non-existent reasoning fields and explicit empty strings are saved separately.

Adapters parse state based on known fields, not merging arbitrary JSON into requests, to avoid overwriting roles, body, or tool calls. Dedicated state counts towards context size limits, is written to the context store with standard messages, is referenced by checkpoints, and is not published to chat records. The workbench can view raw state and source.

Idempotent model calls verify both the request digest and backend source. Even if the request body is identical, operation results from the original Provider cannot be reused after switching Providers.

When switching backends, generic body and tool records are preserved, but complete execution state migration between different protocols is not guaranteed. Reasoning content that is of unknown origin or previously unrecorded cannot be fabricated. When adding new backend protocols in the future, adapters and contract tests should be implemented separately; support cannot be claimed merely by replacing the URL.

DeepSeek protocol references: [Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/), [Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode/).

Preset and protocol tests use controllable services to verify request formats, error handling, and configuration retention, which does not imply that all vendors, model versions, or GPU deployments have undergone online acceptance testing.

## Streaming Response Assembly

The model client assembles Chat Completions SSE into a unified completion response, then passes it to the protocol adapter. Body, reasoning content, refusal explanations, and tool call IDs, names, and parameters use incremental buffering, generating field values at the end of the stream to avoid copying the previously accumulated complete string for every small fragment. Multiple tools are assembled separately according to the vendor's call index, but continuous indices are still required in the end; non-string values of vendor extension fields replace old values, subsequent strings restart accumulation, and null does not overwrite existing content.

The model client retains usage and timing receipts, with a total reception limit of 8 MiB; raw HTTP/SSE responses are not written to the database. Stream end markers, request identity, end reasons, usage, and tool index validation follow existing boundaries. This optimizes response assembly, not publishing model internal streams directly to chat. Known cost evidence and call identity are persisted independently; a second copy of the response body or `Deltas` recovery cache is not saved.

If the process is interrupted before the complete output enters the checkpoint, subsequent execution allows re-requesting the model. Each re-request uses a new attempt ID, operation ID, and budget reservation, preserving the cost and unknown state of the original attempt, still subject to maximum attempt counts and deadlines. The same operation ID does not replay results or send HTTP requests again. Idempotency and recovery rules for tool operations remain unchanged.

Deployment configuration `diagnostics.model_responses` defaults to `false`, independent of `debug.enabled`. When enabled, raw responses are written to `<data_dir>/logs/model-responses/responses.jsonl`, with each record containing call identity, time, HTTP status, Content-Type, response body, and incomplete flag. At most the current file and two rotated files are retained, each not exceeding 64 MiB; log directory permissions are 0700, files are 0600, and request headers or authentication information are not saved. Response bodies may contain input, output, and sensitive content returned by vendors, and should only be enabled for troubleshooting. When reading fails, received content is recorded; when the reception limit is exceeded, it is truncated and marked; log failures only write to general service logs and do not block execution. After disabling the switch, new file records stop, and existing logs are not automatically deleted.

`go test ./internal/agent/model` covers large fragments, Unicode, interleaved tool calls, extension field replacement, abnormal streams, timing, and billing evidence. `go test ./internal/agent/model -run '^$' -bench BenchmarkChatStreamAssembly -benchmem` compares time and allocation for 256, 1,024, and 4,096 fragments separately for body, reasoning, and tool parameters; performance figures depend on the machine and are not fixed response time commitments.

## ChatGPT Responses

`openai-responses` serves administrator-managed ChatGPT plan connections at the public `/v1/responses` endpoint. Functions use the `molr` namespace, system messages become developer messages, and same-source output order, assistant phase, and encrypted reasoning are preserved with explicit history and tool outputs. SSE succeeds only after `response.completed`; quota errors retain their structured reason and are not automatically retried.

The Runtime OAuth adapter and existing Vault manage credentials. Refreshes are serialized and rotating credentials are saved atomically before using the renewed access token. `PricingInfo.Basis=subscription-reference` identifies internal reference valuation; reservations, settlement, group call limits, and concurrency reuse existing accounting.

## Automatic pricing rules

`internal/pricing/providers.json` stores versioned official tariffs. An external `pricing.rules_file` or `pricing.rules_url` fully replaces the built-in rules. Both default to empty and are mutually exclusive. Each regional entry declares a provider ID, HTTPS origins, standard API paths, currency and source URL. Domestic and international regions use separate entries. Models match complete IDs and explicit aliases. Fixed `rates` and scheduled `off_peak` / `peak` rates are mutually exclusive.

Fixed rates may have `tiers`. Nonzero `input_tokens_above` and `output_tokens_above` boundaries are exclusive; zero leaves that dimension unrestricted. Tiers are ordered by input then output boundary. The last matching tier replaces all three rates for the entire request. `valid_until` is an exclusive RFC3339 cutoff. The catalog returns base prices and tiers; reservations use each rate's maximum. Calls retain the rules resolved at dispatch, and receipts retain the applied tier. Manual prices override both automatic tiers and reservation rates. Original USD / CNY currencies are preserved.

Sources and scope are listed under [Automatic pricing](../../admin/resources/models.md#automatic-pricing). `TestProviderPricesMatchExactProductAndCurrency`, `TestTierSnapshotReserveAndPromotionExpiry`, `TestAutomaticPriceTiersCurrencyAndManualOverride` and `TestTieredPricingUsesActualUsageAndRetainsReceipt` cover regional currencies, subscription-path exclusion, expiry, snapshots, tier boundaries and receipts.

`pricing.Updater` restores the same source’s cache and refreshes at startup, then checks every 24 hours during the Runtime lifecycle. Shutdown cancels polling and active automatic requests. `POST /api/models/refresh-pricing` requires administrator access and CSRF, shares the same refresh operation, coalesces overlapping requests and accepts no caller-supplied source. With no external source, it returns `Source=builtin`, `Updated=false` without reading another source’s cache or making network requests.

Files are compared by content, including atomic replacements and changes with identical modification times. Remote sources require HTTPS and the deployment network guard pins the origin; redirects are rejected. Requests have a 10-second timeout and a 1 MiB decoded-body limit. Rules and conditional validators change only after full validation and atomic persistence succeed. Failures preserve the active rules, cache and ETag / Last-Modified. Cache files live under the data directory at `pricing-cache/<source-hash>.json`, with mode 0600. Source kind and full path/URL identify each cache. Startup restores valid cached rules, falling back to built-ins if unavailable, before checking the configured source. Manual prices and existing invocation/receipt snapshots remain unchanged.

`updater_test.go` covers file/HTTPS updates, conditional requests, cache failure, restart recovery, source isolation, shutdown and concurrent refreshes. `TestPricingRefreshHTTPAuthorizationAndRestartCache` covers real authentication, administrator checks, CSRF, manual refresh and restart. This mechanism does not implement vendor-price collection or website publication.
