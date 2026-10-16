# Slack channel

The fixed Go adapter in `runtime/internal/im/slack` uses Socket Mode and the Web API. It does not load a resident plugin. Setup is described in [Slack chat entrance](../../user/chat-channels/slack.md).

## Identity and ingress

`auth.test` and `bots.info` verify workspace, bot and app identity; the Socket Mode hello must match that app. Bindings fix the Molr owner, assistant, existing conversation, Slack workspace, app, bot, member and DM. Creation and pairing confirmation recheck assistant authorization and competing chat entrances.

Pairing expires after five minutes. AES-GCM protects credentials with owner/binding AAD; only the pairing code hash is stored. WebUI input remains available until the nominated member sends the correct code in a DM. One app or workspace member can bind only one assistant. Other processes must not consume Socket Mode for that same app.

Ingress validates workspace, app, member, DM and message type, excluding bots, group messages, edits and deletions. `ReceiveChannel` and attachment operations recheck binding, account and assistant state inside transactions. No user session or full-access token is created. Weixin, Telegram and Slack occupancy checks run inside binding transactions.

## Durable receiving and delivery

Private storage uses `slack.bindings`, `slack.incoming`, `slack.deliveries` and quote caches. Socket events are deduplicated by binding generation and event ID and committed before ACK. Downloads and assistant execution run later. The incoming message, completion marker, quote and visible event commit together; completed inbox records discard their raw content. Disconnect cancels queued input and output; revoked bindings cannot write.

Explicit Publish creates independent deliveries in the message transaction without consuming the scheduler’s IM outbox. Text splits at UTF-8 boundaries into at most 3500 bytes per part; attachments get individual deliveries. Each binding sends in order, at most once per round, with at least one second between rounds. Sending is persisted before the request. Success requires a complete Slack `ok: true` response, plus a server ts for text. Timeouts, 5xx and malformed/incomplete responses become unknown and are not retried automatically. Restart converts sending to unknown. HTTP 429 honors Retry-After with at least one minute between attempts and at most five attempts. Notifications commit with delivery state.

Socket Mode renews connections and uses shared backoff after network errors. Invalid credentials or missing permissions require reconnection. Disabled accounts and dismissed assistants stop channel work. Runtime cancels and joins workers before closing storage.

## Attachments and quotes

Private downloads accept HTTPS files.slack.com only. Credentials never follow redirects; the existing public network guard checks actual DNS/IP destinations. Upload uses files.getUploadURLExternal, byte upload, then files.completeUploadExternal. Upload URLs are restricted to Slack domains and receive no bot token. Uncertain sharing completion is not retried.

ChannelStore enforces size, quotas, ownership and binding checks. Images, files, audio and video enter the existing attachment system. Audio is read by an available capable model without another transcription service. Unread content is explicitly marked.

DM thread_ts supplies a locally cached parent quote only; it neither creates a separate conversation nor fetches history. The cache is binding-scoped, limited to 4096 entries, 30 days and 16 KiB of text per entry. Quoted attachments recheck binding permissions. Disconnect clears it. Cards become text and Molr links; live chat_channel context explains capabilities and delivery boundaries.

## HTTP endpoints and verification

- `GET /api/assistants/{id}/slack`: status and the latest 30 deliveries, without credentials or pairing codes.
- `POST /api/slack/connect`: `AssistantID`, `AppToken`, `BotToken`, `UserID`; returns `ID`, `Code`, `ExpiresAt`.
- `POST /api/slack/disconnect`: `AssistantID`; cancels pairing or removes a completed binding.

Run `go test ./internal/im/slack` and `go test ./internal/app -run 'Test(Slack|Telegram|Weixin)'` for protocol, isolation, attachments, deduplication and channel exclusivity. Simulations do not replace real Slack installation, pairing, client media rendering and long-connection acceptance.
