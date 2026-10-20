# Personal Settings

On desktop, click the avatar at the bottom of the sidebar; on mobile, click the avatar and username at the top of the assistant page to enter user settings. Administrators can enter the management page via the "Admin Settings" button in the top-right corner of the user settings page header. For operations, see [Managing Users and User Groups](../../admin/administration/users-and-groups.md).

## Profile and Avatar

Set your display name and avatar in User Settings → Profile. You can choose a preset avatar, or upload a PNG, JPG, or WebP image not exceeding 10 MB and 40 million pixels; the image will be center-cropped into a 256 × 256 thumbnail within the browser. After clicking "Save", the new avatar will be displayed in the desktop sidebar and at the top of the mobile view; after clearing and saving, the first character of the name will be restored. The avatar is saved with the account and remains available after logging in on a different device.

## Language, Theme, and Preferences

General settings manage preferences such as language, time zone, and currency. "Show Plugins" is disabled by default; when enabled, it displays plugin navigation, plugin themes, and related management options, with settings synchronized across the account. For specific scope, see [Plugins](plugins-and-skills.md). The theme page distinguishes between interface theme, color scheme, and light/dark mode. Once saved to the account, it can be accessed on other devices. Two built-in themes, Default and Test, are provided. You can also select [Plugin Themes](plugins-and-skills.md) that have been installed, authorized, and enabled by the administrator. Theme color schemes include Forest, Sea Salt Blue, Iris Purple, Warm Sand, Rose, and Plain Ink, all supporting light and dark displays. Plain Ink uses a neutral grayscale interface, while semantic colors are retained for success, warning, error, and other notifications.

For viewing, editing, and dreaming time of memories, see [Memory Guide](memory.md). For selecting default models and assistant models, see [Permissions and Budget](permissions-and-budget.md#selecting-models).

## Usage

User Settings → Usage opens with this year's statistics. Choose this month, the last 30/90 days, a previous year, or a custom range of up to one year, and filter by assistant and model. The overview shows input, output, cached input, total tokens, confirmed model spend, model calls and active days. Total tokens are input plus output; cached input is already included. Calls without reported usage are identified separately, not treated as zero consumption.

Switch the daily calendar between tokens and spend. Heatmap colors follow the selected palette and light/dark mode. Hover or focus a date to see the date and exact value of the selected metric on separate lines, with unknown usage identified separately. Use arrow keys to move between dates and select a date for its breakdown. Desktop uses weekly columns with fixed-size day cells. When space is limited, it shows the most recent weeks through today, or through the end of a historical range. Narrow the date filter to inspect earlier dates. Mobile uses a monthly calendar. Group the table by assistant or model and sort descending by tokens, spend or calls. Select a name to filter further. Dismissed assistants retain their history. Selecting a day changes only the breakdown; the overview retains the full filtered range.

Dates use your account timezone, and late receipts remain attributed to the original call date. Costs are aggregated in their recorded currencies and converted using one exchange-rate snapshot into your preferred currency; converted amounts carry `≈`. Zero-cost calls can still consume tokens. Running calls and unknown costs are listed separately and excluded from confirmed spend. Lifetime usage and allowances is expanded by default and shows lifetime calls, reserved spend, attachment/context storage and current member allowances, independent of historical date filters.

## System Notifications

WeChat delivery/connection anomalies, persistent assistant blockages, authorization requests, pending plan handovers, and task/background execution failures are managed centrally by the server for notification status. Background execution failure notifications display "View in Development Workbench" when the development workbench is available and display is configured; clicking this locates the corresponding execution. When display is not configured, the notification and close button are retained, but the view entry is not shown. For executions that were not collected or have expired, a clear prompt indicates that the record is unavailable. Other notifications go directly to the corresponding chat, authorization, task, or plan page. Content requiring long-term reference is retained on the corresponding business page; there is no separate notification history page.

Chat displays one notification at a time, along with the remaining count. The current notification will not be interrupted by newly arrived notifications; after successful closure, the next notification is displayed in order of occurrence. If closure fails, the current notification is retained. Closure status is synchronized across devices; refreshing will not cause the same notification to reappear. Closing does not resend messages, cancel tasks, or change business processing results. Notifications automatically disappear after the issue is resolved; new anomalies generate new notifications.

"Notification Settings" in user settings manage notifications, previews, sounds, and desktop notifications for the current browser or desktop app. Disabling proactive notifications does not affect pending notifications within chats or unread message counts. Trusted HTTPS/local environments can authorize desktop notifications; push notifications are temporarily suspended after the page is closed, and pending notifications can be viewed in the corresponding chat upon reopening. Auto-collapse of desktop notifications and auto-dismissal of Toasts do not imply read or resolved status.

Turning on Desktop notifications requests authorization when needed; there is no separate authorization button. The desktop app uses native system notifications and stores preferences locally for each connection and account. Turning the switch off preserves in-app alerts and sound. macOS manages system authorization: allow Molr in system notification settings and check Focus if notifications do not appear. Keep the app open and connected.

Desktop notifications and message sound each show their status, test action, and result in the corresponding settings row. Send test notification can request browser permission when it has not yet been requested; when desktop notifications are off or unavailable, it displays an in-app alert. Preview sound does not change the sound switch. An accepted delivery request does not guarantee that the system displays the notification.

## About Molr

The last section of user settings, "About", displays the versions of WebUI and Runtime, Git commit, version consistency, and update information; clicking the commit allows viewing the corresponding code. The "Service Information" group below the version information displays the name and address of the current Runtime. Below this, you can visit the [Molr GitHub Project](https://github.com/molr-ai/molr) and star the project. The full Logo uses a day-night cycle animation, automatically switching at 08:00 and 20:00 based on the device's local time, respecting the reduce motion preference.

## Data Export

User Settings → Personal Data Export packages the current account's data into a ZIP file, which can be downloaded from the page after generation. By default, it includes shared and assistant-private memories (including core, topic, review, and forgetting records), assistant profiles, tasks and revisions, to-dos and plans, account settings, resource records belonging to the current account, as well as dreaming configurations and processing records. During export, you can optionally include chat history (checked by default) and attachments and deliverables (unchecked by default). If the export request is cancelled or times out, the Runtime stops reading attachments and closes opened files; incomplete archives are not published. Data export is not a complete service disaster recovery. Deployers still need to back up the database, files, assistant workspaces, configurations, and keys. See [Deployment and Maintenance](../../admin/deployment/maintenance.md).

## Desktop client window

After connecting to Runtime in the desktop client, the top bar shows the current connection, Disconnect and minimize/maximize/restore/close controls. Drag the empty title area to move the window. These controls remain available on login and error pages. Browser visits do not show desktop window controls.

Reopening the desktop app automatically reconnects to the Runtime selected before closing and reuses any still-valid login session. Explicitly disconnecting or returning to the connection list keeps the next launch on that list. If automatic reconnection fails, retry or choose another connection; a stopped local Runtime must be started manually.

Disconnect returns to the desktop connection list and preserves authentication for the next connection. Closing the window or disconnecting leaves Runtime and background tasks running. Unfinished downloads from the current page are cancelled and must be restarted. Failed connections or renderer crashes return to the bundled recovery screen, where you can retry or select another connection.
