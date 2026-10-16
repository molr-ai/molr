# Telegram channel

The fixed adapter in `runtime/internal/im/telegram` uses the official [Bot API](https://core.telegram.org/bots/api). See the [user guide](../../user/chat-channels/telegram.md) and [HTTP API](../reference/http-api.md#telegram-chat-channel).

Background connections and queues use [shared commit notifications and deadline scheduling](../architecture/overview.md#chat-channel-background-scheduling), without polling local binding or message collections while idle.

## Binding and authorization

Each assistant binds a dedicated bot and one Telegram private-chat user. `getMe` validates the token; `getWebhookInfo` checks existing consumers without deleting their webhooks. A five-minute, single-use random `/start` code identifies a candidate user. Only an authenticated WebUI confirmation with assistant write permission activates the binding. Pending connections live in memory and expire on cancellation or restart.

The binding transaction rechecks authorization, assistant state, bot uniqueness and other channels. `app.chatChannels` dispatches conversation/file authorization, WebUI input checks and Publish by the current binding, rejecting ambiguous multiple bindings. Weixin also checks Telegram conflicts when committing a binding.

Tokens use AES-GCM with channel, owner and binding ID as AAD. Status, notices, model context and logs exclude tokens. Because Telegram URLs contain tokens, HTTP errors expose fixed reason codes rather than request URLs or server descriptions.

## Receiving and delivery

One worker per bot performs 20-second long polls, accepting only the bound user ID and chat ID in a private chat without a topic ID. Other messages do not wake the assistant. Files download outside transactions through `files.ChannelStore`; the final transaction rechecks the binding and atomically writes messages, deduplication records and the next offset. Polling never waits for model execution. Albums remain separate messages; edit events are not subscribed.

Only assistant Publish creates deliveries, in the message transaction without consuming the scheduler's IM Outbox. Plain text is split into at most 4000 UTF-16 units; each attachment has a separate delivery. Accepted sends retain Telegram message IDs. Before sending, state becomes durable `sending`; success becomes `accepted`, while timeouts or incomplete replies become `unknown`. Startup converts leftover `sending` to `unknown` without resending.

Each binding sends one ordered part per round, at least one second between rounds, with at least 40 milliseconds between bindings. HTTP 429 waits for `retry_after`, defaulting to one minute, for at most five attempts. HTTP 401 requires a new token; 409 stops competing polling; 403 marks the bot blocked until a new inbound user message. Poll failures use exponential backoff up to 640 seconds. Disconnect serializes with sending and cancels unfinished deliveries. All channel workers stop before database closure.

## Files and networking

Downloads are capped at 20 MB and uploads at 50 MB, further restricted by deployment upload limits. Eligible JPEG/PNG files use `sendPhoto`; other files use `sendDocument`. Download URLs have a fixed Telegram HTTPS origin; unsafe paths and redirects are rejected. Requests have time and response-size limits. Attachment failures preserve available text and explicitly mark unread contents.

The default public-address guarded client ignores environment proxies. Administrators may set an HTTP(S) proxy using `telegram.proxy_url`, without URL credentials, paths, queries or fragments. This is a deployment-trusted network egress; destination TLS still authenticates Telegram. Bot management endpoints cannot modify proxy or API origins.

## Validation

From the Runtime directory run `go test ./internal/im/telegram ./internal/im/weixin ./internal/app ./internal/api ./internal/config`. Tests cover browser confirmation, cancellation/expiry, channel exclusion, authorization, deduplication/cursor rollback, files, revocation, rate limits, uncertain sends, restart recovery and credential redaction. WebUI tests simulate channel endpoints for connection, locked input and disconnection. Real Telegram presentation and network reachability require a dedicated bot acceptance test.
