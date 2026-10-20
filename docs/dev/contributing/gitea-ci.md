# Gitea CI

`.gitea/workflows/ci.yml` 在 PR、`main` 推送及手动触发时执行 Linux/macOS 检查、构建和独立安装验收；`desktop.yml` 在相关 PR 或手动触发时执行五个平台的桌面测试和打包。产物保留七天。检查和构建复用仓库现有脚本，发布继续由 `.github/workflows/` 管理。

## 本地 action 镜像

在 Gitea 的 `actions` 组织创建以下拉取镜像，保留上游分支和标签以及已构建的 `dist/`。工作流使用当前大版本引用；更新镜像后先运行手动 CI 验证。

| 本地仓库 | 上游 | 工作流引用 |
| --- | --- | --- |
| `actions/checkout` | <https://github.com/actions/checkout> | `v7` |
| `actions/setup-node` | <https://github.com/actions/setup-node> | `v7` |
| `actions/setup-go` | <https://github.com/actions/setup-go> | `v7` |
| `actions/gitea-upload-artifact` | <https://github.com/ChristopherHX/gitea-upload-artifact> | `v7` 分支 |
| `actions/gitea-download-artifact` | <https://github.com/ChristopherHX/gitea-download-artifact> | `v8` 分支 |

前三个 action 使用正式版本大标签；后两个使用维护者提供的 Gitea 兼容分支，不能替换为 GitHub 官方 artifact 仓库。下载只访问当前运行的产物，不传 `github-token` 或跨运行查询参数。无需额外同步 Docker、Python 或 cache actions。

在 **Gitea 服务端**实际使用的 `app.ini` 中设置，然后重启 Gitea：

```ini
[actions]
ENABLED = true
DEFAULT_ACTIONS_URL = self
```

Docker 部署通常使用容器内 `/data/gitea/conf/app.ini`；也可在 Compose 的 Gitea 服务中设置 `GITEA__actions__DEFAULT_ACTIONS_URL: self` 后重新创建容器。它不是 runner 配置或仓库 variable。该设置对整个实例生效：`actions/checkout@v7` 会解析到当前 Gitea 的 `actions/checkout`。其他仓库未镜像的 actions 需要补镜像或改为完整上游 URL。参见 [Gitea 配置说明](https://docs.gitea.com/1.23/administration/config-cheat-sheet/#actions-actions)。

仓库设置中启用 Actions。服务器和 runner 均须能访问实例的实际 `ROOT_URL`，包括 artifact 返回的下载地址。公开工作流不保存内网地址或注册令牌。

## 原生 runner

安装支持 `runs.using: node24` 的当前稳定版 Gitea Runner，并为 action 提供 Node 24 运行时。项目构建由 `setup-node` 安装 Node `22.21.0`，Go 版本读取 `runtime/.go-version`；action 运行时与项目工具链是两个要求。先验证 checkout、setup-node、setup-go 及其后续 artifact action 均可执行，不能仅凭 runner 在线判断兼容。

每个执行环境只注册对应架构的标签，使用 **host 模式**。以下是五种构建环境及四种独立安装验收环境；标签不代表必须配置九台物理机器，可以由独立虚拟机提供。

| 系统 | 构建／桌面标签 | 独立安装验收标签 |
| --- | --- | --- |
| Ubuntu 24.04 AMD64 | `molr-linux-amd64:host` | `molr-install-linux-amd64:host` |
| Ubuntu 24.04 ARM64 | `molr-linux-arm64:host` | `molr-install-linux-arm64:host` |
| macOS 15 Intel | `molr-macos-amd64:host` | `molr-install-macos-amd64:host` |
| macOS 15 Apple Silicon | `molr-macos-arm64:host` | `molr-install-macos-arm64:host` |
| Windows Server 2025 AMD64 | `molr-windows-amd64:host` | 桌面流程覆盖原生 Runtime 生命周期 |

用当前 runner 的 `generate-config` 生成配置后修改，例如 Linux AMD64 构建 runner：

```yaml
runner:
  capacity: 1
  timeout: 90m
  labels:
    - molr-linux-amd64:host
```

每个 runner 使用自己的注册文件、工作目录、可写工具缓存及测试账号。不要让两个 runner 进程共享同一测试账号并发运行：准备 action 使用固定 systemd 探针单元名，安装测试也会操作用户服务。安装验收环境不注册构建标签，任务完成后销毁或恢复干净快照。Gitea 1.23 不保证 workflow `timeout-minutes` 生效，因此在 runner 设置超时；安装验收 runner 可设为 `40m`。参见 [runner 配置](https://docs.gitea.com/runner/configuration/)与[标签说明](https://docs.gitea.com/runner/labels/)。

- 所有环境预装 Git、Python 3，以及 action 需要的 Node 24 运行时；Windows 的 `python3` 命令必须实际可用。runner 账号应有工具安装目录的写权限。
- Linux 使用非 root 专用账号和免交互 sudo，提供 systemd 用户服务、cgroup v2 委派、AppArmor、Bash、tar、gzip、file 和基础编译工具。现有 `prepare-sandbox` action 安装沙盒依赖并运行启动探针；桌面 job 安装 Xvfb 和 Electron 图形库，网站测试安装 Chromium 及其依赖。普通 Docker job 不能替代这里的原生环境。
- macOS 安装 Xcode Command Line Tools，提供 `codesign`、`hdiutil`、`file`、tar、Bash 和 Python 3。runner 在专用用户的登录会话中运行，使用户级 launchd 和 Electron 图形测试可用。
- Windows 提供 PowerShell 7（`pwsh`）、Git 和 Python 3，在专用用户的交互桌面会话中执行 Electron 测试；不要用无桌面的服务会话替代图形验收。工作流显式使用 `pwsh`。

Linux 独立安装验收会卸载 Bubblewrap、静态 BusyBox、AppArmor profiles，再由安装器恢复依赖。只能将 `molr-install-*` 标签分配给专用可重建环境，不能分配给日常开发机或其他项目的共享 runner。

## 网络与验证

本地 action 镜像不包含 Node/Go 发行包、npm/Go 模块、Playwright 浏览器、Electron 及打包工具。runner 需能访问这些下载源，也可通过本地 runner 环境配置代理、包源或预装工具缓存。工作流关闭 setup actions 的自动依赖缓存，首次接入无需配置额外缓存服务。

两个工作流使用普通 jobs，不依赖可复用 workflow；完整安装验收在同一次运行中下载构建产物。Gitea 1.23 的服务器功能与新 runner 独立，不能通过升级 runner 假定 `permissions`、`concurrency` 或 GitHub 的 PR 合并预览语义生效。参见 [1.23 兼容差异](https://docs.gitea.com/1.23/usage/actions/comparison/)和 [PR 触发说明](https://docs.gitea.com/1.23/usage/actions/faq/)。

接入验收依次检查：

1. 手动触发 `Gitea CI`，确认四个构建任务均匹配正确系统／架构并通过，四个独立安装任务下载同次运行的包并完成 server、sandbox 两种模式验收。
2. 手动触发 `Gitea desktop acceptance`，确认五个平台的 Electron、原生 Runtime 生命周期和安装包构建通过，artifact 可下载；此流程不需要签名证书或发布凭据。
3. 用普通 PR 验证 CI 自动触发，再用桌面相关路径的 PR 验证桌面 workflow 的路径过滤。缺少任一 runner 时矩阵不完整，不能把其余任务成功视为全平台通过。

语法检查与本地测试不等于 Gitea 实跑通过。首次运行需核对服务器、runner、action 镜像具体版本和产物传输结果，日志及实际部署配置保存在私有内部资料中。
