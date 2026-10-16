# DingTalk chat entry

Connect an application robot to one Molr assistant for private chats within the organization. Each robot binds to one assistant in a Molr instance. Only the paired user's messages are accepted; group chats are excluded.

## Prepare a robot

Create an application in the [DingTalk developer console](https://open-dev.dingtalk.com/), enable its robot, select **Stream** mode, grant robot private-message sending permission, configure application visibility and publish. Receiving images and files also requires robot file-download permission. The operator needs the organization's developer permissions.

Prepare the application's Client ID (AppKey) and Client Secret (AppSecret). Runtime connects outward to DingTalk; no public callback URL is required. The host must reach DingTalk HTTPS, WSS and attachment services. Do not run multiple receivers for the same robot; use a separate test robot for acceptance.

## Connect and pair

1. Open the assistant chat menu → **Switch chat channel** → **Connect DingTalk**. Disconnect any other chat channel or cancel its pending pairing first.
2. Enter the application credentials and connect. Credentials are encrypted and excluded from assistant context and status responses.
3. Find the robot in DingTalk and send the one-time code as a **private text message**. The code expires after ten minutes. Do not share it.
4. After pairing, the page shows **Connected**, the robot sends a confirmation, and the WebUI composer locks. Send subsequent messages in DingTalk.

WebUI chat remains available before pairing. The code is displayed only when created. If the page was closed before pairing, cancel pairing and reconnect. Credential, permission or Stream configuration errors appear as connection issues; correct the configuration, disconnect or cancel, and reconnect. Use the same flow to replace credentials.

Messages and attachments pass through DingTalk. The connection confirmation still requires sending permission: **Connected** means identity pairing succeeded; delivery is tracked separately.

## Chat and attachments

- Send and receive text and Markdown. Long replies are split. Existing history and assistant context remain; earlier messages are not forwarded.
- Receive images, files, rich-text images and video attachments. Voice uses DingTalk's transcript when available, otherwise an audio attachment. Understanding depends on format and model capabilities; not every DingTalk voice format is supported by models.
- Explicitly published images and other attachments are sent as **downloadable files**, not native image messages. Attachments obey Runtime upload limits and quotas, capped at 20 MiB for this channel. DingTalk's own size and format restrictions also apply.
- Failed attachments are explicitly marked unread or undelivered. Successful text delivery does not imply attachment success.
- DingTalk interactive cards are unsupported. Necessary Molr task or permission cards become text summaries and login-required Molr links. Approvals stay in WebUI; the phone must be able to reach its address.
- Background replies use application APIs and do not depend on a temporary SessionWebhook. Delivery still depends on visibility, credentials, permissions and platform limits.

## Errors and disconnection

Connections reconnect automatically, with consecutive failures backing off from 5 to 640 seconds. Rate-limited sends allow at most five attempts, waiting at least one minute or the platform's longer retry interval. Timeouts and incomplete responses remain **unconfirmed** and are not automatically retried. Check DingTalk first. Platform acceptance is not a read receipt.

Dismissing an assistant or disabling its owner pauses traffic; restoring them resumes it. Cloning an assistant does not copy its binding. Channels are exclusive: ordinary chat writes through WebUI, SDK and plugins are rejected while bound; reading history, managing files and configuration, and approvals remain available.

Choose **Disconnect DingTalk** and confirm to cancel pending replies, remove this connection's credentials and restore WebUI input. Messages already accepted by DingTalk cannot be recalled. Local disconnection does not delete the DingTalk application or revoke its platform permissions.
