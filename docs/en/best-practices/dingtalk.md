# Use DingTalk

The official [DingTalk DWS CLI](https://github.com/DingTalk-Real-AI/dingtalk-workspace-cli/blob/main/README_zh.md) lets your assistant query calendars, search documents, browse drive files, and perform authorized collaboration tasks. Its command is `dws`.

## Install and Authorize

Complete the [environment setup](README.md#prepare-the-execution-environment), then send:

> Follow https://github.com/DingTalk-Real-AI/dingtalk-workspace-cli to install DWS CLI and the required Skills in your workspace. Check dependencies, use the headless login flow, and send me the authorization link. After authorization, verify access by reading today's calendar.

Official prebuilt binaries and npm installation are available. Have the assistant choose a method suitable for its sandbox and check that the program runs before logging in:

```bash
dws --help
dws auth login --device
```

Open the returned link, select the organization, and authorize access. If CLI access is disabled, follow the application flow and sign in again after your administrator enables it. Verify with a read-only query:

```bash
dws calendar event list
```

## Example Requests

> List today's DingTalk events in time order, including meeting times and material I need to prepare.

> Find DingTalk documents with “quarterly review” in the title. Summarize conclusions and action items from the latest review, with source links.

> Turn these meeting notes into draft tasks with owners and deadlines. Wait for my confirmation before creating them in DingTalk.

For multiple organizations, specify the organization and account. Define the target for sending or writing tasks as described in [task scope](README.md#start-with-a-small-task).

## Verify and Troubleshoot

Continue once the calendar query succeeds for the intended organization; an empty calendar can be valid. For permission errors, check organization CLI access, account permissions, and Molr network grants. For startup failures, ask the assistant to check binary compatibility and sandbox dependencies.

Check `dws <command> --help` before other operations. See the [official guide](https://github.com/DingTalk-Real-AI/dingtalk-workspace-cli/blob/main/README_zh.md) for details.
