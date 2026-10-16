# Feishu channel

The fixed adapter in `runtime/internal/im/feishu` uses the official Go SDK v3.12.2 WebSocket client. Registration follows that version's `scene/registration` protocol through a bounded HTTP client with timeouts, response limits and fixed domains. See [Feishu chat channel](../../user/chat-channels/feishu.md) for user instructions.

Background connections and queues use [shared commit notifications and deadline scheduling](../architecture/overview.md#chat-channel-background-scheduling), without polling local binding or message collections while idle.

## Identity and lifecycle

An application binds to one assistant owned by one account. The binding stores the application ID, app-scoped Open ID, tenant from the first valid event and generation ID. App Secret is AES-GCM encrypted with account, binding and application AAD. Input must match these identities and have sender type `user` and chat type `p2p`. Group and bot messages are ignored.

Connection changes, disconnects and sends are serialized per owner and assistant. Binding transactions recheck authorization and channel exclusivity. Input uses `ReceiveChannel` with transactional authorization, without creating a user Session or Token. Ordinary WebUI and SDK message input is blocked while bound. Revoked generations cannot restore connections or write messages; canceled registrations cannot bind. Pending QR sessions are memory-only and must restart after process restart.

Runtime owns the WebSocket clients. Connection transitions update `feishu.changed` and unified notifications. Disabled accounts and inactive assistants stop workers, while every receive and send also checks live authorization. Shutdown cancels and joins channel workers before closing storage.

## Persistence and messages

Private keyspaces are `feishu.bindings`, `feishu.incoming` and `feishu.deliveries`. Input deduplicates by application, tenant and platform `message_id`, independently of binding generation to prevent delayed replay after rebinding. Chat insertion, deduplication and tenant pinning commit atomically; rollback permits redelivery. Receiving is limited to two seconds and never waits for model execution.

Explicit assistant Publish creates deliveries in the same transaction. Text is split at UTF-8 boundaries into at most 12000-byte segments; history before binding is not forwarded. Each segment has a stable UUID. Success requires a valid business success code and server message ID. Unknown outcomes and throttling allow at most five attempts, spaced one to four minutes apart. Retries stop 55 minutes after the initial attempt, leaving margin within the platform's one-hour deduplication window. Crash-left `sending` records become `pending` inside the window and `unknown` outside. Per-binding delivery follows conversation sequence and segment order. Disconnect cancels queued output.

Published attachments and cards become text with a Molr link requiring login. Rich posts contribute textual content; other input types are marked unread and attachments are not downloaded. The `chat_channel` context describes these limits. Private model streams are not forwarded.

## Validation scope

Protocol tests use synthetic HTTP responses to verify requested scopes, message JSON, stable UUIDs and errors. SQLite tests cover input filtering, duplicates, rollback, revocation, registration cancellation and crash recovery. WebUI tests use an isolated Runtime and simulated Feishu responses. Real tenant registration, administrator approval, publication scope and client rendering require real-account acceptance.

References: [Official SDK](https://github.com/larksuite/oapi-sdk-go/tree/v3.12.2), [Receive messages](https://open.feishu.cn/document/server-docs/im-v1/message/events/receive), [Send messages](https://open.feishu.cn/document/server-docs/im-v1/message/create).
