# Roadmap

This page tracks future features as near-term work, longer-term plans, or features under consideration. See the [user guide](../user/README.md) for available capabilities. Near-term items also appear in the root README; longer-term items are not scheduled, and items under consideration still need a decision on whether to proceed. Release dates depend on implementation and acceptance.

## Near-term work

### Desktop app

Users currently access the WebUI through desktop browsers; a standalone desktop client has not been released.

- **Scope**: Provide an installable desktop client that connects to Molr services and supports sign-in, ongoing chats, plan management, and viewing deliverables.
- **Completion criteria**: Validate installation, launch, service connections, sign-in, and core operations on supported desktop systems. Chats, plans, and deliverables stay consistent with the WebUI when using the same account and service.
- **Boundaries**: Supported systems and package formats will be defined during client implementation. Command execution is provided by the connected Runtime and sandbox; Windows sandbox support requires separate acceptance.

### Windows sandbox support

Linux and macOS execution backends exist today; a native Windows backend has not been implemented. See [sandbox implementation](security/sandbox.md).

- **Scope**: Provide controlled native Windows command execution for PowerShell and command-line programs, integrated with existing compute, file, and network permissions and task management. Report the isolation and resource limits actually supported by the platform.
- **Completion criteria**: Validate command execution, authorized directory access, network restrictions, task cancellation, and permission revocation on Windows hardware. The interface displays actual capabilities and identifies unsupported limits.
- **Boundaries**: This item covers the sandbox execution backend. WSL2 is a Linux execution environment; native Windows capabilities require separate acceptance. The isolation mechanism will be selected during implementation design.

### Multi-assistant group chats

Each conversation currently has one assistant; other assistants cannot read or publish to it. Group chats will extend this participant model. See the [conversation model](../../../runtime/internal/im/conversation/api.go) and [participant checks](../../../runtime/internal/im/conversation/service.go).

- **Scope**: Let users create WebUI group chats, add or remove their assistants, and address responders with @mentions. Participants can read shared group messages and explicitly shared deliverables, with each message identifying its sender.
- **Completion criteria**: Two or more assistants can respond when addressed in the same conversation. Removing a participant stops its subsequent reads and messages. Each assistant retains its own resource permissions and budget.
- **Boundaries**: The first version covers assistants owned by the same account. Joining a group does not expose private chats, private memories, or unshared files. Cross-account groups, groups on external chat platforms, and automatic task delegation are outside the initial scope.

## Longer-term plans

### Mobile app

Users can currently access the WebUI through mobile browsers; a standalone mobile client has not been released.

- **Scope**: Provide an installable mobile client that connects to Molr services and supports chats, sending and receiving attachments, viewing plans, and permission approvals, with touch interactions.
- **Completion criteria**: Validate installation, sign-in, core operations, and reconnection after network changes on supported mobile systems and physical devices. Data stays consistent with other clients using the same account and service.
- **Boundaries**: Supported systems and distribution methods will be defined during design. Background push delivery needs a separate design that accounts for mobile platform restrictions and is not implied by app installation.

### Task delegation between assistants

[Plan dependencies and handoffs](../user/guides/plans.md) already exist, while an assistant's [plan tools](../../../runtime/internal/agent/tools/plans.go) are limited to plans assigned to that assistant. This feature adds delegation during execution and collection of results.

- **Scope**: Within collaboration boundaries allowed by the user, an assistant can delegate subtasks to another assistant, share necessary material, wait for results, and assemble the final output. Users can inspect parent and child tasks, responsible assistants, status, and costs.
- **Completion criteria**: Complete a flow covering decomposition, delegation, execution, result return, and assembly. The parent task reflects subtask failure, cancellation, or permission waits. Delegation depth and concurrency are bounded to prevent cycles.
- **Boundaries**: Delegation does not copy the initiating assistant's permissions or private memories. The receiving assistant uses its own permissions and budget, with an explicit scope for shared content.

### Push notifications after closing the page

In-app alerts and desktop notifications already exist, but [notifications are not pushed after the page is closed](../user/guides/settings.md).

- **Scope**: After subscribing a device, users can receive task completion, failure, or permission request notifications with the WebUI page closed. Clicking a notification opens the relevant task or approval view.
- **Completion criteria**: Validate delivery with the page closed, click navigation, deduplication, unsubscription, and disabling delivery after sign-out on supported browsers and devices. Users can control notification types and content previews.
- **Boundaries**: First establish how self-hosted instances connect to push delivery, required network access, and supported platforms. If an external push service is involved, document which data it receives.

## Under consideration

### Simultaneous chat entry points

An assistant's [chat entry points are currently exclusive](../../../runtime/internal/app/chat_channels.go). Connecting an external channel leaves the WebUI available for viewing and management, while chat input moves to that channel.

- **Candidate scope**: Accept messages through both the WebUI and external channels within the same ongoing conversation. Reply through the originating entry point or one selected by the user.
- **Open questions**: Whether messages should sync to every entry point, how identities across platforms should be linked, which history and attachments may cross entry points, and how concurrent input should avoid duplicate replies.
- **Conditions for scheduling**: Establish concrete use cases for simultaneous entry points and decide identity, delivery, and privacy rules before committing to development or scheduling.

## Maintenance

After a feature passes acceptance, document it in the relevant usage guide and remove it from the roadmap. Keep both language versions of the README in sync when near-term scope changes. Check current implementation before adding candidates and describe the new scope so existing capabilities are not listed again as future work.
