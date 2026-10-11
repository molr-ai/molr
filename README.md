<h1 align="center">Molr</h1>

---

<p align="center">
  可自托管的个人 AI 助理。<br />
  用持续对话交代事情，以计划推进执行，让权限与预算始终由你掌握。
</p>

<p align="center">
  <a href="README.en.md">English</a> | <strong>简体中文</strong>
</p>

<p align="center">
  <a href="https://molr.ai">官网</a> ·
  <a href="docs/getting-started.md">快速开始</a> ·
  <a href="docs/README.md">文档</a> ·
  <a href="https://github.com/molr-ai/molr/issues">问题反馈</a>
</p>

Molr 在你自己的设备上运行服务，连接你选择的模型。通过桌面或手机浏览器与助理持续协作，在同一处查看计划、进展和交付结果，并按助理管理记忆、资料访问范围和费用。

> **项目状态：** 正在开发中，尚未发布稳定版。当前提供真实可用的 WebUI 与 Go Runtime；UI Design 使用模拟数据展示交互，独立原生客户端尚未发布。

## 功能亮点

- **持续对话与记忆** — 每位助理拥有独立的上下文和记忆；支持查看、编辑和管理长期记忆。
- **计划与后台执行** — 支持定时、重复安排、前置交接与执行记录；服务运行期间，无需保持网页打开。
- **模型与预算自选** — 接入云端或本地模型，按助理分配模型和预算，查看执行用量与费用。
- **明确的资源权限** — 按助理授权文件目录与工具；Linux 命令在配置好的隔离沙盒中执行。
- **多设备与独立账号** — 桌面、手机浏览器接续同一服务；家庭和小团队可通过用户组分配资源与额度。
- **插件与主题** — 通过管理员导入和授权，扩展 HTTP 工具、主题及独立页面。

## 快速开始

当前支持从源码构建 Linux amd64／arm64 发行包，内嵌 WebUI；GitHub Release 与在线公共下载源尚未接入。

已发布版本可从[官网安装区](https://molr.ai/#install)选择正式版、预发布版或 nightly，安装步骤见[快速开始](docs/getting-started.md)。尚无版本的渠道显示为空；以下步骤用于自行构建。

准备 [Node.js](package.json)（`>=22.12.0`）、[仓库指定版本的 Go](runtime/.go-version)、Python 3 和系统 tar，然后执行：

```bash
git clone https://github.com/molr-ai/molr.git
cd molr
npm ci
npm run build:release
```

产物位于 `dist/releases/`。默认 Linux amd64 构建可使用以下命令安装；其他架构或自定义版本请替换为实际生成的文件名：

```bash
bash scripts/install.sh --archive dist/releases/molr_0.1.0-local_linux_amd64.tar.gz
```

安装目标机需要 systemd；Linux 命令沙盒还需要 Bubblewrap、静态 BusyBox 及相应内核能力。安装器会检查依赖，并输出访问地址和一次性登录链接。登录后连接模型、设置预算，即可开始与助理对话。

完整操作见[快速开始](docs/getting-started.md)；HTTPS、长期运行、沙盒依赖和备份恢复见[部署与维护](docs/deployment/README.md)。

## 文档

| 入口 | 内容 |
| --- | --- |
| [产品文档](docs/README.md) | 使用指南与常见问题 |
| [助理、聊天与计划](docs/guides/assistants-and-chat.md) | 持续对话、定时安排、结果与修订 |
| [资源、权限与预算](docs/guides/permissions-and-budget.md) | 模型连接、资料范围、授权与费用 |
| [插件](docs/guides/plugins-and-skills.md) | HTTP 工具、主题与页面扩展 |
| [架构总览](docs/concepts.md) | 组件构成、执行链路与数据存放 |
| [Linux 命令环境](docs/deployment/sandbox.md) | 沙盒依赖、运行限制与能力检查 |
| [开发文档](dev-docs/README.md) | 实现说明、接口、品牌与协作规范 |

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

前端默认监听 `0.0.0.0`。各 worktree 使用独立的依赖、数据和空闲端口；WebUI 通过 `MOLR_RUNTIME_URL` 连接同一 worktree 自己的 Runtime。启动配置与验证命令见各目录的 README，Runtime 构建和验收要求见[开发规范](dev-docs/contributing/development.md)。

## 许可证

Molr 自有代码与文档采用 [MIT 许可证](LICENSE)。第三方组件保留各自许可证，详见[第三方声明](THIRD_PARTY_NOTICES.md)与[许可证及分发说明](dev-docs/releasing/licensing.md)。
