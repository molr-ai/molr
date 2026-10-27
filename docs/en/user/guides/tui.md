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

Enter sends; Alt+Enter or Ctrl+J inserts a newline. The composer grows from one to three visible rows and scrolls additional text. PgUp/PgDn and the wheel scroll history; scrolling upward at the top loads older records. Ctrl+End returns to the latest message. Lost realtime connections reconnect and fetch missed messages. Ctrl+R refreshes manually. Failed sends preserve the draft; retrying the same content with Enter reuses its idempotency key. Runtime retains sent messages after exit; unsent drafts are not saved to disk.

The TUI shows attachment notices. Use the WebUI or desktop app to view or upload attachments, create assistants, and configure resources. Tasks initiated through terminal chat run in Runtime under the assistant’s existing [permissions and budgets](permissions-and-budget.md) and sandbox controls.

## Review permission requests

Ctrl+P or `/approval` opens a pending request with its resource, actions, scope, expiry, and limits. Approve is selected initially; arrows switch to reject and Enter submits. Opening the page does not grant permission. PgUp/PgDn scrolls long requests. Decisions use the displayed request and version; reopen the page after a conflict. Grouped requests are resolved together with the proposed parameters unchanged; use WebUI to edit parameters. Reopen approvals to process the next request.

## Read and copy

User messages and the composer share a background; assistant replies use the terminal background. Messages and help support Markdown tables, quotes, code, bold, and italics; the composer keeps source text. Drag to select visible text, Ctrl+C to copy, and click anywhere or press Esc to clear. Highlighting preserves text styles. Composer and message selections stay within their own regions. Copy requires terminal support and permission for OSC52 clipboard writes. Scrolling or resizing clears the selection.

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
| Ctrl+P / `/approval` | Review a permission request for the current assistant |
| Ctrl+C | Copy selected text, or exit when nothing is selected |
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
