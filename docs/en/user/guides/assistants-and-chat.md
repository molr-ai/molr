# Assistants and Chat

Select an assistant from the assistant list to assign tasks, send attachments, and view replies. For first-time use, see [Quick Start](../getting-started.md).

In the default desktop interface, drag the gaps between the assistant list, chat, and docked side panel to resize them. Focus a separator and use Left/Right to adjust it, Shift for larger steps, or Home/End for the current minimum/maximum width. Widths are saved for the current browser and account and restored after a reload. Assistant information, permissions, tasks, running tasks, browser, files, and terminal panels each keep their own width. Smaller windows constrain the displayed widths and use a drawer when docking cannot fit; enlarging the window restores the saved widths.

## Persistent Chat and Assistant Management

Each assistant has its own persistent chat, execution state, and resource permissions. You can search by name, pin, rename, and edit models and budgets. The list prioritizes pinned assistants; pinned and unpinned assistants are each sorted in descending order by the time of their last message, with assistants that have no messages placed at the end of their respective groups; the order updates automatically when new messages arrive. The current real WebUI does not provide session grouping operations.

After hiring a brand-new assistant, the system automatically triggers a birth (`born`) event, guiding the assistant to send a brief initial message; there is no need to send a chat message first. The generation process runs asynchronously, using the selected model and subject to normal permissions, budgets, and failure handling constraints. Cloning, rehiring, restarting services, and resetting context do not trigger the birth event again.

When opening `/#/assistants`, no assistant is selected; after selecting from the list or the topic's assistant selector, the address changes to `/#/assistants/<助理 ID>` and the corresponding chat opens. Refreshing, browser forward, and back navigation restore the selection based on the address; returning to the assistant entry clears the current selection.

When cloning, only explicitly selected content and still-valid authorizations are copied; it does not take over tasks currently being executed by the original assistant. While memory is being copied, the new assistant remains absent from the list and cannot receive messages. It becomes available after file copying and initialization finish. Retry the original request to resume an interrupted clone; Runtime startup also attempts to resume pending clones automatically; file conflicts need to be resolved before retrying. Replaying a successful request does not initialize the assistant again or recreate memories deleted afterward. Firing stops subsequent execution and archives the chat; view or rehire in "User Settings → Departed Assistants". Rehiring does not automatically restart old tasks, nor does it restore old permissions and plans by default.

Assistant thinking and internal tool calls do not all enter the chat. The page displays messages explicitly published by the assistant; background plans can execute without new user messages, and scheduled triggers do not masquerade as messages sent by you.

If the assistant is about to finish without having called the send tool since the latest accepted user message, the system privately reminds it whether it forgot to send a message. Each user message can trigger at most one reminder across permission approval, recovery, and background continuation; permission cards do not count as send-tool calls. The assistant may still decide that no reply is needed and remain silent. The reminder does not automatically publish internal replies or bypass stops, permissions, or execution quotas. A previously sent progress message suppresses the reminder, so this check does not guarantee delivery of a final result.

## Name, Avatar and Status

You can directly tell the assistant "Call me Xiaoman from now on, change to a cat avatar," or send an image and say "Set this image as your avatar." The assistant modifies its own name and avatar via the built-in `profile_update`; after saving, the assistant's image in the chat title, list, and historical messages is updated synchronously. Users can still manually modify it on the assistant information page.

Supports 12 presets, as well as PNG, JPEG, and WebP images (maximum 10 MiB, 40 million pixels); images are automatically center-cropped to 256 × 256. After clearing the avatar, the first character of the name is displayed. The name is limited to 256 characters, cannot be empty, and cannot contain control characters.

The assistant can also set a status through `profile_update` to express its current mood, thoughts, hopes, or personality. The assistant information panel displays it as read-only. While idle, the chat header shows this text instead of “Idle”; working, dreaming, waiting, paused, stopped, and archived states still show the actual activity. Status accepts up to 256 characters with no control characters. Only the assistant can set or clear it; clearing restores “Idle”.

This tool only modifies the current assistant's profile; it does not change the model, budget, permissions, or chat identity. When using images, file read permissions are still required; chat attachments are first saved to the assistant workspace. When name and avatar are submitted together, they are saved only if all succeed. Concurrent modifications prompt a version conflict and do not automatically overwrite; if there is an unsaved draft on the information page, the draft is retained. In case of conflict, close and reopen, verify the latest information, and then edit.

