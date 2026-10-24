# 搭建 Molr 实例

> [!WARNING]
> **项目状态：** Molr 仍处于开发阶段，尚未发布稳定版，可能存在不稳定行为，后续版本也可能包含破坏性变更。

<!-- -->

> [!WARNING]
> **使用环境：** 目前不建议将 Molr 服务直接暴露到公网，请在可信局域网等安全网络环境中使用。

本文面向安装并准备服务的人。已有账号的成员从[用户快速开始](../user/getting-started.md)进入。安装和系统配置需要服务器访问权限；接入资源和管理成员需要 Molr 管理员账号。

## 安装与初始化

本指南部署 Runtime 的 `server` 模式（默认），提供 WebUI、账号、助理和本机沙盒能力。已有完整服务、只需增加其他设备资源时，使用同一 Runtime 包的 `sandbox` 模式，按[沙盒节点部署](sandbox/remote.md)操作；节点不初始化账号或助理。桌面应用的本机服务与远程连接见[安装指南](deployment/installation.md#桌面应用)。

使用 Docker 时，按 [Docker 部署](deployment/docker.md)构建包含 Runtime 与 WebUI 的镜像，并完成配置与账号初始化；下文为原生安装方式。

从官网「部署后端」复制安装命令。默认使用 curl，自动检测系统与架构，安装正式渠道最新版；没有正式发行包时显示“即将推出”。原生安装支持 Linux 和 macOS 的 AMD64／ARM64，安装包包含 Runtime 与 WebUI。Linux 的 Bubblewrap、静态 BusyBox 等依赖由目标机系统包管理器提供；macOS 使用系统 Seatbelt。

也可直接运行安装脚本，使用默认 stable 渠道，无需保存脚本文件：

```bash
curl -fsSL https://molr.ai/install.sh | sh -s -- --mode server
```

需要指定渠道时，在命令末尾添加 `--channel preview` 或 `--channel nightly`；渠道及版本说明见[安装与服务管理](deployment/installation.md)。官网不提供渠道切换，其命令始终安装正式渠道最新版。使用者无需安装 Go、Node.js 或单独部署网页。

首次安装默认使用当前普通用户，从 8787 开始选择空闲端口并生成独立配置。Linux 安装 systemd 用户服务；root 执行时须通过 `--user molr` 指定非 root 运行账户。依赖缺失时，apt 系系统会询问是否安装；其他系统按输出补齐依赖。systemd 用户管理器必须可用；安装器自动检测并尝试开启 linger，以便退出登录后继续运行；若权限不足或未能验证开启，按提示执行管理员命令并确认 `Linger=yes`。macOS 13 及以上使用当前普通用户的 launchd 服务，不使用 sudo。

资源权限预设与运行模式独立。各安装渠道首次安装默认使用个人预设（`permissive`），开放服务账号可访问的目录、网络和计算资源，助理仍需授权。命令行可用 `--resource-preset least-privilege` 选择最小权限，关闭命令执行；完整服务仍提供内置工作区存储。具体范围见[资源权限预设](deployment/installation.md#runtime-模式与资源权限预设)。

安装完成会输出局域网访问地址和有效期 15 分钟的一次性登录链接。点击链接并确认登录后进入首次引导：

1. 选择自动发现的本机模型服务，或配置一个云端／自定义 Provider。
2. 连接后确认所选模型及默认每日 USD 1 预算，可以修改。未知价格需要补充；填 0 表示明确确认免费。
3. 点击「开始使用」，自动创建 **Molr** 助理并进入聊天。首次问候会调用所选模型并计入预算。

语言和时区根据浏览器设置。命令行交互安装默认要求设置管理员用户名和密码，不保存明文密码。无人值守安装可显式添加 `--auto-init-admin`，沿用自动创建 `owner` 和随机密码的方式，密码保存在数据目录的 `initial-password` 文件（0600）。也可用 `--init-owner NAME` 指定用户名并通过标准输入提供密码。需要重新生成登录链接时，使用安装目录的程序：

```bash
/path/molr access -config /path/server.json -username YOUR_ADMIN -password-stdin
```

将 `YOUR_ADMIN` 替换为安装时设置的用户名，通过标准输入提供当前密码。只有自动初始化且初始密码仍有效时可省略 `-password-stdin`，从文件读取；启用 MFA 后还需 `-mfa-code`。重跑安装会保留原配置、账号和数据，不重置密码或重复创建助理。

局域网可以使用 HTTP，无需证书。HTTP 不加密密码、会话及消息，仅适用于可信网络。HTTPS 配置见[部署与维护](README.md)。系统通知等浏览器能力可能要求 HTTPS，页面内提醒仍可使用。

## 接入资源

按[连接与管理模型](resources/models.md)核对模型能力与价格。助理需要文件或命令能力时，再准备[沙盒资源](resources/sandbox.md)。连接资源不会自动授予助理权限。

## 开放给成员

多人使用时，按[用户与用户组](administration/users-and-groups.md)创建组、分配模型和工具及配额，再创建成员账号。新建用户组默认没有资源。个人部署可以直接使用初始化的管理员账号。

## 开始使用与维护

进入[用户快速开始](../user/getting-started.md)，聘请助理并验证一个小任务。HTTPS 与网络设置见[配置与访问](deployment/configuration.md)，版本更新和备份见[维护指南](deployment/maintenance.md)。
