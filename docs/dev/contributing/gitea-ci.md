# Gitea CI

`.gitea/workflows/ci.yml` 在所有 PR、`main` 推送及手动触发时运行 CI，默认覆盖全部平台，也可按下文选择范围。源码检查执行一次，WebUI 与桌面前端产物构建一次，随后由五个平台的原生任务下载并校验版本、提交、依赖锁文件和内容摘要，再构建 Runtime、测试桌面、打包并验证最终安装包。Linux AMD64 和 macOS ARM64 分别执行浏览器测试，四个独立安装任务验证 Unix Runtime 包；本次选择的所有任务成功后 `Gitea CI passed` 才通过。共享前端、Runtime 安装包和桌面安装包保留七天。

桌面验收已并入同一个工作流，不按 PR 路径跳过，也没有单独的 `desktop.yml` 入口。原生矩阵复用 `nativeBuildMatrix()`，按选择的平台过滤，并将 runner 名称转换为下表的本地标签。发布继续由 `.github/workflows/` 管理，其发布门禁仍检查 GitHub 自己的 CI 记录。

## 选择 CI 范围

在仓库 **设置 → Actions → Variables** 配置默认范围；PR 和 `main` 推送自动采用这些变量。手动运行 `Gitea CI` 时可填写同名含义的输入覆盖本次运行，输入留空则沿用仓库变量。未配置时保持全平台验收。

| 仓库变量 | 手动输入 | 可选值 | 默认 |
| --- | --- | --- | --- |
| `MOLR_CI_PLATFORMS` | `platforms` | `all`，或逗号分隔的 `linux-amd64`、`linux-arm64`、`macos-amd64`、`macos-arm64`、`windows-amd64` | `all` |
| `MOLR_CI_INSTALLERS` | `installers` | `enabled` / `disabled`，控制独立 Runtime 安装验收 | `enabled` |

只有一台 Linux AMD64 构建 runner 时，设置：

```text
MOLR_CI_PLATFORMS=linux-amd64
MOLR_CI_INSTALLERS=disabled
```

这会运行源码检查、共享前端构建、Linux AMD64 Runtime 与桌面构建／测试／打包验收，以及 Linux 浏览器测试；不调度 ARM64、macOS、Windows 或独立安装 runner。桌面安装包验收仍执行，`installers=disabled` 只关闭需要可重建环境的独立 Runtime 安装任务。

选择 `all` 恢复五个平台；例如 `linux-amd64,linux-arm64` 选择两种 Linux 架构，需要两种原生 runner。浏览器测试跟随选中的 Linux AMD64 和 macOS ARM64；独立安装验收开启时跟随选中的 Unix 平台。只选 Windows 时没有独立安装任务，Runtime 生命周期由桌面验收覆盖。源码检查、共享前端构建和最终门禁始终需要 `molr-linux-amd64`，即使原生目标只选其他平台。

平台拼写错误、重复项或无效安装开关会让源码任务失败。选择范围不会根据在线 runner 自动缩减；选中但未配置的 runner 会等待执行。未选中的平台不进入矩阵；整组浏览器或安装任务未启用时，最终门禁只接受该组的 `skipped` 状态，已选任务失败、取消或意外跳过均不能通过。源码任务和最终门禁日志会列出实际验证范围；部分平台通过不代表全平台通过，也不改变 GitHub 发布门禁。

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

用当前 runner 的 `config init` 创建配置后修改（`config generate` 可查看完整选项），例如 Linux AMD64 构建 runner：

```yaml
runner:
  capacity: 1
  timeout: 90m
  labels:
    - molr-linux-amd64:host
```

