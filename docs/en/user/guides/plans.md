# Planning and Execution

Arrange tasks into plans, and view the status, costs, and artifacts of each execution.

## Plans and Execution Records

"Plans" aggregate to-dos, schedules, scheduled executions, and continuous watches for both you and your assistants, allowing filtering by owner, type, and status; the plan panel in chat defaults to viewing the current assistant.

The plan sidebar or drawer in chat switches between Upcoming, Overdue, No date, and Continuous watches for the current assistant. Upcoming and watch lists show 10 items per page and support loading more. Clicking a plan slides the panel content to the left to show its details, using the same layout as the standalone plan list. The back button in the header returns to the list and preserves its scroll position.

The list defaults to showing the next 50 events in ascending order of occurrence time, with the option to load more. “List · Continuous watches” is a separate scope and does not appear in upcoming, overdue, or undated lists. Only the selected list is requested. Initial loads show a loading indicator; refreshes keep the current content, and failed reads can be retried. Ordinary type filters exclude watches. The watch list hides the type filter and retains owner and status filters. Each occurrence of a recurring plan is listed separately; undated items and overdue one-time arrangements are viewed through separate entries. Clicking a specific event shows the date and completion actions corresponding to that specific occurrence, while series edits still apply to the entire plan.

Plans opens in the current week by default, with Monday through Sunday in columns and a full-day vertical timeline. Events are positioned by time and duration; overlapping events appear side by side, and events spanning midnight appear in each affected day. Events without an end time use a short marker at their occurrence time. Today and the current time are highlighted, with colors and icons distinguishing items, time blocks, and automatic executions. Navigate by week, return to this week, or jump to a date; on narrow screens, scroll horizontally inside the timeline. Click an event for details or a date for its full agenda.

The calendar toolbar centers the displayed week range or month and updates it as you navigate. Click this date label to open the date picker; applying jumps to the chosen period, while cancelling keeps the current range. When viewing another period, “Back to this week” or “Back to this month” appears on the left; it is hidden for the current week or month. A compact week/month switch is on the right. The list remains available. All views share owner, type, and status filters. Month mode provides previous/next month and month selection. Events that do not fit in a cell are collapsed as +N; clicking +N or the date opens the full list for that day. Dreaming is not a calendar event; automated time slots can be adjusted in the dream settings section of assistant info.

The solid dots before list items indicate type: green for to-dos, blue for schedules, purple for scheduled execution, and orange for continuous watches; these are for distinction only and are not clickable. Creation, editing, plan details, and unfinished execution details warn about dreaming conflicts. Each assistant has one configurable automated time slot per day.

When adding a new item, you can first fill in the title, then specify the owner, date, or time. Assigning to an assistant only indicates responsibility and does not mean immediate initiation. One-time executions and each occurrence of recurring plans have independent records; viewing records allows you to understand the instructions, status, costs, and artifacts at that time.

Opening plan details loads execution history from newest to oldest, 20 records per page. Select “Load more history” for earlier runs. Expanding a record loads its original instructions, result, and task details. The history count shows the plan’s total executions. Empty history shows “No execution records” without a zero count. Initial loading has a loading indicator, and failed reads can be retried. Empty history does not determine whether a plan or watch is running.

Completing a recurring arrangement completes one occurrence. Pausing prevents future starts without interrupting an existing execution. Deleting removes the plan and its recurring series, cancels unfinished executions, and stops continuous observation. Execution history and watch events do not prevent deletion; dependencies from other undeleted plans must be removed first. Deletion is logical: underlying history and files remain, with no restore interface. Editing a plan does not overwrite prior execution snapshots.

