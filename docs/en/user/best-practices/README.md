# Best Practices

Give your assistant a concrete task so it can choose tools to retrieve information, organize results, and collaborate. This chapter explains setup and example requests for common scenarios.

## Choose a Scenario

| Scenario | Connection method | Start here |
| --- | --- | --- |
| Browser | Integration in progress | [Operate a Browser](browser.md) |
| DingTalk | Official DWS CLI | [Use DingTalk](dingtalk.md) |
| Feishu | Official Lark CLI | [Use Feishu](feishu.md) |
| ClawEmail | Official Mail-CLI (`mail-cli`, recommended) | [Use ClawEmail](clawemail.md) |
| Zhihu | Official Zhihu CLI | [Use Zhihu](zhihu.md) |
| Weibo | Official Weibo CLI (`weibo-cli`) | [Use Weibo](weibo.md) |
| X (Twitter) | Official xurl CLI + workspace Skill | [Use X (Twitter)](x.md) |
| GitHub | Official GitHub CLI (`gh`) | [Use GitHub](github.md) |
| Codex coding tasks | Official Codex CLI | [Use Codex](codex.md) |
| ZCode coding tasks | Z.ai's official ZCode CLI | [Use ZCode](zcode.md) |
| Google services | Google Workspace CLI (`gws`) | [Use Google Services](google.md) |

## Prepare the Execution Environment

A CLI is a command-line tool that the assistant runs through Shell to access a platform. Send the setup request from each guide to your assistant so it can follow the official documentation, check dependencies, and guide authorization.

Grant the necessary compute and network resources in the assistant's permissions page. Keep the CLI, dependencies, and persistent configuration inside the assistant's `/work`; official installers may need their default destinations adjusted. Place complete Skill directories under `/work/.agents/skills/`. See [Plugins & Skills](../guides/plugins-and-skills.md) for installation and [Files & Commands](../guides/files-and-commands.md) for workspaces and terminals.

Complete platform login on the official authorization page. Configure credentials using a method supported by the CLI; keep secrets out of Skills, deliverables, and shared scripts. If the sandbox cannot access the system keychain, follow the CLI's official documentation for an appropriate credential method and verify that subsequent commands can read it. Ordinary non-interactive commands do not load `/work/.profile`; environment variables set in one terminal are not automatically inherited by later calls.

## Start with a Small Task

After setup, ask the assistant to retrieve one calendar entry, a specific document, or a few search results. Once the result is correct, combine these operations into a larger workflow.

Specify the account or organization, source material, time range, and output format. For messages, record changes, or new events, identify the target and scope. If you want to review first, ask for a draft before execution. Both Molr resource grants and platform account permissions must allow the operation; see [Permissions & Budget](../guides/permissions-and-budget.md).
