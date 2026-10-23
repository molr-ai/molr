# Molr - 你的个人 AI 助理

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="packages/ui/public/brand/molr-wordmark-light.svg" />
    <img src="packages/ui/public/brand/molr-wordmark.svg" alt="Molr" width="256" />
  </picture>
</p>

<p align="center">
  <a href="#近期路线图">
    <img src="https://img.shields.io/badge/status-in%20development-orange" alt="状态：开发中" />
  </a>
  <a href="https://github.com/molr-ai/molr/actions/workflows/ci.yml?query=branch%3Amain">
    <img src="https://github.com/molr-ai/molr/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI" />
  </a>
  <a href="https://github.com/molr-ai/molr/releases/latest">
    <img src="https://img.shields.io/github/v/release/molr-ai/molr?label=stable" alt="最新正式版" />
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="许可证：MIT" />
  </a>
  <a href="docs/README.md">
    <img src="https://img.shields.io/badge/docs-%E7%AE%80%E4%BD%93%E4%B8%AD%E6%96%87-blue" alt="文档：简体中文" />
  </a>
</p>

<p align="center">
  <a href="README.md">English</a> | <strong>简体中文</strong>
</p>

<p align="center">
  <a href="https://molr.ai">官网</a> ·
  <a href="docs/user/getting-started.md">快速开始</a> ·
  <a href="docs/README.md">文档</a> ·
  <a href="https://github.com/molr-ai/molr/issues">问题反馈</a>
</p>

Molr 是可自托管的个人 AI 助理，在你自己的设备上运行，连接你选择的模型。你可以通过桌面应用或电脑、手机浏览器，用持续对话交代任务，以计划推进执行，并在同一处查看进展与交付结果。每位助理的记忆、资料访问权限和预算都由你管理。

![Molr 产品宣传图](docs/assets/molr-promotion-zh-cn.webp)

> [!WARNING]
> **项目状态：** Molr 仍在持续开发，可能存在不稳定行为，后续版本也可能包含破坏性变更。

<!-- -->

> [!WARNING]
> **使用环境：** 目前不建议将 Molr 服务直接暴露到公网，请在可信局域网等安全网络环境中使用。

当前提供 Linux、macOS 和 Windows 桌面应用、WebUI 与 Go Runtime。桌面应用可连接已有服务，也可按需安装和管理本机服务；手机通过浏览器使用 WebUI。

## 功能亮点

- **持续对话与记忆** — 每位助理拥有独立的上下文和记忆；支持查看、编辑和管理长期记忆。
- **计划与后台执行** — 支持定时、重复安排、前置交接与执行记录；服务运行期间，无需保持网页打开。
- **模型与预算自选** — 接入云端或本地模型，按助理分配模型和预算，查看执行用量与费用。
- **明确的资源权限** — 按助理授权文件目录、工具与计算资源；Linux、macOS 和 Windows 使用各自的执行后端，按实际能力报告隔离与资源限制。
- **多设备与独立账号** — 桌面应用和浏览器接续同一服务，通过远程沙盒使用其他设备的文件与执行资源；家庭和小团队可通过用户组分配资源与额度。
- **插件与主题** — 通过管理员导入和授权，扩展工具、Skills、主题及独立页面。

## 快速开始

