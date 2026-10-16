# DingTalk channel

The fixed Go adapter in `runtime/internal/im/dingtalk` receives private robot messages through Stream and sends replies with an application access token. See [DingTalk chat entry](../../user/chat-channels/dingtalk.md). Protocol references are the [official Stream description](https://open-dingtalk.github.io/developerpedia/docs/learn/stream/protocol/), [message structures](https://open-dingtalk.github.io/developerpedia/docs/learn/bot/message/) and [official channel SDK](https://github.com/DingTalk-Real-AI/dingtalk-channel-sdk-go), checked on 2026-10-16.

Background connections and queues use [shared commit notifications and deadline scheduling](../architecture/overview.md#chat-channel-background-scheduling), without polling local binding or message collections while idle.

## Binding and trust boundaries

Each application binds to one assistant per instance. Pairing records contain a random binding generation, application ID, a hash of a ten-minute pairing code and AES-GCM encrypted credentials with owner/binding AAD. Status DTOs exclude credentials, tokens and codes. The code appears only in the creation response, never in chat or model context; duplicate pairing callbacks are deduplicated. Workers remove expired unpaired records.

Pairing accepts only `conversationType=1`, the configured robot, the robot's organization and an identified employee. Once paired, organization, employee ID, sender ID and private conversation ID must all match. The binding transaction rechecks account/assistant status and exclusivity with Weixin, Telegram and Slack; other channels also check active DingTalk bindings and unexpired pairing reservations. Binding generations isolate revoked connections and late callbacks.

Ingress uses trusted `conversation.ReceiveChannel` and `files.ChannelStore`, without issuing user sessions or API tokens. Ordinary WebUI/SDK/plugin writes pass both channels' `CheckWebInput`. Message and deduplication records commit atomically. Downloads run outside transactions, with binding rechecks at file commit and message association.


## Transport and delivery

The bounded Stream transport uses the existing `golang.org/x/net/websocket` dependency. Endpoints must be DingTalk WSS on port 443. HTTP and WebSocket dialing enforce public IP checks; credentials never follow redirects. Frames are capped at 1 MiB, with a 32-item callback queue and independent heartbeat handling. Shutdown waits for callbacks before closing storage. Queue overflow reconnects; server replay of robot messages is not guaranteed.

`Published` creates independent `dingtalk.deliveries` inside the message transaction, without consuming the scheduler's IM outbox. Text splits at UTF-8 boundaries into at most 3500 bytes; each attachment has its own delivery. Images are sent as files without exposing Molr file URLs. Cards become text and login-required links; no card callbacks are registered.

Sends persist `sending` first. Acceptance requires `processQueryKey` and no invalid or rate-limited recipient. Explicit throttling permits at most five attempts, waiting at least one minute and respecting Retry-After. Timeouts, 5xx and incomplete responses become `unknown`, with no automatic retry. Restart changes abandoned `sending` records to `unknown`. Credential errors suspend connections; disconnection cancels unfinished deliveries. Sending and disconnection serialize per owner/assistant; already transmitted requests cannot be recalled.

Application tokens are cached by credentials and refreshed before expiration. Attachment URLs come from the robot download API and must use HTTPS, DingTalk or Alibaba Cloud domains and public IPs. Redirects are disabled and application tokens are not attached. The channel upload cap is the smaller of the Runtime limit and 20 MiB. Upload/read failures remain separate from uncertain final sends. Connection and delivery issues use unified notifications.

## API

| Method and path | Request | Response |
| --- | --- | --- |
| `GET /api/assistants/{id}/dingtalk` | Assistant ID | Channel, state, application ID, safe error code and timestamps |
| `POST /api/dingtalk/connect` | `AssistantID`, `ClientID`, `ClientSecret` | `Code`, `ExpiresAt` |
| `POST /api/dingtalk/disconnect` | `AssistantID` | Empty result; cancels pairing or disconnects |

Endpoints enforce authenticated ownership, assistant authorization, CSRF and HTTP limits. Paired connections must be disconnected before replacing credentials. Pending pairing does not lock WebUI. `dingtalk.changed` refreshes status; `chat_channel` context describes text, attachment and approval behavior.

## Verification boundary

Go tests cover identity, group rejection, pairing expiration/cancellation, deduplication, file isolation, unknown delivery, throttling, heartbeat and shutdown. WebUI tests simulate platform callbacks and cover pairing, input locking, disconnection and six palettes in light/dark desktop/mobile layouts. Real organization permissions, robot discovery, client file rendering and long-idle proactive messaging require a test account; controlled protocol tests do not establish those platform behaviors.
