# WeChat Chat Entry

After an assistant is created, it defaults to chatting in the WebUI. Open the "..." menu in the top-right corner of the chat page and select "Switch chat channel". Select Weixin in the drawer, then click "Connect WeChat", scan the QR code with WeChat, and confirm. If WeChat requires a pairing verification code, enter the code on the page.

The QR code page will indicate: Chat messages and attachments sent and received via the WeChat channel will be sent to Tencent, and Tencent may retain this data. Please be mindful of data privacy.

The entry switches only after the QR code is confirmed and the binding is successfully saved. Connection failures, cancellations, or expired QR codes do not affect the existing WebUI chat. If the current assistant is already connected, it will prompt that re-scanning is unnecessary. If WeChat returns that it is already bound, you need to verify the current assistant's local binding and credentials; it will not automatically take over connections from other assistants.

## Usage After Connection

- Send messages in WeChat and receive assistant replies; send a text message first to establish the reply context.
- The WebUI displays the same chat history, retaining tasks, files, configurations, and permission approvals, but no longer provides chat input. The server also rejects regular message-sending API requests for this session.
- Records and assistant context prior to binding are retained; old records are not retroactively sent to WeChat.
- Each WeChat identity can only bind to one assistant within a single Molr instance; cloning an assistant does not copy the WeChat binding. Sending and receiving are paused after an assistant is dismissed and resume after re-hiring.
- Supports two-way sending and receiving of text, images, and files. WeChat images and files are saved to the attachments of the same session; videos can be received as attachments. Voice messages transcribed by WeChat are processed as transcribed text; voice messages without transcription are received as audio attachments, and supported SILK voice messages are automatically converted to WAV. Understanding voice requires enabling a model that supports audio; if conversion fails, the original audio is retained.
- Images explicitly published by the assistant to the chat are sent as image messages, while other attachments are sent as file messages; other files in the workspace are not automatically uploaded. Single attachments are subject to the Runtime upload limit and storage quota, with a channel limit not exceeding 100 MiB. Receiving a file does not mean the model has read it; understanding images and files also depends on the selected model and file format.
- Supports WeChat quoted replies, including text, partial text, and attachments. Quotes containing only the message ID rely on local cache for restoration; if not cached, expired, or if the attachment has been deleted, this will be explicitly stated, and WeChat history prior to integration cannot be automatically restored.
- WeChat does not display Molr interactive cards. During connection, the model receives channel prompts and prioritizes text; necessary cards are converted into titles, summaries, and Molr links. Approvals are still completed in Molr.

## Connection Status and Delivery

Automatic reconnection occurs during network interruptions. After a failure, the wait time starts at 5 seconds and doubles with each attempt, up to a maximum of 640 seconds; the wait interval resets after a successful recovery. When "Re-scan Required" is indicated, reconnect in the "Chat Entry" popup; during this period, the chat entry remains WeChat. Scheduled or background replies may fail due to expired WeChat reply credentials, and proactive reminders after long periods of no interaction cannot be guaranteed.

When sending is successful, the chat page does not display delivery prompts. Exceptions such as waiting for new WeChat messages, sending restrictions, attachment failures, platform rejections, or unknown sending results will still be displayed. "Result Unknown" does not mean sending failed; if already received in WeChat, it can be ignored, and such messages will not be automatically resent. Delivery exceptions are displayed in the current chat; multiple reminders are closed one by one and viewed sequentially. Closing via the top-right corner saves "Ignored" on the server side; refreshing, re-logging in, or switching devices will not proactively remind you of the same exception again. Historical messages can still be viewed in the chat history. New exception states or new message delivery exceptions generate new notifications. Closing does not resend, cancel, or modify delivery results. When waiting for reply context, you can send a new message in WeChat. Interface success does not indicate that the user has read the message. Rate limit prompts display only one message, which can be closed; wait for the interval specified by the server before retrying.

If attachment uploads encounter temporary network or server errors, they are attempted up to three times; this only retries the upload step and does not resend messages already submitted to WeChat.

Text segments and individual attachments for each public reply are recorded independently for delivery; assistant tasks are not re-executed due to sending failures. Long texts are split into multiple WeChat messages. Existing regular friend chats and group chats within WeChat are not within the scope of integration.

When bound, the message input box at the bottom of the chat page retains its outline in a locked style, displaying instructions for sending via WeChat and restoring input upon unbinding; clicking "Manage WeChat Connection" inside the box directly opens the same popup. Abnormal connection states such as "Re-scan Required" are displayed as prompt blocks in the popup. During automatic reconnection and when re-scanning is required, system reminders at the top of the chat will prompt; clicking "View Details" opens connection management. Automatic reconnection reminders do not require re-scanning; closing the reminder does not stop background retries, the same interruption will not trigger repeated reminders, and it is automatically cleared after connection recovery or unbinding.

## Unbinding

Click "Unbind WeChat" in the "Chat Entry" popup and confirm to restore WebUI chat. History and assistant context are retained; pending WeChat replies are canceled; messages already accepted by the platform cannot be recalled. Local unbinding stops Molr from using the binding credentials but does not revoke all authorizations on the WeChat server side.


Connection and link configuration is maintained by the administrator; see [WeChat connectivity](../../admin/deployment/configuration.md#wechat-connectivity).