推荐从[官网](https://molr.ai/#install)下载桌面应用，或使用下方命令安装后台服务；两种方式默认提供最新正式版。

| 你的使用场景 | 推荐方式 |
| --- | --- |
| 在自己的电脑上开始使用，希望通过图形界面完成安装和管理 | **桌面应用**：下载安装后，在应用中创建本机服务；已有服务时也可直接连接 |
| 在常开设备或服务器上运行助理，供电脑、手机或多个账号访问 | **后台服务（Runtime）**：在目标 Linux/macOS 设备执行安装命令，再用浏览器或桌面应用连接 |
| 已有后台服务，只想增加另一台设备的文件访问或执行资源 | **独立沙盒节点（Sandbox）**：在资源所在设备安装，再由已有服务连接并授权 |

手机通过浏览器访问已运行的后台服务，无需安装客户端。助理任务和数据保存在运行后台服务的设备上；需要持续执行任务时，应让该设备保持开机、联网且服务运行。

### 桌面应用

前往[官网下载安装](https://molr.ai/#install)，选择适合系统和架构的桌面包：Linux AMD64／ARM64 使用 `.deb`，macOS Apple Silicon／Intel 使用 `.dmg`，Windows AMD64 使用 `.exe`。

打开应用后，选择“在这台电脑上使用”创建本机服务，或“连接远程服务”填写已有 Runtime 的地址。桌面完整版已包含 Runtime、WebUI 和本机沙盒，无需另装服务包；创建本机服务仍需在应用内确认。关闭桌面窗口不会停止已运行的后台服务。具体操作见[桌面应用安装](docs/admin/deployment/installation.md#桌面应用)。

### 后台服务（Runtime）

在要运行助理的 Linux 或 macOS 设备终端执行：

```bash
curl -fsSL https://molr.ai/install.sh | sh -s -- --mode server
```

安装脚本自动检测系统与架构，下载并校验最新正式版，再引导完成服务安装与管理员账号设置。Windows 用户可通过桌面应用创建本机服务。

后台服务提供账号、助理、计划、WebUI 和本机沙盒。安装后用桌面应用或浏览器连接输出的服务地址，数据保存在运行服务的设备。Linux 使用 systemd，启用受控命令还需 systemd 254+ 等[沙盒依赖](docs/admin/sandbox/linux.md)；macOS 使用 launchd，要求 macOS 13 或更新版本。在线安装、校验和维护步骤见[安装指南](docs/admin/deployment/installation.md)。

### 独立沙盒节点（Sandbox）

已有后台服务，需要使用另一台设备的文件或执行资源时，在那台 Linux/macOS 设备的终端执行：

```bash
curl -fsSL https://molr.ai/install.sh | sh -s -- --mode sandbox --name molr-node
```

再从后台服务的资源页面连接节点并授权。节点不提供账号、助理或 WebUI；单机使用已经内置沙盒，不必额外部署节点。连接与权限设置见[独立 Sandbox 服务](docs/admin/sandbox/remote.md)。

安装包不包含模型权重或推理服务。后台服务就绪后，连接本地或云端模型、聘请助理并设置预算，即可开始对话。

- [快速开始](docs/user/getting-started.md)：登录并开始第一次对话。
- [安装与服务管理](docs/admin/deployment/installation.md)：指定版本、选择发布源或离线安装；历史版本与预发布包可从 [GitHub Releases](https://github.com/molr-ai/molr/releases) 获取。
- [Docker 部署](docs/admin/deployment/docker.md)：构建包含 WebUI 的完整容器镜像。
- [从源码构建](docs/admin/deployment/source-build.md)：自行构建与部署。
- [部署与维护](docs/admin/README.md)：HTTPS、沙盒依赖和备份恢复。

## 近期路线图

- [ ] **多助理群聊** — 在同一会话中加入多位助理，通过 @ 指定参与者，共享群内消息和交付物。

范围与完成标准，以及远期计划和待定项，见[完整路线图](docs/dev/roadmap.md)。

## 文档

| 入口 | 内容 |
| --- | --- |
| [用户文档](docs/user/README.md) | 日常使用与常见问题 |
| [管理员文档](docs/admin/README.md) | 安装、资源接入、成员管理与运维 |
| [助理、聊天与计划](docs/user/guides/assistants-and-chat.md) | 持续对话、定时安排、结果与修订 |
| [资源、权限与预算](docs/user/guides/permissions-and-budget.md) | 模型连接、资料范围、授权与费用 |
| [插件](docs/user/guides/plugins-and-skills.md) | 工具、Skills、主题与页面扩展 |
| [架构总览](docs/user/concepts.md) | 组件构成、执行链路与数据存放 |
| [Linux 命令环境](docs/admin/sandbox/linux.md) | 沙盒依赖、运行限制与能力检查 |
| [开发文档](docs/dev/README.md) | 实现说明、接口、品牌与协作规范 |

## 参与开发

欢迎通过 [Issues](https://github.com/molr-ai/molr/issues) 反馈问题，或提交 Pull Request 改进代码和文档。开始前请阅读[项目协作约定](AGENTS.md)，在独立 worktree 中完成开发、测试与文档修改。

仓库由 Go Runtime、Electron 桌面应用、三个前端应用和共享包组成：

| 目录 | 用途 |
| --- | --- |
| [`runtime/`](runtime/README.md) | Go 服务、Agent 执行与沙盒 |
| [`apps/desktop/`](apps/desktop/README.md) | Electron 桌面应用、连接管理与本机服务管理 |
| [`apps/webui/`](apps/webui/README.md) | 连接真实 Runtime 的产品界面 |
| [`apps/website/`](apps/website/README.md) | 产品官网与公开文档 |
| [`apps/ui-design/`](apps/ui-design/README.md) | 品牌、组件和模拟交互展示 |
| [`packages/sdk/`](packages/sdk/README.md) | Runtime 客户端与公共协议 |
| [`packages/ui/`](packages/ui/README.md) | 共享组件、设计变量与品牌资源 |
| [`packages/ui-host/`](packages/ui-host/README.md) | 主题解析与隔离适配基础 |

在任务 worktree 中安装依赖，按需启动对应应用：

```bash
npm ci
npm run dev:desktop     # 桌面应用开发；本机服务和打包前提见 apps/desktop/README.md
npm run dev             # WebUI：5191，需要独立 Runtime
VITE_SITE_MODE=live npm run dev:website # 官网：5189
npm run dev:design      # UI Design：5188，模拟数据
```

前端默认监听 `0.0.0.0`。各 worktree 使用独立的依赖、数据和空闲端口；WebUI 通过 `MOLR_RUNTIME_URL` 连接同一 worktree 自己的 Runtime。启动配置与验证命令见各目录的 README，Runtime 构建和验收要求见[开发规范](docs/dev/contributing/acceptance.md)。

## 许可证

Molr 自有代码与文档采用 [MIT 许可证](LICENSE)。第三方组件保留各自许可证，详见[第三方声明](THIRD_PARTY_NOTICES.md)与[许可证及分发说明](docs/dev/releasing/licensing.md)。

## 社区

- [Linux Do](https://linux.do/)
