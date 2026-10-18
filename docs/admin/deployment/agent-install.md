# 让 Agent 为我安装

在官网选择「部署后端」或「部署沙盒」，再通过「让 Agent 为我安装」复制指令，发送给能读取网址、操作目标设备终端的 Agent。官网指令默认要求安装正式渠道最新版。系统、架构和下载工具由 Agent 检测。手机上也可以复制指令，但 Agent 仍需要目标设备的终端访问能力。

本文同时以无需 JavaScript 的 Markdown 发布：[中文](https://molr.ai/agent-install.md)、[English](https://molr.ai/en/agent-install.md)。以下是 Agent 的执行流程。

## 确认目标和已有实例

根据用户已有说明确认目标设备。目标不明确时询问，不把 Agent 自己的临时容器或工作区当作安装设备。远程安装使用用户已授权的连接；没有连接能力时说明缺少什么。

检查系统、架构、当前账户、服务管理器、可用空间和已有 Molr 服务及安装记录。原生安装支持范围以发布目录为准。macOS 使用当前普通用户的 launchd 服务，不使用 sudo；Linux 默认使用当前普通用户的 systemd 服务。root 安装必须显式指定非 root 的 `--user USER`。

发现已有实例，先运行该实例的 `molr-setup status --non-interactive`，核对版本、路径和服务。正常运行的实例可以直接交付；升级、修复或另建实例应符合用户意图。不要删除安装记录或数据来绕过问题。维护命令使用实例输出的实际程序路径。

选择 `server`（默认）部署完整 Molr，或选择 `sandbox` 为已有 Molr 添加沙盒节点。两种模式使用同一个 Runtime 包。安装记录保存模式，维护时沿用；不能通过升级或修复切换模式。资源权限预设与运行模式独立。

## 检查发布信息

用户额外指定版本号时，先在发布清单中核对该版本与目标平台，预览和执行均传入 `--version VERSION`，不要改装其他版本。需要明确选择渠道或指令未指定版本时，读取 `https://molr.ai/releases/catalog.json`，检查用户选择的 `stable`、`preview` 或 `nightly` 渠道是否有匹配系统和架构的发行包及其环境要求。用户未指定渠道时使用 stable；渠道为空或没有适用平台时，说明情况并让用户选择，不自动切换渠道。

读取失败时报告发布信息暂不可用，可以在目标设备重试。不要编造版本、下载地址或从非官方来源补包。需要 Docker 时按 [Docker 部署](https://molr.ai/zh/docs/admin/deployment/docker/)处理；其他不支持的环境先说明可选路径。

## 预览安装计划

在目标设备的私有临时目录下载官方脚本，保留脚本用于后续执行。例如已选择 stable 时：

```sh
curl -fsSL --proto '=https' --proto-redir '=https' https://molr.ai/install.sh -o install.sh
sh install.sh --channel stable --mode server --non-interactive --dry-run
```

沙盒节点将两条示例命令中的 `--mode server` 换成 `--mode sandbox`，并可用 `--name molr-node` 区分实例。需要开放命令和目录资源时，在用户授权范围内显式选择 `--resource-preset permissive`；未指定的非交互安装使用最小权限预设。

可用 wget 代替 curl。将示例中的 stable 换成用户所选渠道；无需传入 `--arch`，安装器自动检测本机架构。需要固定预览和执行的版本时，两次调用都指定发布目录中的 `--version VERSION`。下载器会下载并校验发行包，再执行包内 `molr-setup`；不绕过校验。

`--dry-run` 会下载临时包并展示计划，不改变安装账户、目录权限、配置或服务，也不代表运行能力已经验证。根据输出检查安装目录、账户、服务、依赖及权限问题。自定义实例、端口或访问地址时，在预览和执行中使用相同参数：`--name molr-NAME`、`--port PORT`、`--public-host PRIVATE_IPV4`。

向用户简要说明实际变更。已获授权的常规安装继续执行；只有超出既有授权的系统变更才请求具体授权，例如安装系统依赖或调整共享目录权限。

## 执行安装

环境和权限满足要求时，使用同一脚本、渠道和已检查的参数执行：

```sh
sh install.sh --channel stable --mode server --non-interactive --no-browser
```

按检查结果添加参数：

- `--install-deps`：授权安装器准备系统依赖和所需策略；当前自动依赖准备支持 APT。需要的管理员权限必须可用，不能在静默流程中等待密码输入。
- `--directory-permissions fix`：用户授权后移除计划中目录的组／其他用户写权限，不递归改动其内容。不默认使用 ignore。
- `--skip-dependencies`：仅在已手动准备并检查依赖时使用，不能用它掩盖缺失依赖。

`--yes` 不能代替依赖和目录权限的明确选择。安装失败时记录失败阶段、已发生的变更和下一步；修复原实例，保留其数据。不要通过关闭沙盒、放宽系统隔离或启用 Full Access 让安装看似成功。

安装和服务细节见 [安装与服务管理](https://molr.ai/zh/docs/admin/deployment/installation/)。

## 验证与交付

以安装器的实际结果为准：

| 结果 | 含义与后续操作 |
| --- | --- |
| `verified` | 正在运行的版本及认证能力检查通过，可交付访问入口 |
| `verification_pending`，退出码 3 | 已安装但认证验证尚未完成；按输出使用当前凭据继续检查，不重装 |
| `not_started` | 使用了 `--no-start`，服务尚未启动，不能报告安装已可用 |
| 其他失败 | 报告具体失败阶段，检查对应服务日志后处理 |

同版本重复运行可能只显示状态，不能把退出码 0 当作本次验证成功。`server` 模式需要补充验证时，使用实际程序与配置路径运行 `molr doctor -config CONFIG`。已改密码时用 `-password-stdin`，启用 MFA 时按命令要求提供验证码；凭据通过私密输入传递，不写进聊天、命令行参数或公开日志。先等待 HTTP 就绪，再执行认证验证；复用有效会话，遇到 429 停止重复登录并按限流要求等待。

按实际资源配置验证能力，不能根据模式推断权限。启用 Linux 命令执行时，已认证 `/api/sandbox` 应返回 `Enabled=true`、`Capabilities.Reason=available`，且 `Shell`、`ProcessIsolation`、`FileIsolation`、`NetworkIsolation`、`ResourceLimits` 均为 true。macOS 受控执行应返回 Shell、FileIsolation、NetworkIsolation 为 true，NetworkBackend 为 seatbelt-toggle，ProcessIsolation 与 ResourceLimits 为 false。最小权限预设关闭命令执行；server 模式仍提供内置工作区存储，sandbox 模式则关闭整个资源节点。

`sandbox` 模式由安装器使用节点令牌检查 HTTPS 能力接口；不运行 `molr doctor`、不初始化用户或模型、不生成登录链接。交付节点 HTTPS 地址、证书 SHA-256 指纹和令牌文件位置，引导用户在完整服务的「资源 → 沙盒」连接；令牌通过私密渠道传递。以下登录交付步骤仅适用于 `server` 模式。

交付已安装版本与渠道、目标设备、访问地址、服务状态、验证结果，以及尚未完成的步骤。初次安装成功时将安装器生成的 15 分钟一次性登录链接私下交给用户，注明有效期，不预先打开或兑换。链接未生成或过期时可使用 `molr access -config CONFIG`，按需要通过私密输入提供当前凭据；无法取得凭据时说明缺少登录入口，不伪造链接。

确认用户访问路径：远程设备输出的本地或内网地址可能需要现有 SSH 隧道或网络连接，不为此擅自开放公网端口。最后引导用户在初始化向导中连接模型、创建助理；尚未配置模型时如实说明，安装成功不代表已经可以与模型对话。
