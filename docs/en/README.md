# Molr Product Documentation

Molr is a self-hostable personal AI assistant. Connect to your chosen models, delegate tasks via chat, and view schedules and results in plans.

## Getting Started

- [Quick Start](getting-started.md): Install, log in, and complete your first delegation; existing accounts can start from logging in.
- [Core Concepts](concepts.md): Assistant, Plan, Memory, Resources, and the boundaries between services and data.

## Daily Usage

- [Assistant & Chat](guides/assistants-and-chat.md), [Plans & Execution](guides/plans.md): Delegate tasks, schedule time, and view results.
- [Memory & Auto-Organization](guides/memory.md): View, edit, confirm, or forget memories.
- [Permissions & Budget](guides/permissions-and-budget.md): Approve resource scopes, set quotas, and revoke authorizations.
- [Files & Commands](guides/files-and-commands.md): Workspaces, background tasks, terminals, and temporary services.
- [Plugins & Skills](guides/plugins-and-skills.md): Use installed plugins and skills.
- [Personal Settings](guides/settings.md), [Account Security](guides/account-security.md): Appearance, reminders, passwords, and login devices.

## Connect Models

- [Connect and Manage Models](models/README.md): Connect model providers and configure model capabilities, pricing, and credentials.

## Connect Chat Channels

- [WeChat Chat Entry](chat-channels/weixin.md): Connect WeChat to chat with an assistant, manage the connection, and disconnect.

## Sandbox

- [Manage Sandbox Resources](sandbox/resources.md): Connect execution hosts and add storage, compute, and network resources.
- [Linux Sandbox Deployment](sandbox/linux.md): Prepare local dependencies and configure isolation, GPU access, and network scopes.
- [Standalone Linux Sandbox Service](sandbox/remote.md): Deploy a remote execution node and connect it to Molr.

## Best Practices

Start with the [Best Practices Overview](best-practices/README.md) to prepare tools and permissions, then explore [Operate a Browser](best-practices/browser.md), [Use DingTalk](best-practices/dingtalk.md), [Use Feishu](best-practices/feishu.md), [Use Zhihu](best-practices/zhihu.md), [Use GitHub](best-practices/github.md), and [Use Google Services](best-practices/google.md).

## Managing Instances

Administrators manage members and allocate available resources through [Users & User Groups](administration/users-and-groups.md). For plugin installation, enabling, and shared connections, see [Plugin Management](administration/plugins.md).

## Deployment & Maintenance

The [Deployment Entry](deployment/README.md) provides steps based on installation method and runtime environment, covering service installation, access configuration, Linux sandbox, remote nodes, updates, and backup recovery.

When encountering issues with connections, permissions, budgets, or execution, locate the problem by page symptoms in [Troubleshooting](troubleshooting.md).

## Contributing to Development

For implementation mechanisms, interface contracts, and contribution processes, see [Development Documentation](../../dev-docs/en/README.md).
