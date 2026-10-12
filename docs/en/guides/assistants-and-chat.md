# Assistants and Chat

Select an assistant from the assistant list to assign tasks, send attachments, and view replies. For first-time use, see [Quick Start](../getting-started.md).

## Persistent Chat and Assistant Management

Each assistant has its own persistent chat, execution state, and resource permissions. You can search by name, pin, rename, and edit models and budgets. The list prioritizes pinned assistants; pinned and unpinned assistants are each sorted in descending order by the time of their last message, with assistants that have no messages placed at the end of their respective groups; the order updates automatically when new messages arrive. The current real WebUI does not provide session grouping operations.

After hiring a brand-new assistant, the system automatically triggers a birth (`born`) event, guiding the assistant to send a brief initial message; there is no need to send a chat message first. The generation process runs asynchronously, using the selected model and subject to normal permissions, budgets, and failure handling constraints. Cloning, rehiring, restarting services, and resetting context do not trigger the birth event again.

When opening `/#/assistants`, no assistant is selected; after selecting from the list or the topic's assistant selector, the address changes to `/#/assistants/<助理 ID>` and the corresponding chat opens. Refreshing, browser forward, and back navigation restore the selection based on the address; returning to the assistant entry clears the current selection.

When cloning, only explicitly selected content and still-valid authorizations are copied; it does not take over tasks currently being executed by the original assistant. Firing stops subsequent execution and archives the chat; view or rehire in "User Settings → Departed Assistants". Rehiring does not automatically restart old tasks, nor does it restore old permissions and plans by default.

Assistant thinking and internal tool calls do not all enter the chat. The page displays messages explicitly published by the assistant; background plans can execute without new user messages, and scheduled triggers do not masquerade as messages sent by you.

If the assistant finishes processing a user message without calling the send tool, the system privately reminds it whether it forgot to send a message, at most once per turn. The assistant can still determine that no reply is needed and remain silent; the reminder does not automatically send internal replies to the chat, nor does it bypass stop, permissions, or execution quotas.

## Name and Avatar

You can directly tell the assistant "Call me Xiaoman from now on, change to a cat avatar," or send an image and say "Set this image as your avatar." The assistant modifies its own name and avatar via the built-in `profile_update`; after saving, the assistant's image in the chat title, list, and historical messages is updated synchronously. Users can still manually modify it on the assistant information page.

Supports 12 presets, as well as PNG, JPEG, and WebP images (maximum 10 MiB, 40 million pixels); images are automatically center-cropped to 256 × 256. After clearing the avatar, the first character of the name is displayed. The name is limited to 256 characters, cannot be empty, and cannot contain control characters.

This tool only modifies the current assistant's profile; it does not change the model, budget, permissions, or chat identity. When using images, file read permissions are still required; chat attachments are first saved to the assistant workspace. When name and avatar are submitted together, they are saved only if all succeed. Concurrent modifications prompt a version conflict and do not automatically overwrite; if there is an unsaved draft on the information page, the draft is retained. In case of conflict, close and reopen, verify the latest information, and then edit.

## Messages, Drafts, and Stop

Messages support Markdown, attachments, and result references. The copy button copies the original message text; attachment reading and downloading are protected by permissions.

Below assistant messages, you can like or dislike; clicking again cancels, or you can switch to the other feedback; it is retained after refresh if saved. When selected, the sleeve is solid color and the palm is light-filled; like is red, dislike is gray. There is a brief animation when activated; if the system or user settings enable reduced motion, it switches statically.

Message feedback clicks immediately update the selected state and play the animation; if saving takes more than 300 ms, a small dot is displayed below the button; during this time, only the like/dislike for that message is locked, and the input box and other messages remain operable. On success, the selection is maintained and the animation is not replayed; on failure, the original selection is restored and a retry prompt is shown, followed by re-synchronizing the server state. Canceling feedback is also displayed immediately.

Clicking the information button below an assistant message allows you to view the usage for this turn. If the development workbench is available and set to display, the popup provides a "View in Development Workbench" entry, opening the current assistant's workbench and automatically locating the execution for this turn; if the record was not collected or has expired, a prompt is displayed. If set to not display, the entry is hidden. Cache hit rate represents the proportion of cached input tokens to input tokens; if input is zero or data is missing, `—` is displayed.

Chat drafts are saved by the current tab and shared when switching topics; they are not server-side cross-device synchronized chat records. If browser storage is unavailable, they can only be retained within the page; important content should be sent or saved separately.

You can add messages during execution; the assistant receives supplements at execution boundaries. The stop button interrupts the current turn; writes that have already taken effect are not rolled back; subsequent new messages or normal plans can still wake the assistant. To stop subsequent arrangements of a plan or cancel a task, operate in the corresponding plan/execution record.

## Search History and Context

The model context for long conversations is automatically compressed by default when it reaches 80% of the window. The compression request retains the complete current context and appends compression instructions; only after success is the active context rebuilt with a summary; original chats and execution records are retained, and there is no need to manually clear the chat. Usage estimation does not block requests; actual context overflow is determined by the error code returned by the model service.

## Input Box Commands and Context Compression

Entering `/` at the beginning of the message input box allows you to view commands; entering `/com` or `/压缩` filters `/compress`. Use arrow keys to select, Tab or Enter to complete, and Esc to close; after completion, press Enter again or click send to execute. Entering `/compress` completely allows direct sending; Shift+Enter still inserts a line break, and Chinese input method word selection does not execute commands.

`/compress` queues a context compression event: it waits for the current model response and the entire batch of tool calls for that response to complete before compressing; if the assistant is idle, it processes directly; during dreaming, it waits for the dreaming session to end. This command accepts no parameters, does not generate chat messages, and does not send or remove attached attachments. When the threshold is reached automatically, the same event mechanism is used, and duplicate requests are merged.

Only when compression is actually processed does the assistant status display "Compressing context". Compression retains chats, files, memories, and execution evidence, and continues the original work after completion; on failure, the original context is retained and an error is prompted. Calling the summary model uses the current model authorization and budget. Normal messages continue to queue; "Urgent" still interrupts model generation; Shell completion notifications and running reminders are integrated after the current step ends.

## WeChat Chat Entry

Assistants chat in the WebUI by default. You can connect WeChat via "… → Switch chat channel" in the top right corner of the chat page; upon success, WeChat handles the chat, and the WebUI is used for viewing records and management. After unbinding, WebUI input is restored, and records and context are retained. See [WeChat Chat Entry](../chat-channels/weixin.md) for details.

If you need scheduled execution or to view deliverables, continue reading [Plans and Execution](plans.md).
