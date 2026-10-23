# Molr - Your personal AI assistant

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="packages/ui/public/brand/molr-wordmark-light.svg" />
    <img src="packages/ui/public/brand/molr-wordmark.svg" alt="Molr" width="256" />
  </picture>
</p>

<p align="center">
  <a href="#near-term-roadmap">
    <img src="https://img.shields.io/badge/status-in%20development-orange" alt="Status: in development" />
  </a>
  <a href="https://github.com/molr-ai/molr/actions/workflows/ci.yml?query=branch%3Amain">
    <img src="https://github.com/molr-ai/molr/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI" />
  </a>
  <a href="https://github.com/molr-ai/molr/releases">
    <img src="https://img.shields.io/github/v/release/molr-ai/molr?include_prereleases&amp;sort=date&amp;label=latest" alt="Latest release, including prereleases and nightly builds" />
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="License: MIT" />
  </a>
  <a href="docs/en/README.md">
    <img src="https://img.shields.io/badge/docs-English-blue" alt="Documentation: English" />
  </a>
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

Molr is a self-hosted personal AI assistant that runs on your own device and connects to the models you choose. Use the desktop app or a browser on your computer or phone to delegate tasks through ongoing conversations, plan work, and review progress and deliverables in one place. You control each assistant's memory, file access permissions, and budget.

![Molr product overview](docs/assets/molr-promotion-en.webp)

> [!WARNING]
> **Project status:** Molr is under active development and has no stable release yet. You may encounter instability, and future versions may introduce breaking changes.

<!-- -->

> [!WARNING]
> **Network access:** Exposing the Molr service directly to the public internet is not currently recommended. Use it in a secure network environment, such as a trusted local network.

Molr provides desktop apps for Linux, macOS, and Windows, alongside the WebUI and Go Runtime. The desktop app can connect to an existing server or install and manage a local service on demand. Mobile users access the WebUI through a browser.

## Features

- **Ongoing conversations and memory** — Each assistant has its own context and memory, with tools to review, edit, and manage long-term memories.
- **Plans and background execution** — Schedule tasks, recurring work, and dependent handoffs with execution records. The browser can be closed while the service keeps running.
- **Your models and budgets** — Connect cloud or local models, assign models and budgets per assistant, and track usage and costs.
- **Explicit resource permissions** — Grant each assistant access to directories, tools, and compute resources. Linux, macOS, and Windows use their own execution backends and report the isolation and resource limits they support.
- **Multiple devices and separate accounts** — Continue in the desktop app or a browser connected to the same service, and use remote sandboxes to access other devices' files and execution resources. Families and small teams can allocate resources and quotas through user groups.
- **Plugins and themes** — Extend tools, Skills, themes, and standalone pages through plugins imported and authorized by an administrator.

## Getting started

Choose the desktop app, a background service, or an independent sandbox node. Download test releases from [GitHub Releases](https://github.com/molr-ai/molr/releases). The [website installer](https://molr.ai/#install) and the default `install.sh` release source offer stable releases only; they do not fall back to prereleases when no stable version is available.

### Desktop app

Download the desktop package for your system and architecture: `.deb` for Linux AMD64/ARM64, `.dmg` for macOS Apple Silicon/Intel, or `.exe` for Windows AMD64.

Open the app and choose to use this computer to create a local service, or connect to a remote server by entering an existing Runtime address. Full desktop packages include Runtime, WebUI, and the local sandbox, so no separate server package is needed; creating the local service still requires confirmation in the app. Closing the desktop window does not stop the running service. See [desktop installation](docs/en/admin/deployment/installation.md#desktop-app) for the steps.

### Background service (Runtime)

For terminal-based deployment on Linux or macOS, download the Runtime `.tar.gz` for your system and architecture and its matching `.sha256` file from Releases. Verify and extract the archive, then run from the package directory:

```bash
./bin/molr-setup --mode server
```

The background service provides accounts, assistants, plans, WebUI, and a local sandbox. Connect to the reported service address with the desktop app or a browser; data stays on the server device. Linux uses systemd, with systemd 254+ and other [sandbox dependencies](docs/en/admin/sandbox/linux.md) required for controlled commands. macOS uses launchd and requires macOS 13 or later. See the [installation guide](docs/en/admin/deployment/installation.md) for online installation, checksum verification, and maintenance.

### Independent sandbox node (Sandbox)

To add another device's files or execution resources to an existing background service, extract the same Runtime package on that Linux/macOS device and run:

```bash
./bin/molr-setup --mode sandbox --name molr-node
```

Connect and authorize the node from the background service's Resources page. A node provides no accounts, assistants, or WebUI. A single-device setup already includes a local sandbox and needs no additional node. See [independent Sandbox services](docs/en/admin/sandbox/remote.md) for connections and permissions.

Packages include neither model weights nor an inference server. Once the background service is ready, connect a local or cloud model, hire an assistant, and set its budget to start chatting.

- [Getting started](docs/en/user/getting-started.md): sign in and start your first conversation.
- [Installation and service management](docs/en/admin/deployment/installation.md): install a specific version, choose a release channel, or install offline.
- [Docker deployment](docs/en/admin/deployment/docker.md): build the complete container image with WebUI.
- [Building from source](docs/en/admin/deployment/source-build.md): build and deploy Molr yourself.
- [Deployment and maintenance](docs/en/admin/README.md): HTTPS, sandbox dependencies, and backup and recovery.

## Near-term roadmap

- [ ] **Multi-assistant group chats** — Bring multiple assistants into one conversation, address participants with @mentions, and share group messages and deliverables.

See the [full roadmap](docs/en/dev/roadmap.md) for scope, completion criteria, longer-term plans, and features under consideration.

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

The repository contains a Go Runtime, an Electron desktop app, three frontend applications, and shared packages:

| Directory | Purpose |
| --- | --- |
| [`runtime/`](runtime/README.md) | Go service, agent execution, and sandbox |
| [`apps/desktop/`](apps/desktop/README.md) | Electron desktop app, connections, and local service management |
| [`apps/webui/`](apps/webui/README.md) | Product interface connected to the real Runtime |
| [`apps/website/`](apps/website/README.md) | Product website and public documentation |
| [`apps/ui-design/`](apps/ui-design/README.md) | Branding, components, and mock interactions |
| [`packages/sdk/`](packages/sdk/README.md) | Runtime client and public protocols |
| [`packages/ui/`](packages/ui/README.md) | Shared components, design tokens, and brand assets |
| [`packages/ui-host/`](packages/ui-host/README.md) | Theme resolution and isolation adapters |

Install dependencies in your task worktree, then start the application you need:

```bash
npm ci
npm run dev:desktop     # Desktop development; local service and packaging prerequisites: apps/desktop/README.md
npm run dev             # WebUI: 5191; requires a separate Runtime
VITE_SITE_MODE=live npm run dev:website # Website: 5189
npm run dev:design      # UI Design: 5188; mock data
```

Frontends listen on `0.0.0.0` by default. Each worktree needs its own dependencies, data, and available ports. Set `MOLR_RUNTIME_URL` to connect the WebUI to that worktree's own Runtime. See each directory's README for configuration and validation commands, and the [development guide](docs/en/dev/contributing/acceptance.md) for Runtime build and acceptance requirements.

## License

Molr's own code and documentation are licensed under [MIT](LICENSE). Third-party components retain their respective licenses. See [Third-party notices](THIRD_PARTY_NOTICES.md) and the [licensing and distribution guide](docs/en/dev/releasing/licensing.md).

## Community

- [Linux Do](https://linux.do/)
