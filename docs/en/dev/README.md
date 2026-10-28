# Molr Development Documentation

This guide is for contributors and maintainers. For everyday tasks, see the [user guide](../user/README.md); for installation, resource connections, and operations, see the [administrator guide](../admin/README.md).

The [roadmap](roadmap.md) lists near-term work, longer-term plans, and features under consideration, with their scope and completion criteria.

## Getting Started with Contributions

Start with the [collaboration guide](contributing/README.md), then read the [development environment](contributing/environment.md), [collaboration workflow](contributing/workflow.md), and [acceptance standards](contributing/acceptance.md) in order. [AGENTS.md](../../../AGENTS.md) lists the standards to read before starting a task.

See [documentation conventions](contributing/documentation.md) for content ownership, writing, translation, and website publishing; see [Gitea CI](contributing/gitea-ci.md) for action mirrors, runners, and separate installation acceptance.

Each application maintains its startup and testing commands: [Runtime](../../../runtime/README.md), [WebUI](../../../apps/webui/README.md), [desktop app](../../../apps/desktop/README.md), [website](../../../apps/website/README.md), and [UI Design](../../../apps/ui-design/README.md).

## Understanding Architecture and Execution

Start with the [architecture overview](architecture/overview.md) for system composition, module responsibilities, and execution flow. Depending on the change, continue with [technical foundations](architecture/tech-stack.md), [context updates](runtime/agent-context.md), [context storage](runtime/context-storage.md), [memory and automatic organization](runtime/dream-memory.md), [model adapters](runtime/model-adapters.md), or [multimodal files](runtime/multimodal-files.md). For execution troubleshooting, see the [development workbench](runtime/workbench.md).

## Modifying Permissions and Isolation

The [security model](security/security-model.md) explains authentication and trust boundaries. [Users and groups](security/users-and-groups.md), [resource quotas](security/resource-quotas.md), and [sandbox implementation](security/sandbox.md) cover identity isolation, quota metering, and execution isolation.

## Consulting Call Contracts

See the [HTTP API](reference/http-api.md) for client requests, [Go module interfaces](reference/module-interfaces.md) for internal calls, and [tool contracts](reference/file-tools.md) for model file and resource operations. The TypeScript client entry point is the [SDK](../../../packages/sdk/README.md).

## Developing Frontends

- [Frontend architecture](frontend/architecture.md) and [themes and page resolution](frontend/themes.md): SDK, Host, page loading, and state recovery.
- [Design system](frontend/design-system.md) and [interaction conventions](frontend/interaction.md): visual, copy, action, and feedback standards.
- [Branding](frontend/brand.md) and [component API](../../../packages/ui/COMPONENTS.md): brand resources and shared component usage.

## Developing Extensions and Channels

- [MCP integration](extensions/mcp.md) and [plugins](extensions/plugins.md): tool discovery, extension protocols, authorization, and lifecycle.
- [WeChat](extensions/weixin.md), [Feishu](extensions/feishu.md), [Telegram](extensions/telegram.md), [Slack](extensions/slack.md), [DingTalk](extensions/dingtalk.md), and [QQ](extensions/qq.md): channel connections, identity linking, and message delivery.

## Maintaining Releases

[Release maintenance](releasing/releases.md) covers versions, channels, and build publication. Before committing and distributing, check [public content and file isolation](releasing/public-content.md) and [licenses](releasing/licensing.md).

- [Continuous plans and watcher protocol](runtime/watchers.md)
