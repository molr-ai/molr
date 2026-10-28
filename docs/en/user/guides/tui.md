# TUI user guide

Use `molr tui` to connect to an existing Molr service from your terminal, select assistants, exchange messages, and review permission requests. Connect to the same service and account to continue conversations from the WebUI or desktop app.

## Prerequisites

Have a working `molr` executable, a service address, and an account. The executable ships with Runtime; see the [installation guide](../../admin/deployment/installation.md). Use a terminal of at least 42 columns by 18 rows, with both standard input and output connected to an interactive terminal.

With an existing service, account, and administrator-assigned models, create an assistant directly in the TUI. See [Quick start](../getting-started.md) for account and resource preparation.

## Start and connect

Run in an interactive terminal:

```bash
molr tui
```

By default, the TUI restores the last successfully connected Runtime. With no previous selection, it opens connection management. Use arrows and Enter to open a saved connection; choose “Add connection” or press N to add an address. Name and CA certificate path are optional. Tab changes fields and Enter saves and connects. Failed connections keep the list and error visible for retry or another selection.

Press Ctrl+O or send `/connections` from chat to open connection management; Esc returns to the current chat. Switching Runtimes cancels the previous connection's requests and realtime subscription. Connections, login sessions, and local chat drafts are isolated by Runtime and account. Returning to the same account restores its drafts.

## Login and sessions

First-time connections open a separate login page for username and password, followed by an MFA code when enabled. Passwords and codes are masked and never saved to disk. TUI connections and the last selection live in `molr/tui/connections.json` under the user configuration directory; each session has its own file under `molr/tui/sessions/`, separate from desktop application data, with mode `0600`. After session expiry, reopen connection management and select that connection to sign in again.

## Select assistants and chat

A gray dot `●` appears to the left of the first line of each assistant message to distinguish consecutive messages. Paragraphs and wrapped lines within the same message do not repeat the marker.

The terminal uses existing Runtime assistants, conversation history, and permission requests. Chat, assistant selection, approvals, and help use separate borderless full-screen pages. Ctrl+K or `/assistant` opens selection; type to search names, use arrows, and press Enter. Switching assistants preserves drafts, cursors, and scroll positions during the current run. Use `/new`, or Ctrl+N in assistant selection, to create an assistant. F1 opens the operation menu.

Enter sends; Alt+Enter or Ctrl+J inserts a newline. The composer grows from one to five visible rows and scrolls additional text. Alt+↑ / Alt+↓ and the wheel scroll history; scrolling upward at the top loads older records. Ctrl+G (or Ctrl+End) returns to the latest message. Lost realtime connections reconnect and fetch missed messages. Ctrl+R refreshes manually. Failed sends preserve the draft; retrying the same content with Enter reuses its idempotency key. Runtime retains sent messages after exit; unsent drafts are saved locally. `/retry-send` retries an unconfirmed send with its original payload and idempotency key, excluding later edits.

When scrolled away from the bottom, a clickable “Back to bottom” control appears on the right above the composer. It changes to “New messages” when the conversation has unread messages. It shares the status row, leaving a blank row above the composer and keeping history in place. Incoming messages preserve your reading position. PgUp/PgDn remain available for paging.

Message bodies support up to 4000 characters. At the limit, the TUI preserves the draft and prompts you to convert it into a text attachment with Ctrl+T. Tasks run in Runtime under the assistant’s [permissions and budgets](permissions-and-budget.md) and sandbox controls. Use `/settings` for the assistant model, budget, and compression threshold; administrators prepare resources, and the TUI manages assistant grants.

When an assistant uses an external channel such as WeChat or Telegram, the TUI shows the channel above the composer and changes its prompt from `›` to `/` for commands only. Type `help`, `compress`, or another command directly; a leading `/` is also accepted. Tab completes and Enter executes. Message drafts and attachments are preserved separately and restored when the channel is disconnected. Command input cannot add attachments, and Ctrl+C clears only the current command. Send messages through that channel, or use `/channel` to review and confirm disconnection to restore TUI input. Sending also waits until channel discovery completes; Ctrl+R checks again. Stopping execution, approvals, history, and attachment downloads remain available. Disconnecting stops external-channel traffic and cancels unfinished deliveries while preserving chat history and drafts.