Plan details separate plan information from execution information. The plan section shows ownership, enabled/paused state, dates, recurrence, and the next execution; undated plans show “No date set”. The description uses the same property-row layout and initially shows up to six lines. Longer descriptions offer Show more/Show less, preserve line breaks and the full text, and adapt to width and font-size changes. A plan entry shows plan information and execution history in reverse chronological order, without a separate latest-execution section. A calendar occurrence or history entry shows plan information and “Selected execution” with its status, actual start and end times, elapsed time, result, and cost, while hiding the history list. “View execution history” returns to the current plan’s records and clears the occurrence time and execution ID from the address. Start and complete actions for one-time plans remain below plan information; successful actions open the corresponding execution. Execution status, times, and cost share the same property-row layout. Instructions at execution are collapsed by default; revision history is also collapsed, retaining additions, acceptance criteria, and attachments without repeating the initial instructions. For a selected personal or assistant schedule occurrence without an execution record, the state is “Not started” before its scheduled time and “Not executed” once that time arrives. Existing records always show their actual execution state, without borrowing results from another date. To-do deadlines indicate lateness rather than execution state; continuous watches use their actual run state, and a whole recurring plan is not classified by one occurrence’s time. The displayed state updates at the scheduled time and is rechecked when the page becomes visible.

A whole-plan address has no execution selector. An unstarted occurrence uses only `at`; an existing execution uses only `execution`, with its occurrence time restored from the record. When an occurrence gains an execution record, the address is replaced with its ID without adding a browser-history entry. Reload, browser history, and one-time sign-in restore the selection. History loads in pages; selecting a record switches the execution details, and a known execution ID can address a record outside the loaded pages. Actual start times come from Runtime timing records and show “Not recorded” when unavailable. Elapsed time requires both recorded start and end times. Committed events refresh execution status.

## Handoffs and Scheduled Executions

Plans can set prerequisite items. Current prerequisite dependencies support one-time plans; after prerequisites are completed, handoffs can be prompted within the app, or users can explicitly enable automatic initiation of subsequent assistant executions.

Handoffs do not come with expanded permissions or budgets. Result descriptions filled in by individuals and prerequisite artifacts can serve as basis for subsequent steps, but data access remains subject to assistant permission restrictions. Current in-app handoff prompts are not equivalent to system push notifications or email notifications.

Scheduled executions are based on the time zone and recurrence rules saved in the service, constrained by the online status of the execution device, resources, permissions, and budget. After the client is closed, the online Runtime can continue received work; execution cannot occur during Runtime downtime, and the handling of missed schedules depends on the plan strategy.

## Results and Revisions

In execution details, view waiting reasons, usage, and deliverables, and pause, resume, or cancel according to the current status. Appending revisions retains historical artifacts and incurred costs; do not interpret cancellation as deleting historical results.

"Message received," "Assistant has published a reply," and "Execution completed" are different statuses. Task delivery relies on actual artifacts and inspection evidence; currently, there is no universal acceptor that can automatically judge the quality of arbitrary content. Important results need to be verified by yourself.

For time settings for automatic organization, see [Memory and Automatic Organization](memory.md#dreaming-automatic-organization).

## Continuous watches

Plan lists distinguish to-dos, personal schedules, scheduled execution, and continuous watches. The assistant prepares a watcher program and handles relevant changes; unchanged checks do not call a model. Ask, for example, “notify me about new issues.” No plan end date is set unless requested.

Global plans and the chat plan panel show watches separately through “List range → Continuous watches”. The global list loads 20 watches per page and the assistant panel loads 10, with load-more controls. Each watch occupies one row, never a calendar cell. Checks and process restarts do not become calendar events. Details show process status, successful observations, changes, execution history, and pause/resume controls. Processing one change does not complete the whole watch.

Watchers use the assistant's existing compute, file, and network grants. Expired grants stop observation and leave the plan blocked; the assistant can notify you or request the necessary authorization. Changed permissions are checked before recovery. Paused or expired plans do not resume automatically. Whether missed changes can be recovered depends on the source and watcher program.

Assistants create watchers through existing plan tools. The UI supports inspection, editing handling instructions and end dates, and pausing/resuming. See the [watcher protocol](../../dev/runtime/watchers.md).
