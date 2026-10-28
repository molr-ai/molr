# TUI user guide

Use `molr tui` to connect to an existing Molr service from your terminal, select assistants, exchange messages, and review permission requests. Connect to the same service and account to continue conversations from the WebUI or desktop app.

## Prerequisites

Have a working `molr` executable, a service address, and an account. The executable ships with Runtime; see the [installation guide](../../admin/deployment/installation.md). Use a terminal of at least 42 columns by 18 rows, with both standard input and output connected to an interactive terminal.

If you are new to Molr, follow the [Quick start](../getting-started.md) in WebUI to create an assistant and choose a model. Existing assistants are ready to connect.

## Start and connect

Run in an interactive terminal:

```bash
molr tui
```

By default, the TUI restores the last successfully connected Runtime. With no previous selection, it opens connection management. Use arrows and Enter to open a saved connection; choose “Add connection” or press N to add an address. Name and CA certificate path are optional. Tab changes fields and Enter saves and connects. Failed connections keep the list and error visible for retry or another selection.

Press Ctrl+O or send `/connections` from chat to open connection management; Esc returns to the current chat. Switching Runtimes cancels the previous connection's requests and realtime subscription. Connections, login sessions, and in-process chat drafts are isolated by Runtime and account. Returning to the same account restores its drafts.

## Login and sessions

First-time connections open a separate login page for username and password, followed by an MFA code when enabled. Passwords and codes are masked and never saved to disk. TUI connections and the last selection live in `molr/tui/connections.json` under the user configuration directory; each session has its own file under `molr/tui/sessions/`, separate from desktop application data, with mode `0600`. After session expiry, reopen connection management and select that connection to sign in again.

## Select assistants and chat

The terminal uses existing Runtime assistants, conversation history, and permission requests. Chat, assistant selection, approvals, and help use separate borderless full-screen pages. Ctrl+K or `/assistant` opens selection; type to search names, use arrows, and press Enter. Switching assistants preserves drafts, cursors, and scroll positions during the current run. If there are no assistants, create one in WebUI and press Ctrl+R to refresh.

Enter sends; Alt+Enter or Ctrl+J inserts a newline. The composer grows from one to five visible rows and scrolls additional text. PgUp/PgDn and the wheel scroll history; scrolling upward at the top loads older records. Ctrl+End returns to the latest message. Lost realtime connections reconnect and fetch missed messages. Ctrl+R refreshes manually. Failed sends preserve the draft; retrying the same content with Enter reuses its idempotency key. Runtime retains sent messages after exit; unsent drafts are not saved to disk.

Message bodies support up to 4000 characters. At the limit, the TUI preserves the draft and prompts you to convert it into a text attachment with Ctrl+T. Tasks run in Runtime under the assistant’s [permissions and budgets](permissions-and-budget.md) and sandbox controls. Create assistants and configure resources in WebUI or the desktop app.

When an assistant uses an external channel such as WeChat or Telegram, the TUI shows that channel and pauses ordinary chat input and new attachments, preserving drafts. Send messages through that channel, or disconnect it in WebUI under “Switch chat channel” to restore TUI input. Sending also waits until channel discovery completes; Ctrl+R checks again. Stopping execution, approvals, history, and attachment downloads remain available. `/channel` displays recovery guidance.