## Messages, Drafts, and Stop

Main navigation and assistant list text is excluded from select-all and drag selections. Chat messages remain selectable and copyable. Press Ctrl+A (Cmd+A on macOS) in a search field or message composer to select all text in that field.

Messages support Markdown, attachments, and result references. The copy button copies the original message text; attachment reading and downloading are protected by permissions.

Below assistant messages, you can like or dislike; clicking again cancels, or you can switch to the other feedback; it is retained after refresh if saved. When selected, the sleeve is solid color and the palm is light-filled; like is red, dislike is gray. There is a brief animation when activated; if the system or user settings enable reduced motion, it switches statically.

Message feedback clicks immediately update the selected state and play the animation; if saving takes more than 300 ms, a small dot is displayed below the button; during this time, only the like/dislike for that message is locked, and the input box and other messages remain operable. On success, the selection is maintained and the animation is not replayed; on failure, the original selection is restored and a retry prompt is shown, followed by re-synchronizing the server state. Canceling feedback is also displayed immediately.

Clicking the information button below an assistant message allows you to view the usage for this turn. If the development workbench is available and set to display, the popup provides a "View in Development Workbench" entry, opening the current assistant's workbench and automatically locating the execution for this turn; if the record was not collected or has expired, a prompt is displayed. If set to not display, the entry is hidden. Cache hit rate represents the proportion of cached input tokens to input tokens; if input is zero or data is missing, `—` is displayed.

Chat drafts are saved by the current tab and shared when switching topics; they are not server-side cross-device synchronized chat records. If browser storage is unavailable, they can only be retained within the page; important content should be sent or saved separately.

You can add messages during execution; the assistant receives supplements at execution boundaries. The stop button interrupts the current turn; writes that have already taken effect are not rolled back; subsequent new messages or normal plans can still wake the assistant. To stop subsequent arrangements of a plan or cancel a task, operate in the corresponding plan/execution record.

## Search History and Context

When browsing older messages, click “Load earlier messages” in the center of the divider to load more history while keeping your reading position. When you are far from the latest message, a “Back to bottom” button appears at the bottom of the message area. The label changes to “New messages” when the conversation has unread messages. Clicking it returns to the bottom and exits search-message navigation.

When hiring an assistant or opening Assistant information, adjust “Context compression threshold” below the model selector. Click the information icon for help. The slider ranges from 64K to 80% of the current model’s effective context window, rounded down to whole tokens. The default is the smaller of 128K and that maximum. If the maximum is 64K or less, the slider is fixed at that maximum and disabled. 1K = 1024 tokens.

Drag the slider to choose a custom threshold, or click “Restore default” to use the automatic value, then save the form. Switching models recalculates the automatic value and clamps a custom value to the new model’s range. Changes saved during execution apply at the next safe checkpoint; compression waits for the entire current tool batch to finish. A single model response or tool result can carry usage beyond the threshold.

Lower thresholds reduce history in subsequent requests but increase summary calls and may lose detail. Actual costs also depend on model pricing and cache hits. Long conversations are automatically compressed at the threshold. The compression request retains the complete current context and appends compression instructions; only after success is the active context rebuilt with a summary; original chats and execution records are retained, and there is no need to manually clear the chat. Usage estimation does not block requests; actual context overflow is determined by the error code returned by the model service.

## Service restarts and task recovery

After a service restart, in-flight tasks automatically wake the assistant to continue; you do not need to send “continue.” The assistant preserves completed work, checks interrupted operations and reads new user messages first. Successful recovery needs no extra announcement. Work you stopped, paused or reset is not restarted by recovery. Repeated interruptions, unreadable progress or a specific condition requiring your help produce an explanation only when work cannot continue.

## Input Box Commands and Context Compression

Entering `/` at the beginning of the message input box allows you to view commands; entering `/com` or `/压` filters the compression command. Use arrow keys to select, Tab or Enter to complete, and Esc to close; after completion, press Enter again or click send to execute. Entering a complete English command or Chinese alias allows direct sending; Shift+Enter still inserts a line break, and Chinese input method word selection does not execute commands.

