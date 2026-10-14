# Molr - 你的个人 AI 助理

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="packages/ui/public/brand/molr-wordmark-light.svg" />
    <img src="packages/ui/public/brand/molr-wordmark.svg" alt="Molr" width="256" />
  </picture>
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

Molr 是可自托管的个人 AI 助理，在你自己的设备上运行，连接你选择的模型。你可以通过桌面或手机浏览器，用持续对话交代任务，以计划推进执行，并在同一处查看进展与交付结果。每位助理的记忆、资料访问权限和预算都由你管理。

> [!WARNING]
> **项目状态：** Molr 仍处于开发阶段，尚未发布稳定版，可能存在不稳定行为，后续版本也可能包含破坏性变更。

<!-- -->

> [!WARNING]
> **使用环境：** 目前不建议将 Molr 服务直接暴露到公网，请在可信局域网等安全网络环境中使用。

当前提供真实可用的 WebUI 与 Go Runtime；UI Design 使用模拟数据展示交互，独立原生客户端尚未发布。

## 功能亮点

- **持续对话与记忆** — 每位助理拥有独立的上下文和记忆；支持查看、编辑和管理长期记忆。
- **计划与后台执行** — 支持定时、重复安排、前置交接与执行记录；服务运行期间，无需保持网页打开。
- **模型与预算自选** — 接入云端或本地模型，按助理分配模型和预算，查看执行用量与费用。
- **明确的资源权限** — 按助理授权文件目录与工具；Linux 命令在配置好的隔离沙盒中执行。
- **多设备与独立账号** — 桌面、手机浏览器接续同一服务；家庭和小团队可通过用户组分配资源与额度。
- **插件与主题** — 通过管理员导入和授权，扩展 HTTP 工具、主题及独立页面。

## 快速开始

在配有 systemd 254+ 的 Linux AMD64／ARM64 设备上，运行与[官网](https://molr.ai/#install)一致的推荐安装命令：

```bash
curl -fsSL https://molr.ai/install.sh | sh
```

安装器会自动识别架构、检查依赖并配置服务。打开输出的访问地址或一次性登录链接，登录后连接模型、设置预算，即可开始与助理对话。

- [快速开始](docs/user/getting-started.md)：登录并开始第一次对话。
- [安装与服务管理](docs/admin/deployment/installation.md)：指定版本、选择发布渠道或离线安装。
- [从源码构建](docs/admin/deployment/source-build.md)：自行构建与部署。
- [部署与维护](docs/admin/README.md)：HTTPS、沙盒依赖和备份恢复。

## 文档

| 入口 | 内容 |
| --- | --- |
| [用户文档](docs/user/README.md) | 日常使用与常见问题 |
| [管理员文档](docs/admin/README.md) | 安装、资源接入、成员管理与运维 |
| [助理、聊天与计划](docs/user/guides/assistants-and-chat.md) | 持续对话、定时安排、结果与修订 |
| [资源、权限与预算](docs/user/guides/permissions-and-budget.md) | 模型连接、资料范围、授权与费用 |
| [插件](docs/user/guides/plugins-and-skills.md) | HTTP 工具、主题与页面扩展 |
| [架构总览](docs/user/concepts.md) | 组件构成、执行链路与数据存放 |
| [Linux 命令环境](docs/admin/sandbox/linux.md) | 沙盒依赖、运行限制与能力检查 |
| [开发文档](docs/dev/README.md) | 实现说明、接口、品牌与协作规范 |

## 参与开发

欢迎通过 [Issues](https://github.com/molr-ai/molr/issues) 反馈问题，或提交 Pull Request 改进代码和文档。开始前请阅读[项目协作约定](AGENTS.md)，在独立 worktree 中完成开发、测试与文档修改。

仓库由 Go Runtime、三个前端应用和共享包组成：

| 目录 | 用途 |
| --- | --- |
| [`runtime/`](runtime/README.md) | Go 服务、Agent 执行与沙盒 |
| [`apps/webui/`](apps/webui/README.md) | 连接真实 Runtime 的产品界面 |
| [`apps/website/`](apps/website/README.md) | 产品官网与公开文档 |
| [`apps/ui-design/`](apps/ui-design/README.md) | 品牌、组件和模拟交互展示 |
| [`packages/sdk/`](packages/sdk/README.md) | Runtime 客户端与公共协议 |
| [`packages/ui/`](packages/ui/README.md) | 共享组件、设计变量与品牌资源 |
| [`packages/ui-host/`](packages/ui-host/README.md) | 主题解析与隔离适配基础 |

在任务 worktree 中安装依赖，按需启动对应应用：

```bash
npm ci
npm run dev             # WebUI：5191，需要独立 Runtime
VITE_SITE_MODE=live npm run dev:website # 官网：5189
npm run dev:design      # UI Design：5188，模拟数据
```

前端默认监听 `0.0.0.0`。各 worktree 使用独立的依赖、数据和空闲端口；WebUI 通过 `MOLR_RUNTIME_URL` 连接同一 worktree 自己的 Runtime。启动配置与验证命令见各目录的 README，Runtime 构建和验收要求见[开发规范](docs/dev/contributing/development.md)。

## 许可证

Molr 自有代码与文档采用 [MIT 许可证](LICENSE)。第三方组件保留各自许可证，详见[第三方声明](THIRD_PARTY_NOTICES.md)与[许可证及分发说明](docs/dev/releasing/licensing.md)。
