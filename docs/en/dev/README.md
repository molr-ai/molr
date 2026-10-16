# Molr Development Documentation

This guide is for contributors and maintainers. For everyday tasks, see the [user guide](../user/README.md); for installation, resource connections and operations, see the [administrator guide](../admin/README.md).

## Getting Started with Contributions

First, read [Development Collaboration and Acceptance](../../dev/contributing/development.md) and conduct tasks according to the worktree specifications; for documentation content ownership, link updates, and official website release checks, see [Documentation Maintenance Conventions](contributing/documentation.md). [AGENTS.md](../../../AGENTS.md) lists mandatory coding standards.

Local startup and testing commands are maintained by each application: [Runtime](../../../runtime/README.md), [WebUI](../../../apps/webui/README.md), [Official Website](../../../apps/website/README.md), [UI Design](../../../apps/ui-design/README.md).

## Understanding Architecture and Execution

Start with the [Architecture Overview](architecture/overview.md) to understand system composition, module responsibilities, and execution chains, then read [Technical Foundation](architecture/tech-stack.md), [Context Updates](runtime/agent-context.md), [Context Storage](runtime/context-storage.md), [Memory and Automatic Organization](runtime/dream-memory.md), [Model Adaptation](runtime/model-adapters.md), or [Multimodal Files](runtime/multimodal-files.md) based on the scope of changes. For troubleshooting execution processes, see [Development Workbench](runtime/workbench.md).

## Modifying Permissions and Isolation

[Security Model](security/security-model.md) explains authentication and trust boundaries; [Users and User Groups](security/users-and-groups.md), [Resource Quotas](security/resource-quotas.md), and [Sandbox Implementation](security/sandbox.md) respectively maintain identity isolation, quota metering, and execution isolation mechanisms.

## Consulting Call Contracts

For client requests, see [HTTP API](reference/http-api.md); for internal module calls, see [Go Module Interfaces](reference/module-interfaces.md); for model files and resource operations, see [Tool Contracts](reference/file-tools.md). For the TypeScript client entry point, see [SDK](../../../packages/sdk/README.md).

## Developing Frontend and Extensions

- [Frontend Architecture](frontend/architecture.md), [Theme and Page Parsing](frontend/themes.md): SDK, Host, page loading, and state recovery.
- [Branding](frontend/brand.md), [Design System](frontend/design-system.md), [Interaction Conventions](frontend/interaction.md): Visual resources, semantic colors, accessibility, and shared interactions; for component properties, see [Component API](../../../packages/ui/COMPONENTS.md).
- [Plugin Mechanism](extensions/plugins.md), [WeChat Channel](extensions/weixin.md), [Telegram Channel](extensions/telegram.md): Extension protocols, authorization, lifecycle, and message delivery.

## Maintaining Releases

[Release Maintenance](releasing/releases.md) manages versions, channels, and build releases; before committing and distributing, verify [Public Content and File Isolation](releasing/public-content.md) and [Licenses](releasing/licensing.md).
