# Planning and Execution

Arrange tasks into plans, and view the status, costs, and artifacts of each execution.

## Plans and Execution Records

"Plans" aggregate items, scheduled times, and automated executions for both you and your assistants, allowing filtering by owner, type, and status; the plan panel in chat defaults to viewing the current assistant.

The list defaults to showing the next 50 events in ascending order of occurrence time, with the option to load more. Each occurrence of a recurring plan is listed separately; undated items and overdue one-time arrangements are viewed through separate entries. Clicking a specific event shows the date and completion actions corresponding to that specific occurrence, while series edits still apply to the entire plan.

The calendar defaults to showing the current month, providing options for the previous month, next month, returning to today, and month selection. Events that do not fit in a cell are collapsed as +N; clicking +N or the date opens the full list for that day. Dreaming is not a calendar event; automated time slots can be adjusted in the dreaming section of user settings.

The solid dots before list items indicate type: green for items, blue for scheduled times, and purple for automated executions; these are for distinction only and are not clickable. Creation, editing, plan details, and unfinished execution details warn about dreaming conflicts. Each assistant has one configurable automated time slot per day.

When adding a new item, you can first fill in the title, then specify the owner, date, or time. Assigning to an assistant only indicates responsibility and does not mean immediate initiation. One-time executions and each occurrence of recurring plans have independent records; viewing records allows you to understand the instructions, status, costs, and artifacts at that time.

Completing a recurring arrangement completes a specific execution, not the entire series with a single check. Records with existing execution history or those depended upon by subsequent plans must be retained; subsequent arrangements can be paused. Plan revisions do not overwrite the instruction snapshots of old executions.

## Handoffs and Scheduled Executions

Plans can set prerequisite items. Current prerequisite dependencies support one-time plans; after prerequisites are completed, handoffs can be prompted within the app, or users can explicitly enable automatic initiation of subsequent assistant executions.

Handoffs do not come with expanded permissions or budgets. Result descriptions filled in by individuals and prerequisite artifacts can serve as basis for subsequent steps, but data access remains subject to assistant permission restrictions. Current in-app handoff prompts are not equivalent to system push notifications or email notifications.

Scheduled executions are based on the time zone and recurrence rules saved in the service, constrained by the online status of the execution device, resources, permissions, and budget. After the client is closed, the online Runtime can continue received work; execution cannot occur during Runtime downtime, and the handling of missed schedules depends on the plan strategy.

## Results and Revisions

In execution details, view waiting reasons, usage, and deliverables, and pause, resume, or cancel according to the current status. Appending revisions retains historical artifacts and incurred costs; do not interpret cancellation as deleting historical results.

"Message received," "Assistant has published a reply," and "Execution completed" are different statuses. Task delivery relies on actual artifacts and inspection evidence; currently, there is no universal acceptor that can automatically judge the quality of arbitrary content. Important results need to be verified by yourself.

For time settings for automatic organization, see [Memory and Automatic Organization](memory.md#dreaming-automatic-organization).