每个 runner 使用自己的注册文件、工作目录、可写工具缓存及测试账号。不要让两个 runner 进程共享同一测试账号并发运行：准备 action 使用固定 systemd 探针单元名，安装测试也会操作用户服务。安装验收环境不注册构建标签，任务完成后销毁或恢复干净快照。macOS/Windows 的原生构建任务会安装、启动最终桌面包并使用用户资料目录，因此这些构建 runner 也必须使用可重建的专用用户环境，每次任务后恢复干净状态。Gitea 1.23 不保证 workflow `timeout-minutes` 生效，因此在 runner 设置超时；安装验收 runner 可设为 `40m`。参见 [runner 配置](https://docs.gitea.com/runner/configuration/)与[标签说明](https://docs.gitea.com/runner/labels/)。

- 所有环境预装 Git、Python 3，以及 action 需要的 Node 24 运行时；Windows 的 `python3` 命令必须实际可用。runner 账号应有工具安装目录的写权限。
- Linux 使用非 root 专用账号和免交互 sudo，提供 systemd 用户服务、cgroup v2 委派、AppArmor、Bash、tar、gzip、file 和基础编译工具。现有 `prepare-sandbox` action 安装沙盒依赖并运行启动探针；桌面 job 安装 Xvfb 和 Electron 图形库，网站测试安装 Chromium 及其依赖。普通 Docker job 不能替代这里的原生环境。
- macOS 安装 Xcode Command Line Tools，提供 `codesign`、`hdiutil`、`file`、tar、Bash 和 Python 3。runner 在专用用户的登录会话中运行，使用户级 launchd 和 Electron 图形测试可用。
- Windows 提供 PowerShell 7（`pwsh`）、Git、tar 和 Python 3，在专用用户的交互桌面会话中执行 Electron 测试；不要用无桌面的服务会话替代图形验收。工作流显式使用 `pwsh`。

Linux 独立安装验收会卸载 Bubblewrap、静态 BusyBox、AppArmor profiles，再由安装器恢复依赖。只能将 `molr-install-*` 标签分配给专用可重建环境，不能分配给日常开发机或其他项目的共享 runner。

## 网络与验证

本地 action 镜像不包含 Node/Go 发行包、npm/Go 模块、Playwright 浏览器、Electron 及打包工具。runner 需能访问这些下载源，也可通过本地 runner 环境配置代理、包源或预装工具缓存。工作流关闭 setup actions 的自动依赖缓存，首次接入无需配置额外缓存服务。`GOCACHE` 和 `GOMODCACHE` 显式指向当前工作区的 `.cache/go-build` 和 `.cache/go-mod`，使工具准备、测试与打包复用同一组缓存，同时保持不同工作区隔离。

工作流使用普通 jobs，不依赖可复用 workflow；原生构建、浏览器和安装任务均检出源码任务记录的同一提交，安装验收下载同次运行的构建产物。共享工具准备位于 `.gitea/actions/setup-build/action.yml`，仍使用本地镜像的 v7 actions。Gitea 1.23 的服务器功能与新 runner 独立，不能通过升级 runner 假定 `permissions`、`concurrency` 或 GitHub 的 PR 合并预览语义生效。参见 [1.23 兼容差异](https://docs.gitea.com/1.23/usage/actions/comparison/)和 [PR 触发说明](https://docs.gitea.com/1.23/usage/actions/faq/)。

浏览器任务失败时，将 `apps/website/test-results/` 上传为 `website-failure-<runner 标签>`，保留七天；没有诊断文件时跳过上传。文档导航和桌面下载测试会保存错误、页面地址、网络／页面异常、截图及 `trace.zip`。下载后可运行 `npx playwright show-trace <trace.zip>` 排查；这些测试只使用人工资料。断言失败仍会使任务及最终门禁失败，上传诊断不改变验证结果。

接入验收依次检查：

1. 手动触发 `Gitea CI`，核对日志中的选择范围，确认源码检查、共享前端构建和所有选中的任务通过。全平台验收显式填写 `platforms=all`、`installers=enabled`，应包含五个原生任务、两个浏览器任务及四个独立安装任务；独立安装任务须完成 server、sandbox 两种模式验收。已选任务失败、取消或意外跳过时，最终门禁必须失败。
2. 检查所选平台的桌面安装包均实际完成解包／安装、应用启动和包内 Runtime 生命周期验收，并能下载对应的 `ci-desktop-*` 和 Unix `ci-runtime-*` 产物。该流程固定构建 `0.1.0-local`：macOS 使用 ad-hoc 签名，Windows 不使用发布者签名，均直接完成打包，不执行 Apple 公证；无需签名证书或发布凭据。
3. 用普通 PR 验证按仓库变量自动选择 CI 范围，不应出现按路径跳过桌面验收的情况。单机接入时验证未选平台不进入等待队列，独立安装组按配置跳过；增加其他 runner 后再扩大变量中的范围并验证。

语法检查与本地测试不等于 Gitea 实跑通过。首次运行需核对服务器、runner、action 镜像具体版本和产物传输结果，日志及实际部署配置保存在私有内部资料中。