On macOS iTerm2, use Option+Enter for a newline. TUI requests modifier reporting from terminals supporting the Kitty keyboard protocol and restores the previous mode on exit. Enable **Keys → General → Apps can change how keys are reported** in the current profile. If Option+Enter still sends, the terminal may still encode it as ordinary Enter. Add an Option+Enter mapping under **Keys → Key Mappings**, select **Send Hex Code**, and enter `0x1b 0x0d`, or use Ctrl+J. The app cannot recover an Option modifier already lost by the terminal. See [iTerm2 keyboard settings](https://iterm2.com/documentation-preferences-profiles-keys.html).

## Attachments and long pastes

Ctrl+F or `/attach` opens file selection. Use arrows to browse, Enter to open directories or add files, Space for multiple selection, and Backspace for the parent directory. Ctrl+L edits the path; Tab completes a unique match. `/attach ./report.xlsx` also accepts a path, including `~`, spaces, and paired quotes. Paths refer to the machine running the TUI; over SSH, they refer to the remote machine.

Files upload into the current assistant’s draft. Enter sends them with explanatory text, or on their own. Each message supports up to five attachments of at most 20 MiB each; server deployment limits and account quotas still apply. Upload progress is displayed. Ctrl+E or `/drafts` opens pending attachments: R retries, D or Delete removes. Retries reuse the original bytes and idempotency key. Failed sends retain text and attachments; success removes only the attachments included in that send. Switching services cancels transfers; retry when returning.

With terminal bracketed paste support, a single paste exceeding 4000 Unicode characters, reaching 20 lines, or taking the existing body past 4000 characters becomes a UTF-8 text attachment instead of message body text. Attachments show their filename, size, and upload status. Short screens show only the first few attachments; Ctrl+E lists all, Enter previews pasted text, Alt+↑ / Alt+↓ scrolls, and Esc returns. Original line endings and tabs are preserved; previews sanitize terminal control characters. Ctrl+T converts the complete current composer text into an attachment so you can add a short instruction. Size or count limit failures explicitly report that the attachment was not added.

Text attachments are not automatically inserted into model context as message body text. Normal and expedited delivery carry attachment references; the assistant uses attachment and file tools to read content as needed. Content it reads may still enter context.

Messages display attachment names and sizes. `/attachments` lists files from loaded chat history. Select one, press Enter to edit the full destination path, then Enter to download. Existing files require replacement confirmation; Esc returns to path editing. Esc cancels an active download. Successful downloads show the saved path. Size and SHA-256 are verified before publishing the destination; failed downloads leave no partial file. Load older chat history to access earlier attachments.

A paste that remains inline and exceeds 100 characters or two lines becomes a purple text block, distinct from ordinary text, showing its character count and a short preview. Each paste is a separate block. Left/right arrows move across the whole block; Backspace or Delete removes it in one action. Type ordinary text before or after blocks. Sending expands each block into its original position in the message body, without uploading a file or sending the preview label. The 4000-character limit counts expanded original text. Switching assistants preserves their individual blocks.

In chat with no mouse selection, Ctrl+C first clears the current text, pasted blocks, and pending attachments, cancelling active uploads. It exits only when the composer is completely empty. Clearing does not retract submitted messages or stop Runtime execution. With a mouse selection, Ctrl+C still copies; Ctrl+Q always exits.

## Execution controls and commands

Ctrl+S or `/stop` (`/停止`) interrupts the current turn without rolling back completed writes. Exiting the TUI does not stop Runtime tasks. While working, send additional messages normally, then use `/expedite` (`/加急`) to prioritize unread messages through the current sequence; already running tools finish first.

`/compress` (`/压缩`) requests context compression without sending a chat message or removing draft attachments. Type `/`, use arrows to select a command, Tab or Enter to complete, and Enter again to execute. Esc dismisses completion. Complete commands execute directly.

`/permission approve|reject` (`/权限审批 同意|拒绝`) handles the latest pending request for the current assistant. Append a complete request ID for an exact match, or `all` / `全部` for all requests pending when the command was submitted. IDs and versions are rechecked before execution. Batches preflight all selected requests, stop on failure, and report how many succeeded; completed decisions remain applied. Approval preserves requested parameters. Complete missing resources or limits in the `/approval` form.

## Review permission requests

Ctrl+P, `/approval`, or clicking the pending-approval prompt above the composer opens the current assistant's pending requests. Select one to inspect its resource, actions, scope, expiry, and limits, then approve as requested, adjust before approval, or reject. Tab changes form fields, arrows change options, and Enter reviews the final scope; Enter on the confirmation page submits. Grouped requests can be adjusted item by item and are submitted together.

Approval binds the displayed request ID and version. New requests cannot replace the reviewed target. Conflicts retain the current contents; return and reopen to compare. Compute, network, storage, and temporary limits use dedicated fields. Full Access displays its complete scope explicitly.

`/permissions` lists existing grants and supports creating, adjusting, and revoking them. Revocation can interrupt work using that grant. Resources must be visible to the account and available; administrator-controlled resource or account limits remain outside the TUI's control.

## Read and copy

User messages and the composer share a background; assistant replies use the terminal background. Messages and help support Markdown tables, quotes, code, bold, and italics; the composer keeps source text. Drag to select visible text in messages or the composer; releasing the mouse copies automatically. Ctrl+C copies again; click anywhere or press Esc to clear. Highlighting preserves text styles. Composer and message selections stay within their own regions. Copy requires terminal support and permission for OSC52 clipboard writes. The status confirms that the request was sent to the terminal, not that the system clipboard accepted it. Scrolling or resizing clears the selection.

Clicking the active reminder or bound-channel prompt above the composer opens status details or channel management, preserving the draft and history position.

## Independent operation and management

F1 opens the operation menu. Management pages use arrows to select, Enter to open or confirm, Tab to change fields, left/right to change options, Alt+↑ / Alt+↓ to read long content, and Esc to return. Dirty forms ask Y to discard or N to continue editing. Ctrl+R refreshes the current list or details.

| Command | Purpose |
| --- | --- |
| `/new` | Create an assistant with an available model |
| `/settings` | Change name, authorized model, budget, and compression threshold |
| `/status` | Inspect execution state, blocking reasons, and active notices |
| `/permissions` | Inspect, create, adjust, and revoke grants |
| `/tasks` | Inspect tasks, revisions, costs, and artifacts; pause, resume, or cancel |
| `/plans` | Inspect existing plans and execution history; pause or resume future work |
| `/processes` | Inspect background processes, terminal sessions, and output; stop selected work |
| `/files` | Browse the assistant workspace, preview text, upload, and download |
| `/search [query]` | Search messages for the current assistant or all assistants |
| `/messages` | Inspect loaded messages, original text, timestamps, attachments, and turn usage |
| `/channel` | Inspect and disconnect external channels to restore input |
| `/retry-send` | Retry an unconfirmed send with its original payload |

Assistant creation includes the selected model grant through the existing create operation. Changing models only offers models with a valid existing execution grant and configured pricing. Grant a new model first in permissions, then select it. A zero budget permits only zero-cost operations. Provider connection management remains outside the TUI.

Chat displays the current active notice and count. `/status` opens or dismisses it; dismissal acknowledges the notice without resolving its source. Task, permission, and plan notices open related details. Errors show safe reason codes and request IDs. Recovery actions use only existing service capabilities.

`/stop` interrupts the assistant's current turn. Task controls affect a business task; plan controls affect future occurrences; process controls end a specific background execution. Existing file changes and charges remain. Terminal sessions require confirmation to take control and end them; interactive Shell input is not provided. Output uses byte cursors and retains the latest 64 KiB on screen, with following and local saving. Incomplete archives are marked.

Task details expose artifacts from current and historical revisions. `/files` accesses the assistant's local `/work`, including paginated directories, hidden-file visibility, text previews, and file upload/download. Uploads are limited to 64 MiB and the service's deployment/workspace limits. Existing names offer version-checked replacement or keeping both files. Uploading does not send a chat message. File details can copy the assistant path or add it to the composer.

Inspect the target after an unknown workspace write result instead of automatically retrying. Downloads fail when a file version changes during transfer. Existing local destinations require confirmation and are checked again before replacement; downloads publish temporary files only after success.

Search opens the exact matching message, with original-text copying/saving, attachments, and card targets. It neither scans full history nor loads surrounding context. Returning restores the search selection. Complete execution state for another assistant refreshes when that assistant is opened.

## Local drafts and connection maintenance

Text, original paste blocks, cursor positions, attachment snapshots, and unconfirmed-send keys live under `drafts/` beside the connection configuration, isolated by connection, account, and assistant. Files use mode `0600` and directories `0700`. Each connection/account cache is limited to 256 MiB; reaching the limit reports an error without deleting unsent content. Attachment bytes are saved before upload so later source-file edits do not alter retries.

Signing back into the same account restores drafts without sending them. Receipts clear only the submitted contents. Clearing with Ctrl+C also updates the local draft. Corrupt or unwritable drafts retain original files and show an error. An OS file lock permits only one writer for an account; close another instance and reconnect. Locks are released when the process exits.

Connection management supports E to edit, L to sign out, A to change accounts, D to remove a connection while retaining drafts, and X to remove the connection and its local drafts. Changing the address creates a separate connection without reusing credentials. Sign out of any connection with a saved login before removing it. If server logout fails offline, R retries revocation and F explicitly clears only the local login, leaving server revocation unconfirmed. Removing a connection does not delete Runtime assistants or chats.

## Shortcut reference

| Shortcut or command | Action |
| --- | --- |
| Ctrl+O / `/connections` | Open connection management |
| Ctrl+K / `/assistant` | Select an assistant |
| Enter | Send a message; confirm the current selection or approval action |
| Alt+Enter / Ctrl+J | Insert a newline |
| Alt+↑ / Alt+↓ (or PgUp / PgDn) | Scroll chat history or approval details |
| Ctrl+G (or Ctrl+End) | Return to the latest message |
| Ctrl+R | Refresh service data |
| Ctrl+S / `/stop` | Stop the current execution |
| `/expedite` / `/加急` | Expedite sent follow-up messages |
| `/compress` / `/压缩` | Request context compression |
| Ctrl+F / `/attach` | Add attachments |
| Ctrl+E / `/drafts` | Inspect, preview, retry, or remove pending attachments |
| Ctrl+T | Convert composer text into an attachment |
| `/attachments` | Inspect and download chat attachments |
| Ctrl+P / `/approval` | Review a permission request for the current assistant |
| Ctrl+C | Copy a selection; otherwise clear a chat draft including attachments, or exit when empty |
| Esc | Clear the selection or return to the previous page |
| F1 / `/help` | Show help |
| Ctrl+Q / `/quit` | Exit the TUI |

## Connection options

`--server URL` or `MOLR_SERVER` overrides the startup selection and adds the address to the list; a successful connection becomes the next default. `--connections FILE` uses an isolated list and session directory. With `--server`, `--session FILE` selects a session file for that connection and `--ca FILE` trusts an additional PEM certificate. `--login` signs in again; `--logout` revokes the selected session and exits while retaining the connection.

For example, connect to a specific service (replace the example address with your own):

```bash
molr tui --server https://molr.example.com
```

Use `--username NAME` to prefill the login username. Run `molr tui --help` for the complete option list.

## Offline preview

`molr tui --demo` retains offline previews with preset assistants, replies, and simulated approvals. It does not connect to Runtime and discards demo data on exit.

```bash
molr tui --demo
```
