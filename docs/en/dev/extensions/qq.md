# QQ channel

The fixed Go adapter in `runtime/internal/im/qq` receives `C2C_MESSAGE_CREATE` over QQ's official WebSocket gateway and sends private messages using access-token authentication. See [QQ setup](../../user/chat-channels/qq.md).

Protocol review date: 2026-10-19. QR registration follows Tencent's `@tencent-connect/qqbot-connector@1.2.0`; gateway fields follow `@tencent-connect/qqbot-nodejs@1.0.5` and the [official WebSocket documentation](https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/event-emit/websocket.html). The adapter implements the protocol directly without installing or running an OpenClaw plugin.

## Binding and identity

The Runtime generates a random 32-byte key and obtains a task ID from `https://q.qq.com/lite/create_bind_task`. The browser receives only a local login ID, QR URL and expiry. `/lite/poll_bind_result` returns an app ID, encrypted secret and optional user OpenID. AES-256-GCM verifies and decrypts the secret. Keys remain in bounded, short-lived in-memory sessions scoped to the owner and assistant. Cancellation, expiry, authorization and other channel bindings are checked before committing.

A returned OpenID binds the scanning user. Otherwise, and for manual credentials, a ten-minute private-chat pairing code establishes identity. Only its digest is stored. Pending pairing reserves the channel without locking WebUI input. Pairing messages are deduplicated and excluded from history. Bots are instance-unique; other senders and non-C2C events are ignored. App secrets are encrypted with channel, owner, binding and app ID in AAD. DTOs, notifications and errors exclude credentials.

## Delivery and recovery

`channelworker` uses commit notifications and deadlines for connections and delivery. Gateway handling includes authentication, heartbeats, reconnect backoff and RESUME. Sequence advances only after inbound commit. Deduplication uses app ID and remote message ID; binding generation is rechecked transactionally.

Only Publish enqueues delivery. Text splits at 4000 UTF-8 bytes without breaking characters. Published attachments and cards become Molr links. Incoming QQ attachments are marked unread and are not downloaded.

QQ's overview specifies a 60-minute passive reply window and four replies per incoming message, while the send endpoint's `msg_id` description still says five minutes. The adapter conservatively uses passive replies within four minutes of receipt, up to four times, then uses proactive messages. Sends are spaced at least three seconds per binding. Proactive delivery depends on the user's QQ settings and platform limits; see [message rules](https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/overview.html).

Delivery persists `sending` before network I/O and `accepted` on success. Uncertain responses and abandoned `sending` records become `unknown` and are not automatically resent. Explicit rate limits honor Retry-After, with at most five attempts. Disconnect serializes with sending and cancels unfinished delivery. Molr continues to authorize files and approvals; acceptance does not mean the user read a message.

## API and verification

`GET /api/assistants/{id}/qq` returns status. `POST /api/qq/login`, `poll`, `cancel`, `connect` and `disconnect` manage QR login, polling, cancellation, manual connection and disconnection. Requests include `AssistantID`; poll and cancel include login `ID`; manual connection includes `AppID` and `AppSecret`. Authentication, assistant permissions, CSRF and rate limits apply.

Run `go test ./internal/im/qq ./internal/app ./internal/api` and `npm run test:webui -- qq.spec.ts weixin.spec.ts telegram.spec.ts slack.spec.ts dingtalk.spec.ts feishu.spec.ts`. Protocol tests cover authenticated QR decryption, pairing, deduplication, authorization, revocation, mutual exclusion, rate limits and uncertain sends. UI tests simulate QQ responses. Real QR authorization and QQ display require a dedicated bot and user confirmation.
