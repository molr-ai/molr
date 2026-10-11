<h1 align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="packages/ui/public/brand/molr-wordmark-light.svg" />
    <img src="packages/ui/public/brand/molr-wordmark.svg" alt="Molr" width="256" />
  </picture>
</h1>

<p align="center">
  A self-hosted personal AI assistant.<br />
  Delegate through ongoing conversations, follow progress through plans, and stay in control of permissions and budgets.
</p>

<p align="center">
  <strong>English</strong> | <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <a href="https://molr.ai">Website</a> ·
  <a href="docs/getting-started.md">Getting started</a> ·
  <a href="docs/README.md">Documentation</a> ·
  <a href="https://github.com/molr-ai/molr/issues">Report an issue</a>
</p>

Molr runs on your own device and connects to the models you choose. Work with your assistants from a desktop or mobile browser, review plans, progress, and deliverables in one place, and manage each assistant's memory, file access, and spending.

> **Project status:** Under active development, with no stable release yet. The WebUI and Go Runtime are functional; UI Design uses mock data to demonstrate interactions. Standalone native clients have not been released.

## Features

- **Ongoing conversations and memory** — Each assistant has its own context and memory, with tools to review, edit, and manage long-term memories.
- **Plans and background execution** — Schedule tasks, recurring work, and dependent handoffs with execution records. The browser can be closed while the service keeps running.
- **Your models and budgets** — Connect cloud or local models, assign models and budgets per assistant, and track usage and costs.
- **Explicit resource permissions** — Grant each assistant access to file directories and tools. Linux commands run in a configured, isolated sandbox.
- **Multiple devices and separate accounts** — Continue on desktop or mobile browsers connected to the same service. Families and small teams can allocate resources and quotas through user groups.
- **Plugins and themes** — Extend tools, Skills, themes, and standalone pages through plugins imported and authorized by an administrator.

## Getting started

You can build Linux amd64/arm64 packages from source with the WebUI embedded. GitHub Releases and a public download source are not available yet.

For published builds, choose stable, preview or nightly in the [website installer](https://molr.ai/#install). Available platforms come from each release. Channels without a release show an empty state. The steps below build a package from source.

Install [Node.js](package.json) (`>=22.12.0`), the [Go version specified by this repository](runtime/.go-version), Python 3, and tar, then run:

```bash
git clone https://github.com/molr-ai/molr.git
cd molr
npm ci
npm run build:release
```

Build artifacts are written to `dist/releases/`. To install a default Linux amd64 build, run the command below. For another architecture or a custom version, use the generated filename:

```bash
bash scripts/install.sh --archive dist/releases/molr_0.1.0-local_linux_amd64.tar.gz
```

The target machine needs systemd. The Linux command sandbox also requires Bubblewrap, static BusyBox, and the relevant kernel capabilities. The installer checks dependencies and prints an access address and a one-time login link. After signing in, connect a model, set a budget, and start a conversation.

See [Getting started](docs/getting-started.md) for the full walkthrough, or [Deployment and maintenance](docs/deployment/README.md) for HTTPS, persistent services, sandbox dependencies, and backup and recovery.

## Documentation

The linked product and contributor guides are currently in Simplified Chinese.

| Guide | Contents |
| --- | --- |
| [Product documentation](docs/README.md) | User guides and troubleshooting |
| [Assistants, conversations, and plans](docs/guides/assistants-and-chat.md) | Ongoing conversations, scheduling, results, and revisions |
| [Resources, permissions, and budgets](docs/guides/permissions-and-budget.md) | Model connections, file access, authorization, and costs |
| [Plugins](docs/guides/plugins-and-skills.md) | Tools, Skills, themes, and page extensions |
| [Architecture overview](docs/concepts.md) | Components, execution flow, and data storage |
| [Linux command environment](docs/deployment/sandbox.md) | Sandbox dependencies, limits, and capability checks |
| [Developer documentation](dev-docs/README.md) | Implementation, interfaces, branding, and contribution workflow |

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

Frontends listen on `0.0.0.0` by default. Each worktree needs its own dependencies, data, and available ports. Set `MOLR_RUNTIME_URL` to connect the WebUI to that worktree's own Runtime. See each directory's README for configuration and validation commands, and the [development guide](dev-docs/contributing/development.md) for Runtime build and acceptance requirements.

## License

Molr's own code and documentation are licensed under [MIT](LICENSE). Third-party components retain their respective licenses. See [Third-party notices](THIRD_PARTY_NOTICES.md) and the [licensing and distribution guide](dev-docs/releasing/licensing.md).
