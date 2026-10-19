# QQ chat channel

Open **More → Switch chat channel** in an assistant chat. With no external channel bound, the drawer shows a channel list. Select QQ to slide into its details. A bound channel opens directly with its binding status. Disconnecting automatically returns to the list. Other channels use the same navigation.

## Connect with a QR code

Choose **Connect QQ with QR code**, scan with mobile QQ, then select or create a dedicated bot and authorize it. The QR code is rendered locally. The Runtime receives and encrypts the app secret; it is never returned to the browser.

When QQ returns your user identity, only your private messages to that bot enter the assistant. If QQ omits the identity, send the complete displayed pairing code to the bot in a private chat within ten minutes. WebUI input remains available until pairing, and the pairing message does not trigger the assistant. Refresh an expired QR code. Returning to the list or closing the drawer cancels the page's pending QR session.

Each bot binds to one assistant per Molr instance. Disconnect another bound channel or cancel its pending pairing before connecting QQ.

## Manual credentials

Choose **Enter credentials manually**, create a bot on the [QQ Open Platform](https://q.qq.com/qqbot/openclaw/login.html), and enter its App ID and App Secret. Send the displayed pairing code in a QQ private chat. Never send the App Secret in chat. An old pairing code cannot be displayed after refreshing the page; cancel and connect again.

## Messages and disconnecting

Private text messaging is supported. Incoming QQ attachments are explicitly marked unread; upload files in Molr. Published attachments and cards become text and authenticated Molr links. Recipients need access to the Molr address. Group chats, native media transfers and streaming messages are not supported yet.

QQ handles chat input after binding; WebUI retains history, tasks, files and permission management. Existing history is not forwarded. Disabling proactive bot messages in QQ may prevent scheduled reminders and replies beyond the response window. Molr shows failed or unconfirmed deliveries; unknown outcomes are never automatically resent.

Choose **Disconnect QQ** and confirm to restore WebUI input and return to the channel list. History and context are preserved, unsent replies are canceled, and the QQ bot itself is not deleted. Channel messages are transmitted to Tencent.
