# Molr - Your personal AI assistant

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="packages/ui/public/brand/molr-wordmark-light.svg" />
    <img src="packages/ui/public/brand/molr-wordmark.svg" alt="Molr" width="256" />
  </picture>
</p>

<p align="center">
  <strong>English</strong> | <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <a href="https://molr.ai">Website</a> ·
  <a href="docs/en/user/getting-started.md">Getting started</a> ·
  <a href="docs/en/README.md">Documentation</a> ·
  <a href="https://github.com/molr-ai/molr/issues">Report an issue</a>
</p>

Molr is a self-hosted personal AI assistant that runs on your own device and connects to the models you choose. From a desktop or mobile browser, delegate tasks through ongoing conversations, use plans to guide execution, and review progress and deliverables in one place. You control each assistant's memory, file access permissions, and budget.

> [!WARNING]
> **Project status:** Molr is under active development and has no stable release yet. You may encounter instability, and future versions may introduce breaking changes.

<!-- -->

> [!WARNING]
> **Network access:** Exposing the Molr service directly to the public internet is not currently recommended. Use it in a secure network environment, such as a trusted local network.

The WebUI and Go Runtime are functional; UI Design uses mock data to demonstrate interactions. Standalone native clients have not been released.

## Features

- **Ongoing conversations and memory** — Each assistant has its own context and memory, with tools to review, edit, and manage long-term memories.
- **Plans and background execution** — Schedule tasks, recurring work, and dependent handoffs with execution records. The browser can be closed while the service keeps running.
- **Your models and budgets** — Connect cloud or local models, assign models and budgets per assistant, and track usage and costs.
- **Explicit resource permissions** — Grant each assistant access to file directories and tools. Linux commands run in a configured, isolated sandbox.
- **Multiple devices and separate accounts** — Continue on desktop or mobile browsers connected to the same service. Families and small teams can allocate resources and quotas through user groups.
- **Plugins and themes** — Extend tools, Skills, themes, and standalone pages through plugins imported and authorized by an administrator.

## Getting started

On a Linux AMD64 or ARM64 device with systemd 254+, run the recommended command from the [website](https://molr.ai/#install):

```bash
curl -fsSL https://molr.ai/install.sh | sh
```

The installer detects the architecture, checks dependencies, and configures the service. Open the access address or one-time login link it prints, then connect a model, set a budget, and start a conversation.

- [Getting started](docs/en/user/getting-started.md): sign in and start your first conversation.
- [Installation and service management](docs/en/admin/deployment/installation.md): install a specific version, choose a release channel, or install offline.
- [Docker deployment](docs/en/admin/deployment/docker.md): build the complete container image with WebUI.
- [Building from source](docs/en/admin/deployment/source-build.md): build and deploy Molr yourself.
- [Deployment and maintenance](docs/en/admin/README.md): HTTPS, sandbox dependencies, and backup and recovery.

## Documentation

Choose the user, administrator, or developer guide for your task.

| Guide | Contents |
| --- | --- |
| [User documentation](docs/en/user/README.md) | Everyday use and troubleshooting |
| [Administrator documentation](docs/en/admin/README.md) | Installation, shared resources, members, and maintenance |
| [Assistants, conversations, and plans](docs/en/user/guides/assistants-and-chat.md) | Ongoing conversations, scheduling, results, and revisions |
| [Resources, permissions, and budgets](docs/en/user/guides/permissions-and-budget.md) | Model connections, file access, authorization, and costs |
| [Plugins](docs/en/user/guides/plugins-and-skills.md) | Tools, Skills, themes, and page extensions |
| [Architecture overview](docs/en/user/concepts.md) | Components, execution flow, and data storage |
| [Linux command environment](docs/en/admin/sandbox/linux.md) | Sandbox dependencies, limits, and capability checks |
| [Developer documentation](docs/en/dev/README.md) | Implementation, interfaces, branding, and contribution workflow |

## Contributing

Report problems through [Issues](https://github.com/molr-ai/molr/issues), or submit a pull request to improve the code and documentation. Read the [contribution guidelines](AGENTS.md) first, and use a separate worktree for development, tests, and documentation changes.

The repository contains a Go Runtime, three frontend applications, and shared packages:

| Directory | Purpose |
| --- | --- |
| [`runtime/`](runtime/README.md) | Go service, agent execution, and sandbox |
| [`apps/webui/`](apps/webui/README.md) | Product interface connected to the real Runtime |
| [`apps/website/`](apps/website/README.md) | Product website and public documentation |
| [`apps/ui-design/`](apps/ui-design/README.md) | Branding, components, and mock interactions |
| [`packages/sdk/`](packages/sdk/README.md) | Runtime client and public protocols |
| [`packages/ui/`](packages/ui/README.md) | Shared components, design tokens, and brand assets |
| [`packages/ui-host/`](packages/ui-host/README.md) | Theme resolution and isolation adapters |

Install dependencies in your task worktree, then start the application you need:

```bash
npm ci
npm run dev             # WebUI: 5191; requires a separate Runtime
VITE_SITE_MODE=live npm run dev:website # Website: 5189
npm run dev:design      # UI Design: 5188; mock data
```

Frontends listen on `0.0.0.0` by default. Each worktree needs its own dependencies, data, and available ports. Set `MOLR_RUNTIME_URL` to connect the WebUI to that worktree's own Runtime. See each directory's README for configuration and validation commands, and the [development guide](docs/dev/contributing/development.md) for Runtime build and acceptance requirements.

## License

Molr's own code and documentation are licensed under [MIT](LICENSE). Third-party components retain their respective licenses. See [Third-party notices](THIRD_PARTY_NOTICES.md) and the [licensing and distribution guide](docs/en/dev/releasing/licensing.md).
