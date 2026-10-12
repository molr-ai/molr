# Files and Commands

First, confirm that the assistant has the required directory and compute permissions. File tools, Shell, and network authorizations are managed separately; see [Permissions and Budgets](permissions-and-budget.md) for specific operations.

## Workspace and Files

Each assistant has an independent local workspace `/work` that persists across sessions. The Runtime initializes the following default directories: `inbox/` stores attachments and imported originals, `projects/` organizes drafts, scripts, and working files by task, and `outputs/` stores deliverables. You can ask the assistant to use other directories; files must be explicitly sent to the chat after saving to complete delivery.

Skills, memory, and reviews use their corresponding directories under `.agents/`; `.local/state/molr/` stores execution records such as tool results and command outputs according to the semantics of the user state directory, persists across restarts, and is not automatically cleaned up currently. `.cache/` is regenerable cache but is not automatically cleaned up currently. `/tmp` belongs only to a single command execution; files needed for subsequent use should be saved in `/work`. The `/global` directory, shared by the same account, is read-only for normal assistant operations.

The assistant automatically receives information about these paths, their purposes, persistence, and availability, and searches for specific files as needed. The root `AGENTS.md` is loaded automatically; when entering a specific working directory, the assistant first lists the directory, then loads instructions from that directory and its parents. Files with the same name in attachments, delivery directories, caches, and runtime artifacts do not automatically become instructions. Directory conventions do not grant additional file or command permissions.

Chat results in global search enter the corresponding assistant, automatically loading the history page where the target is located and highlighting the message; links preserve the message position and remain locatable after refresh. Clicking "Return to latest messages" goes back to the end of the session. If the target does not exist or is inaccessible, a clear prompt is provided.

## How Users Use It

1. Grant execution permissions for compute resources on the permissions page of the assistant information; regular members can only select sandboxes allowed by their user group.
2. When external files are needed, have the administrator connect the directory, then grant the assistant read or read-write scope. The assistant's own local workspace can be used via file tools.
3. Grant network permissions separately when internet access is needed. Allowing command execution does not automatically grant network or model key access.
4. Specify the goal, input files, and expected outputs in the chat. If permissions are insufficient, review the assistant's request and then approve the required scope.

When resources are not ready, the administrator checks [Sandbox Resources](../sandbox/resources.md); system dependencies are handled by the deployer. After disconnecting resources or revoking authorization, restarting does not restore authorization.

## Foreground Commands, Background Tasks, and Terminals

Normal commands executed by the assistant wait synchronously for a maximum of 60 seconds; if not finished, they are converted to background tasks. Programs that need to run continuously can be started directly in the background; commands requiring step-by-step input can open a terminal.

View status and output in the "Running Tasks" section of the chat title bar, and stop individual tasks or all tasks. Terminals can send text, Ctrl-C, and Ctrl-D; sending text appends a newline. The interface provides text input and output but does not provide full-screen terminal emulation. Stopping the assistant's current conversation turn does not close detached background tasks and terminals; they must be stopped in Running Tasks.

The same assistant can run a maximum of 10 concurrent normal isolated executions across all sandboxes, including foreground, background, and terminals; Full Access does not count towards this limit. Tasks are still subject to authorization duration, expiration time, and resource quota limits. Background tasks notify the assistant upon completion, so there is no need to repeatedly ask for progress; an open terminal is not necessarily computing.

The Running Tasks list prioritizes active tasks; recently ended records are collapsed, retaining a maximum of 50 history records. Details display a maximum of the most recent 64 KiB of output; full output can be read from archive files.

Each execution or terminal has an independent `/tmp`, which is cleared upon completion; persistent files are saved to the local `/work` or authorized remote directories. New terminals do not restore processes or environments from previous terminals; environment variables needed for interactive shells can be saved in the local `/work/.profile`. Normal non-interactive commands do not load this file.

Runtime restarts do not restore live processes; leftover tasks are marked as interrupted and are not automatically rerun. Authorization changes, sandbox disconnections, and resource limits may also terminate tasks; cancellation does not roll back written files.

## Shell Port Forwarding

To view web pages created by the assistant, you can request: "Start this project and forward the service port to me." The assistant will obtain an accessible address; use the address provided for delivery, not the localhost in command output.

Running task details show forwarded ports, copy address, and an entry to open HTTP web pages. "Waiting for service" means the connection check has not passed; "Connectable" only means the TCP connection is normal, and web content still needs to be checked. Currently, only administrators can create public port forwards; regular members cannot use this feature.

Forwarding entry points are located on the machine where the Runtime resides; services on remote sandboxes are also accessed through the Runtime. Each task can forward a maximum of four ports, supporting only TCP, including HTTP, WebSocket, and SSE. Other TCP services should use corresponding clients; do not treat HTTP convenience links as protocol identification results.

Anyone who can connect to the address can access the service; Molr does not provide additional login verification or TLS for forwarding. This is suitable for temporary services on explicitly required trusted networks; if the application itself requires authentication, authentication should be configured first. If the address is inaccessible, the deployer checks [Forwarding Listeners and Network](../sandbox/linux.md#forwarding-listen-addresses).

Closing the chat does not close background services; stopping tasks, natural completion, revoking authorization, expiration, or disconnecting the sandbox recovers ports. Forwards are not restored after restart; historical addresses are not suitable as long-term service entry points. Forwarding itself does not require outbound network authorization; service access to external APIs or downloading dependencies still requires network authorization.

## Remote Files

Remote computers do not store assistant workspaces, Skills, or memory. When using remote resources, explicitly tell the assistant the target computer and directory; persistent results must be written to authorized directories on that node, and remote temporary directories are cleared after execution ends.

You can transfer a single file between local and one remote node, with a maximum of 20 MiB per transfer; source and target each require authorization. Overwriting files requires verifying the target version; automatic directory synchronization or direct transfer between two remote nodes is not supported. Remote files that need to be sent to the chat must first be downloaded locally.
