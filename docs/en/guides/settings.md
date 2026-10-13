# Personal Settings

On desktop, click the avatar at the bottom of the sidebar; on mobile, click the avatar and username at the top of the assistant page to enter user settings. Administrators can enter the management page via the "Admin Settings" button in the top-right corner of the user settings page header. For operations, see [Managing Users and User Groups](../administration/users-and-groups.md).

## Profile and Avatar

Set your display name and avatar in User Settings → Profile. You can choose a preset avatar, or upload a PNG, JPG, or WebP image not exceeding 10 MB and 40 million pixels; the image will be center-cropped into a 256 × 256 thumbnail within the browser. After clicking "Save", the new avatar will be displayed in the desktop sidebar and at the top of the mobile view; after clearing and saving, the first character of the name will be restored. The avatar is saved with the account and remains available after logging in on a different device.

## Language, Theme, and Preferences

General settings manage preferences such as language, time zone, and currency. "Show Plugins" is disabled by default; when enabled, it displays plugin navigation, plugin themes, and related management options, with settings synchronized across the account. For specific scope, see [Plugins](plugins-and-skills.md). The theme page distinguishes between interface theme, color scheme, and light/dark mode. Once saved to the account, it can be accessed on other devices. Two built-in themes, Default and Test, are provided. You can also select [Plugin Themes](plugins-and-skills.md) that have been installed, authorized, and enabled by the administrator. Theme color schemes include Forest, Sea Salt Blue, Iris Purple, Warm Sand, Rose, and Plain Ink, all supporting light and dark displays. Plain Ink uses a neutral grayscale interface, while semantic colors are retained for success, warning, error, and other notifications.

For viewing, editing, and dreaming time of memories, see [Memory Guide](memory.md). For selecting default models and assistant models, see [Permissions and Budget](permissions-and-budget.md#selecting-models).

## System Notifications

WeChat delivery/connection anomalies, persistent assistant blockages, authorization requests, pending plan handovers, and task/background execution failures are managed centrally by the server for notification status. Background execution failure notifications display "View in Development Workbench" when the development workbench is available and display is configured; clicking this locates the corresponding execution. When display is not configured, the notification and close button are retained, but the view entry is not shown. For executions that were not collected or have expired, a clear prompt indicates that the record is unavailable. Other notifications go directly to the corresponding chat, authorization, task, or plan page. Content requiring long-term reference is retained on the corresponding business page; there is no separate notification history page.

Chat displays one notification at a time, along with the remaining count. The current notification will not be interrupted by newly arrived notifications; after successful closure, the next notification is displayed in order of occurrence. If closure fails, the current notification is retained. Closure status is synchronized across devices; refreshing will not cause the same notification to reappear. Closing does not resend messages, cancel tasks, or change business processing results. Notifications automatically disappear after the issue is resolved; new anomalies generate new notifications.

"Notification Settings" in user settings manage notifications, previews, sounds, and desktop notifications for the current browser. Disabling proactive notifications does not affect pending notifications within chats or unread message counts. Trusted HTTPS/local environments can authorize desktop notifications; push notifications are temporarily suspended after the page is closed, and pending notifications can be viewed in the corresponding chat upon reopening. Auto-collapse of desktop notifications and auto-dismissal of Toasts do not imply read or resolved status.

## Component Management

Administrators can open Admin Settings → Components from the User Settings page header to view browser component versions and installation progress, and install, update, or repair components. For browser usage, see [Operating the Browser](../best-practices/browser.md).

## About Molr

The last section of user settings, "About", displays the versions of WebUI and Runtime, Git commit, version consistency, and update information; clicking the commit allows viewing the corresponding code. The "Service Information" group below the version information displays the name and address of the current Runtime. Below this, you can visit the [Molr GitHub Project](https://github.com/molr-ai/molr) and star the project. The full Logo uses a day-night cycle animation, automatically switching at 08:00 and 20:00 based on the device's local time, respecting the reduce motion preference.

## Data Export

User Settings → Personal Data Export packages the current account's data into a ZIP file, which can be downloaded from the page after generation. By default, it includes shared and assistant-private memories (including core, topic, review, and forgetting records), assistant profiles, tasks and revisions, to-dos and plans, account settings, resource records belonging to the current account, as well as dreaming configurations and processing records. During export, you can optionally include chat history (checked by default) and attachments and deliverables (unchecked by default). If the export request is cancelled or times out, the Runtime stops reading attachments and closes opened files; incomplete archives are not published. Data export is not a complete service disaster recovery. Deployers still need to back up the database, files, assistant workspaces, configurations, and keys. See [Deployment and Maintenance](../deployment/maintenance.md).