| Command | Chinese alias | Available when |
| --- | --- | --- |
| `/compress` | `/压缩` | The assistant is available |
| `/permission approve` | `/权限审批 同意` | The current assistant has pending permission requests |
| `/permission reject` | `/权限审批 拒绝` | The current assistant has pending permission requests |

Permission commands appear in the menu while requests are pending, with Chinese alias completion. Without an argument, they resolve the current assistant’s newest pending request; resolved requests are excluded.

- `/permission approve <id>` or `/权限审批 同意 <id>` approves a specific request. Replace `<id>` with the complete request ID copied from its permission card, without angle brackets.
- `/permission approve all` or `/权限审批 同意 全部` approves all requests pending for the current assistant when the command is sent.
- Replace `approve` with `reject`, or `同意` with `拒绝`, to reject the newest, specified, or all requests. Both languages accept `all` and `全部`.

Approval preserves the proposed resources, actions, scopes, quotas, and expiry, including grouped items. Rejection grants no access. Missing resources or quotas require “Review permission” on the card. IDs must exactly match a pending request for the current assistant; an unknown ID never falls back to another request. State and version are checked before execution; commands without an argument also check that the newest request has not changed.

Batch commands check all selected requests before submitting them sequentially. A failure stops processing and reports the completed count. Successful decisions remain in effect; review remaining requests before retrying. Requests arriving during processing are excluded from that batch. Outside the approval flow, commands only show a notice. They do not create chat messages and retain attached files in the composer.

`/compress` (or `/压缩`) queues a context compression event: it waits for the current model response and the entire batch of tool calls for that response to complete before compressing; if the assistant is idle, it processes directly; during dreaming, it waits for the dreaming session to end. This command accepts no parameters, does not generate chat messages, and does not send or remove attached attachments. When the threshold is reached automatically, the same event mechanism is used, and duplicate requests are merged.

Only when compression is actually processed does the assistant status display "Compressing context". Compression retains chats, files, memories, and execution evidence, and continues the original work after completion; on failure, the original context is retained and an error is prompted. Calling the summary model uses the current model authorization and budget. Normal messages continue to queue; "Urgent" still interrupts model generation; Shell completion notifications and running reminders are integrated after the current step ends.

## WeChat Chat Entry

Assistants chat in the WebUI by default. You can connect WeChat via "… → Switch chat channel" in the top right corner of the chat page; upon success, WeChat handles the chat, and the WebUI is used for viewing records and management. After unbinding, WebUI input is restored, and records and context are retained. See [WeChat Chat Entry](../chat-channels/weixin.md) for details.

If you need scheduled execution or to view deliverables, continue reading [Plans and Execution](plans.md).

## Workspace file explorer

The folder button in the chat header opens the current assistant's file explorer. On desktop it opens as a movable window with independent width and height, with an option to dock on the right. Mobile uses a bottom drawer. Wide panels show the list and preview side by side; narrow panels switch between them. Reopening restores the assistant's directory, selection and scroll position within the current page.

The explorer accesses this assistant's `/work`. Open folders or use breadcrumbs to navigate. Folders sort first; name, modification time and size sorting apply to loaded entries. Use “Load more” for large directories. “File options” can show hidden files. Links and unavailable entries display their names only.

Create files and folders, drop or select local files to upload into the current directory, download individual files, rename or move entries, and delete files or empty folders. Upload conflicts offer replace, keep both or skip. Uploads are limited to the smaller of the deployment upload limit and 64 MiB, and also obey workspace file and capacity quotas. Recursive deletion of nonempty folders is unavailable.

UTF-8 text previews show up to 1 MiB; complete files up to that size can be edited and explicitly saved with the button or Ctrl/Cmd+S. PNG, JPEG, GIF and WebP files up to 20 MiB support image previews and original size. Other formats offer downloads. Markdown, HTML and SVG are displayed as text without executing file content.

Version checks prevent overwriting a file changed by the assistant. Conflicts preserve your draft and offer reload or save as. Closing, switching panels or leaving through application navigation asks before discarding unsaved changes. While the page is visible, the open file and single-page directory refresh approximately every five seconds. Paginated directories require manual refresh, which returns to the first page. If a write response is lost, refresh and verify its result before another operation.

“Copy path” copies an assistant-readable `/work/…` path. Uploading or saving does not send a chat message or publish a deliverable. The panel excludes shared `/global`, external storage and remote sandboxes. Disabled or unavailable local storage produces an error.
