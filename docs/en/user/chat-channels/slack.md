# Slack chat entrance

Connect Slack from an assistant’s **Switch chat channel** dialog. Only the selected member’s direct messages to the bot enter that assistant. Existing history and context remain; use WebUI for viewing, management and approvals.

## Prepare a Slack app

Create a separate [Slack App](https://api.slack.com/apps) for each assistant and install it into the target workspace. Do not connect the same app from another Runtime or program: Socket Mode distributes events between connections.

1. Enable Socket Mode. Under Basic Information, create an App Token with `connections:write`.
2. Add Bot Token Scopes: `im:history`, `chat:write`, `users:read`, `files:read`, `files:write`.
3. Enable Event Subscriptions and subscribe to the `message.im` bot event.
4. Enable the App Home Messages Tab and allow users to send messages.
5. Install the app; reinstall after changing permissions. Save the App Token and Bot User OAuth Token.
6. Copy your member ID from your Slack profile menu.

Runtime requires outbound HTTPS and WebSocket access to Slack, without a public callback URL. See [Socket Mode](https://docs.slack.dev/apis/events-api/using-socket-mode/).

## Connect and chat

Open **Switch chat channel** from the assistant chat menu. Disconnect Weixin or Telegram first if connected. Enter both tokens and your member ID in the Slack section. Click **Connect Slack**, then send the complete displayed pairing code to the bot from that Slack account in a DM within five minutes. The pairing message does not trigger the assistant.

After confirmation, the WebUI composer is locked. Previous messages are not forwarded. Runtime keeps receiving and sending after the page closes. A Slack app or a member within the same workspace cannot be bound to another assistant. Cancel unfinished pairing; if the code is lost, cancel and connect again.

- Text, images and ordinary files work in both directions. Audio and video arrive as attachments; understanding audio depends on the assistant’s model capabilities. No external transcription service is called automatically.
- DM thread replies include locally cached parent text and attachments as quoted context. All input remains in one assistant conversation. Missing, expired or unavailable references are marked explicitly; Slack history is not fetched. Replies go to the same DM.
- Attachment limits use the smaller of the administrator’s setting and 100 MiB, defaulting to 20 MiB. Partial failures preserve text and successful attachments and mark unread failed content.
- Task and permission cards become text and authenticated Molr links. Complete approvals in WebUI.

Slack transmits and stores messages and attachments. Binding credentials are encrypted and excluded from assistant context.

## Failures and disconnecting

Network failures reconnect automatically with a notification that clears on recovery. For expired credentials or missing permissions, check app settings, disconnect and reconnect. Rate limits honor Slack’s retry interval, with up to five attempts. A timeout or incomplete acknowledgement is an unconfirmed delivery and is not retried automatically; check Slack first.

Disconnecting restores WebUI input, preserves history and context, and cancels unsent replies. Requests already sent cannot be recalled. Dismissal pauses the channel; rehiring resumes it. Group messages, other members and bot messages never enter the assistant.
