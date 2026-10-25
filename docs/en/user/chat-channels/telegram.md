# Telegram chat channel

Choose **Connect Telegram** in the chat menu **… → Switch chat channel**. Each assistant uses a dedicated bot and accepts only the bound user's private messages. An assistant has one external chat channel; disconnect Weixin before connecting Telegram.

## Connect

1. Open [@BotFather](https://t.me/BotFather) in Telegram and use `/newbot` to create a bot and obtain its Bot Token.
2. Enter the token in Molr and select **Verify and continue**. The Runtime encrypts the stored token; it is never returned to the UI or included in model context.
3. Select **Open Telegram** or scan the QR code, then press Start in the bot chat.
4. Return to Molr, check the Telegram user name and ID, and select **Confirm connection**. Links expire after five minutes; start again after cancellation, expiry or a Runtime restart.

WebUI chat remains available until confirmation. Connecting preserves history and assistant context while switching chat input to Telegram. Messages and attachments pass through Telegram; existing Molr history is not forwarded. Do not paste Bot Tokens into chat messages.

A bot already bound to another assistant or configured with another service's webhook is rejected. Use a dedicated bot to avoid competing update consumers.

## Use

Text, images and files work in both directions. Long replies are split into messages. Incoming files belong to the current conversation; understanding them depends on the model and file tools. Failed attachments are explicitly marked as unread. Album images arrive as separate input messages. Groups, private-chat Topics, history synchronization, edited-message synchronization and personal-account automation are outside the current scope.

General file uploads are limited to 50 MB and downloads to 20 MB, also subject to Runtime upload and storage limits. Eligible JPEG/PNG attachments are sent as photos; other attachments are sent as documents. Voice and video can be stored as attachments; there is no dedicated transcription pipeline.

New published replies from background tasks are also delivered to Telegram. Task and permission cards become a title, summary and authenticated Molr link. Approvals remain in Molr, whose address must be reachable from your phone.

## Status and disconnect

Network failures reconnect automatically with delays doubling from five to 640 seconds. Telegram retains uncollected updates for at most 24 hours; long outages can lose messages. Dismissed assistants and disabled accounts stop receiving and sending; rehiring resumes the assistant. Cloning does not copy bindings.

If the bot is blocked, unblock it in Telegram and send a new message. For invalid tokens or competing services, resolve the issue and disconnect/reconnect as instructed. Rate limits use Telegram's retry delay, with at most five attempts. Unconfirmed send results are marked unknown and are not automatically resent. Success means platform acceptance, not a read receipt.

Disconnect in **Manage Telegram connection** to restore WebUI input. History and context remain; pending deliveries are canceled. Already accepted messages cannot be recalled. Local disconnection does not revoke the token; use @BotFather to revoke it.
