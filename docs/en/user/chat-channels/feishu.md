# Feishu chat channel

In an assistant chat, open **More → Switch chat channel → Connect Feishu**. Scan the QR code with Feishu and confirm app creation and private message permissions. Once connected, find the application bot in Feishu and send text.

Each application bot binds to one Molr assistant and accepts private messages only from its bound user. Chat input moves to Feishu; history, tasks, files and permission management remain available in WebUI. Existing history is not forwarded and assistant context is preserved. An assistant can bind only one chat channel at a time.

## Use an existing app

Choose **Use an existing app** and enter its App ID, App Secret and your app-scoped Open ID. Use a Feishu custom app with bot capability, grant `im:message.p2p_msg:readonly` and `im:message:send_as_bot`, and subscribe to `im.message.receive_v1`.

After submitting the connection, select and save long-connection event delivery in the Feishu developer console, publish the app, and ensure your account is in its availability scope. Saving the subscription method requires an online Molr client. Runtime encrypts credentials; status responses never expose App Secret.

Runtime needs outbound access to Feishu, with no public inbound endpoint. Do not connect the same application from another instance: events may be distributed to that instance. QR registration currently supports Feishu tenants, not international Lark tenants. Tenant policy determines whether you can create an app.

## Messages and status

Text and textual rich posts are supported. Images, files, voice and other inbound formats are marked unread; send a text description instead. Published attachments and cards are represented by a Molr link requiring login to view or act. Group chat and interactive cards are not supported.

Channel content is transmitted to Feishu. Disconnections trigger automatic reconnection and notices. Pausing the assistant stops input and output; reactivating it resumes the channel. Delivery errors produce notifications. Unconfirmed and rate-limited sends are retried within bounded attempts and the deduplication window. After that, retries stop and the recipient should check Feishu.

## Disconnect

Open chat channel settings and choose **Disconnect Feishu**. This restores WebUI input, preserves history and context, and cancels unsent replies. Messages already accepted by Feishu cannot be recalled. Disconnecting does not delete the Feishu app; manage it in the developer console.

For assistant access to calendars and documents, see [Operate Feishu](../best-practices/feishu.md). That authorization is separate from the chat channel.
