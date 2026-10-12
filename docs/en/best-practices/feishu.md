# Use Feishu

The official [Lark CLI](https://github.com/larksuite/cli) supports calendars, documents, messages, spreadsheets, and tasks. Your assistant uses `lark-cli` within the authorized account and application scope.

## Install and Authorize

Complete the [environment setup](README.md#prepare-the-execution-environment), then send:

> [!PROMPT]
> Follow https://github.com/larksuite/cli to install Lark CLI and the required Skills in your workspace. Guide me through application setup and account authorization, sending any links I need to open. Start by verifying calendar access.

The official installer is `npx @larksuite/cli@latest install`, requiring Node.js and npm. Have the assistant check installation paths and dependencies, then configure the application and log in:

```bash
lark-cli config init
lark-cli auth login --domain calendar
lark-cli auth status
```

Follow the prompts to configure or create an application and open the authorization link in your browser. This example starts with calendar access; add permissions for documents or messages when needed. Both application permissions and user authorization must allow the operation.

## Example Requests

> [!PROMPT]
> Turn today's Feishu events into a timetable and highlight scheduling conflicts.

<!-- -->

> [!PROMPT]
> Read the Feishu project documents I specify. Summarize this week's progress, blockers, and action items, with document links.

<!-- -->

> [!PROMPT]
> Draft a project group message from this weekly report. Show me the text and target group for confirmation before sending.

Provide document links, a time range, or a target group to identify the right resources. For Base updates or new tasks, specify the table, fields, or owner.

## Verify and Troubleshoot

After checking login status, verify an actual query:

```bash
lark-cli calendar +agenda
```

If login succeeds but a resource is inaccessible, check identity, application permissions, user authorization, and resource sharing. User and bot identities can see different resources; specify which identity to use.

Check `lark-cli <service> --help` for parameters and the [official repository](https://github.com/larksuite/cli) for setup details.