On macOS iTerm2, use Option+Enter for a newline. TUI requests modifier reporting from terminals supporting the Kitty keyboard protocol and restores the previous mode on exit. Enable **Keys → General → Apps can change how keys are reported** in the current profile. If Option+Enter still sends, the terminal may still encode it as ordinary Enter. Add an Option+Enter mapping under **Keys → Key Mappings**, select **Send Hex Code**, and enter `0x1b 0x0d`, or use Ctrl+J. The app cannot recover an Option modifier already lost by the terminal. See [iTerm2 keyboard settings](https://iterm2.com/documentation-preferences-profiles-keys.html).

## Attachments and long pastes

Ctrl+F or `/attach` opens file selection. Use arrows to browse, Enter to open directories or add files, Space for multiple selection, and Backspace for the parent directory. Ctrl+L edits the path; Tab completes a unique match. `/attach ./report.xlsx` also accepts a path, including `~`, spaces, and paired quotes. Paths refer to the machine running the TUI; over SSH, they refer to the remote machine.

Files upload into the current assistant’s draft. Enter sends them with explanatory text, or on their own. Each message supports up to five attachments of at most 20 MiB each; server deployment limits and account quotas still apply. Upload progress is displayed. Ctrl+E or `/drafts` opens pending attachments: R retries, D or Delete removes. Retries reuse the original bytes and idempotency key. Failed sends retain text and attachments; success removes only the attachments included in that send. Switching services cancels transfers; retry when returning.

With terminal bracketed paste support, a single paste exceeding 4000 Unicode characters, reaching 20 lines, or taking the existing body past 4000 characters becomes a UTF-8 text attachment instead of message body text. Attachments show their filename, size, and upload status. Short screens show only the first few attachments; Ctrl+E lists all, Enter previews pasted text, PgUp/PgDn scrolls, and Esc returns. Original line endings and tabs are preserved; previews sanitize terminal control characters. Ctrl+T converts the complete current composer text into an attachment so you can add a short instruction. Size or count limit failures explicitly report that the attachment was not added.

Text attachments are not automatically inserted into model context as message body text. Normal and expedited delivery carry attachment references; the assistant uses attachment and file tools to read content as needed. Content it reads may still enter context.

Messages display attachment names and sizes. `/attachments` lists files from loaded chat history. Select one, press Enter to edit the full destination path, then Enter to download. Existing files require replacement confirmation; Esc returns to path editing. Esc cancels an active download. Successful downloads show the saved path. Size and SHA-256 are verified before publishing the destination; failed downloads leave no partial file. Load older chat history to access earlier attachments.

A paste that remains inline and exceeds 100 characters or two lines becomes a purple text block, distinct from ordinary text, showing its character count and a short preview. Each paste is a separate block. Left/right arrows move across the whole block; Backspace or Delete removes it in one action. Type ordinary text before or after blocks. Sending expands each block into its original position in the message body, without uploading a file or sending the preview label. The 4000-character limit counts expanded original text. Switching assistants preserves their individual blocks.

In chat with no mouse selection, Ctrl+C first clears the current text, pasted blocks, and pending attachments, cancelling active uploads. It exits only when the composer is completely empty. Clearing does not retract submitted messages or stop Runtime execution. With a mouse selection, Ctrl+C still copies; Ctrl+Q always exits.

## Execution controls and commands

Ctrl+S or `/stop` (`/停止`) interrupts the current turn without rolling back completed writes. Exiting the TUI does not stop Runtime tasks. While working, send additional messages normally, then use `/expedite` (`/加急`) to prioritize unread messages through the current sequence; already running tools finish first.

`/compress` (`/压缩`) requests context compression without sending a chat message or removing draft attachments. Type `/`, use arrows to select a command, Tab or Enter to complete, and Enter again to execute. Esc dismisses completion. Complete commands execute directly.

`/permission approve|reject` (`/权限审批 同意|拒绝`) handles the latest pending request for the current assistant. Append a complete request ID for an exact match, or `all` / `全部` for all requests pending when the command was submitted. IDs and versions are rechecked before execution. Batches preflight all selected requests, stop on failure, and report how many succeeded; completed decisions remain applied. Approval preserves requested parameters. Missing resources or quotas require the WebUI approval form.

## Review permission requests

Ctrl+P or `/approval` opens a pending request with its resource, actions, scope, expiry, and limits. Approve is selected initially; arrows switch to reject and Enter submits. Opening the page does not grant permission. PgUp/PgDn scrolls long requests. Decisions use the displayed request and version; reopen the page after a conflict. Grouped requests are resolved together with the proposed parameters unchanged; use WebUI to edit parameters. Reopen approvals to process the next request.

## Read and copy

User messages and the composer share a background; assistant replies use the terminal background. Messages and help support Markdown tables, quotes, code, bold, and italics; the composer keeps source text. Drag to select visible text in messages or the composer; releasing the mouse copies automatically. Ctrl+C copies again; click anywhere or press Esc to clear. Highlighting preserves text styles. Composer and message selections stay within their own regions. Copy requires terminal support and permission for OSC52 clipboard writes. The status confirms that the request was sent to the terminal, not that the system clipboard accepted it. Scrolling or resizing clears the selection.

## Shortcut reference

| Shortcut or command | Action |
| --- | --- |
| Ctrl+O / `/connections` | Open connection management |
| Ctrl+K / `/assistant` | Select an assistant |
| Enter | Send a message; confirm the current selection or approval action |
| Alt+Enter / Ctrl+J | Insert a newline |
| PgUp / PgDn | Scroll chat history or approval details |
| Ctrl+End | Return to the latest message |
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
