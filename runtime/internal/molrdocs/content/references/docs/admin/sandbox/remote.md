# 独立 Sandbox 服务

Runtime 内置本机 sandbox_manager，无需额外启动本机服务。主 Runtime 的 `sandbox.enabled: false` 关闭本机命令执行，工作区、文件工具和远程连接仍可用。Runtime 的 `sandbox` 模式中，同名开关仍控制整个远程节点；远程节点支持 Linux、macOS 和 Windows。文件操作不要求计算授权，但仍受工作区归属与目录权限限制。

远程电脑运行 `molr serve --mode sandbox`，只提供受控执行与已授权目录，不初始化账号、Agent、会话、助理工作区或模型服务。助理及其唯一 `/work` 始终位于 Runtime 本地。节点复用对应平台的文件工具与执行后端：Linux 使用 Bubblewrap，macOS 使用 Seatbelt，Windows 使用 AppContainer；公共协议不包含 Bubblewrap 或 cgroup 路径。

本页面向部署者。服务启动后由管理员[连接沙盒](https://molr.ai/zh/docs/admin/resources/sandbox/#在-webui-连接)，成员使用所属用户组允许的资源并为自己的助理授权。

远程节点同样支持按需 [Full Access 权限](https://molr.ai/zh/docs/admin/sandbox/linux/#按需授予-full-access)：在远端配置 `sandbox.allow_full_access: true`，再为助理授予该节点的 Full Access 计算权限。本机配置不会替远端开放该能力。普通隔离与显式 Full Access 调用可以共存；后者直接使用远端服务的系统身份和环境。

当前 Sandbox 能力协议为版本 9，传输路径为 `/v2/sandbox/`；Runtime 与远程节点版本必须匹配。Linux 网络配置显式声明 TCP/UDP，规则在执行机器生效，见[网络配置](https://molr.ai/zh/docs/admin/sandbox/linux/#网络与高级部署)。

## 使用安装器

Linux/macOS 服务包同时包含 `molr` 和 `molr-setup`。在目标机器执行：

```bash
./bin/molr-setup --mode sandbox --name molr-node --resource-preset permissive
```

可选择 `least-privilege` 预设，默认关闭资源，随后按需调整。安装器安装同一 Runtime 与维护程序，并配置 `sandbox` 模式，准备 systemd／launchd 服务、HTTPS 证书及私有令牌，验证节点能力后输出连接信息。令牌保存在数据目录的 `sandbox-token`，不会打印在安装日志中。使用自定义配置时通过 `--config` 提供 HTTPS 配置与证书；令牌仍由安装器生成，不使用 `--init-owner`。

通过安装后的 `molr-setup status` 查看连接信息，`update` 升级，`uninstall` 卸载并保留配置和数据。完整预设说明见[安装指南](https://molr.ai/zh/docs/admin/deployment/installation/#runtime-模式与资源权限预设)。

## 构建与启动

在部署检出目录使用 `runtime/.go-version` 指定的 Go 版本：

```bash
mkdir -p .cache/build
cd runtime
CGO_ENABLED=0 go build -o ../.cache/build/molr ./cmd/molr
chmod 0755 ../.cache/build/molr
file ../.cache/build/molr
go version -m ../.cache/build/molr
```

确认 Linux 静态 ELF 或 macOS Mach-O、`CGO_ENABLED=0` 和 `0755` 权限。macOS 产物执行 `codesign --force --sign - <binary>` 并用 `codesign --verify --strict <binary>` 验证。Linux 与 Runtime 相同，需要 Bubblewrap、静态 BusyBox、user namespace 和 cgroup v2 委派。完整要求见 [Linux 沙盒部署](https://molr.ai/zh/docs/admin/sandbox/linux/)。

准备一个独立的私有数据目录，例如 `/srv/molr-sandbox/private`。TLS 私钥和服务令牌必须放在该目录内，不能包含符号链接，令牌文件权限为 `0600`。使用随机生成的至少 32 字符令牌；不要把令牌写入启动参数或提交到 Git。可使用可信 CA 或自行签发的 TLS 证书；连接时显式核对证书 SHA-256 指纹，不能跳过验证。

服务使用 Runtime 的严格配置格式。例如：

```json
{
  "data_dir": "/srv/molr-sandbox/private",
  "http": {
    "listen": "0.0.0.0:8443",
    "public_url": "https://sandbox.example.com:8443",
    "tls_mode": "direct",
    "cert_file": "/srv/molr-sandbox/private/server.crt",
    "key_file": "/srv/molr-sandbox/private/server.key"
  },
  "sandbox": {
    "enabled": true,
    "restrict_directories": true,
    "directories": [
      {"path": "/srv/projects", "read_only": false},
      {"path": "/srv/reference", "read_only": true}
    ],
    "networks": [
      {"name": "Git service", "protocols": ["tcp"], "addresses": ["git.example.com"], "ports": ["443"]}
    ]
  }
}
```

`restrict_directories: true` 时，管理员可接入 `directories` 配置的根目录或其子目录；false 时可填写远端本机路径。目录在远端解析和校验，Runtime 保存目录引用，每次执行都重新检查当前部署范围。私有数据目录、安装资产、工作区和允许的外部目录不得重叠。

Linux 使用独立的 systemd 用户服务：

```ini
[Service]
Type=exec
ExecStart=/path/molr serve --mode sandbox -config /private/sandbox.json -token-file /srv/molr-sandbox/private/service.token
Delegate=yes
DelegateSubgroup=supervisor
KillMode=control-group
Restart=on-failure
```

服务名使用 `molr-` 前缀，每个实例使用不同的 unit、端口和数据目录。启动日志输出证书指纹，但不会输出服务令牌。每个沙盒实例只供一个 Runtime 使用，请勿在多个 Runtime 之间复用同一实例或服务令牌，即使这些 Runtime 由同一管理员维护。当前服务不会强制独占，也没有按 Runtime 隔离权限和操作状态；多个 Runtime 需要分别部署独立实例，各自使用独立的服务令牌。

## macOS 节点

macOS 使用相同的 `molr serve --mode sandbox -config … -token-file …` 命令、TLS 与令牌认证。独立节点支持 `molr-setup --mode sandbox` 安装，也可从源码构建。构建应在目标 Mac 原生完成，ARM64 与 AMD64 分别验收。

配置中的 `data_dir`、证书、私钥、令牌和目录全部使用 Mac 上的绝对路径。将上例的 `sandbox` 替换为：

```json
{
  "enabled": true,
  "allow_full_access": false,
  "directories": [
    {"path": "/Users/example/Projects", "read_only": false}
  ],
  "network_presets": ["network"],
  "tool_directories": ["/opt/homebrew", "/usr/local", "/Library/Developer/CommandLineTools"]
}
```

不配置 Linux 的 `networks`、Bubblewrap、cgroup 或硬配额。受控执行的文件、网络、进程清理和端口转发边界沿用 [macOS 原生命令权限](https://molr.ai/zh/docs/admin/sandbox/macos/)。计算授权只限制时长；网络为独立授权开关，无配额。网络到期、撤销或范围变化时终止受管任务；失联继续受 15 秒租约限制。Full Access 仍需部署开关和独立授权。

远程节点不创建助理工作区或共享目录，不提供 `$MOLR_WORKSPACE`、`$MOLR_GLOBAL`。受控命令的 `HOME` 和 `TMPDIR` 是每次执行的私有临时目录。文件工具继续使用目录资源 ID 和资源相对路径；Shell 使用权限列表返回的 `ShellEnvironment`：在 `files` 中传入该项的准确 `ResourceID` 和 `Path`，脚本通过对应变量访问，并给变量展开加双引号。例如变量名为 `MOLR_RESOURCE_<摘要>` 时，使用 `"$MOLR_RESOURCE_<摘要>/file.txt"`（替换为返回的完整变量名）。目录实际路径由节点注入，不放入目录发现协议。

`directory` 可使用 `/tmp`，或 `/resources/<ResourceID>/<Path>` 工作目录别名。别名仅用于工作目录解析，不是脚本中的挂载路径；脚本文本不会被改写。授权子目录时使用对应项的变量，未请求的目录不会注入。

使用独立 launchd 配置管理服务。下面路径都需要替换；日志目录须预先创建：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>ai.molr.remote-sandbox</string>
  <key>ProgramArguments</key><array>
    <string>/Users/example/molr/bin/molr</string>
    <string>serve</string>
    <string>--mode</string>
    <string>sandbox</string>
    <string>-config</string><string>/Users/example/molr/private/sandbox.json</string>
    <string>-token-file</string><string>/Users/example/molr/private/service.token</string>
  </array>
  <key>WorkingDirectory</key><string>/Users/example/molr</string>
  <key>LimitLoadToSessionType</key><array><string>Aqua</string><string>Background</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>StandardOutPath</key><string>/Users/example/molr/private/stdout.log</string>
  <key>StandardErrorPath</key><string>/Users/example/molr/private/stderr.log</string>
</dict></plist>
```

保存为 `~/Library/LaunchAgents/ai.molr.remote-sandbox.plist`，使用 `launchctl bootstrap gui/$(id -u) <plist绝对路径>` 加载到当前登录会话。仅通过 SSH 运行时使用 `user/$(id -u)` 域。此部署方式依赖用户会话；不将注销、休眠或服务重启视为任务仍在运行的保证。恢复后查询原操作状态，不自动重放命令。

启动后从主 Runtime 的 `/api/sandboxes` 检查该节点：`Storage=true`、`Reason=available`、`Shell`、`FileIsolation`、`NetworkIsolation` 为 true，`NetworkBackend=seatbelt-toggle`；`ProcessIsolation`、`ResourceLimits` 为 false。独立节点 `enabled:false` 同时关闭执行与存储。

## 协议、恢复与限制

Runtime 和服务使用 `/v2/sandbox/*` HTTPS 协议，证书指纹固定信任目标，服务令牌验证调用方。JSON 端点为 `info`、`submit`、`get`、`renew`、`cancel`、`revoke`、`read`，TCP 转发另使用认证 Upgrade 端点 `forward`；浏览器 Origin 请求被拒绝，这些接口不挂载到 Runtime 的用户 API。

- 文件编辑、Shell、附件导入在执行前记录稳定操作 ID、身份与参数摘要；重复请求返回已有状态，参数冲突拒绝执行。
- Runtime 保存目标沙盒与原执行身份；网络异常先查询结果，不自动生成新操作重试。两端独立数据库，不共享 Runtime SQLite。
- 运行中的操作持有 15 秒授权租约，Runtime 每约 3 秒重新校验授权并续租。撤权提高授权版本、发送终止通知；失联时租约到期终止。网络分区下不承诺瞬时撤权。
- 执行结果保存失败时，服务保留结果并退避重试保存，不重复执行操作；保存成功前继续占用执行名额。关闭会停止重试并报告未保存错误。结果超过持久记录限制时保存明确的 `unknown` 状态，不自动重放。
- Linux 取消停止进程树；macOS 取消终止受管进程组，不保证清理脱离进程组的后代。不回滚已写文件。服务重启后的未完成操作标记 `unknown`，不猜测成功或自动重放。
- `/read` 只允许文件下载、输出分段与媒体快照读取；大块临时内容不写入操作账本。附件导入保持现有 20 MiB 上限；媒体沿用现有读取限制。
- Shell 支持前台、后台和 TTY，声明 `ShellSessions` 能力；输出归档与控制沿用 `shell_read` / `shell_control`。端口转发要求 `PortForwarding=true`，通过 Runtime 统一提供 TCP 地址；远端仅需原有 TLS 服务端口，详见 [Shell 端口转发](https://molr.ai/zh/docs/user/guides/files-and-commands/#shell-端口转发)。
- 文件操作有版本检查和原子替换，但任意 Shell/外部进程写入不是同一个事务，不能把它们当成分布式文件 CAS。

连接令牌在 Runtime 的私有数据库中加密保存，页面和模型只能读取连接元数据。连接管理、资源添加和连接变更记录审计。`molr-setup backup` 在 Sandbox 模式保存节点数据库、服务令牌、证书和部署配置，不打包授权目录内容或命令输出缓存。授权目录文件由部署者单独迁移；Runtime 的备份只保留远程连接及授权信息。操作见[备份恢复](https://molr.ai/zh/docs/admin/deployment/maintenance/#备份恢复与升级)。

启动、升级或重启后，必须认证检查 Runtime `/api/sandbox` 以及 `/api/sandboxes` 中远程实例的能力。Linux 计算就绪要求 `Reason=available`，且 Shell、进程、文件、网络隔离和资源限制均为 true；HTTP 连通不能代替隔离验证。

Windows 节点通过源码构建的 `molr.exe serve --mode sandbox` 和手动配置启动；桌面安装器目前只管理完整 Runtime。Windows 使用 PowerShell 和网络开关，`Capabilities.Terminal=true` 时支持 ConPTY 终端，不支持受控端口转发或硬配额。受控目录应为原生盘符路径，脚本使用 `$env:<ShellEnvironment>`；`/tmp` 和 `/resources/...` 只作为工作目录参数别名解析。平台差异见[沙盒设计](https://molr.ai/zh/docs/dev/security/sandbox/)。

## 文件与执行生命周期

普通远程 Shell 默认从本次命令的 `/tmp` 开始，持久文件只能写入显式挂载的授权目录。远程文件工具必须指定 `sandbox_id` 与目录 `resource_id`；Full Access 调用继续检查授权并使用远端绝对路径，没有 `/work` 别名。CLI 本地安装使用助理的 `/work`；远程执行仍需显式选择远程资源和目录。

`file_transfer` 在本地与一个显式远程节点之间传输单个文件，支持二进制，单次最多 20 MiB。源和目标独立授权，父目录必须存在；必须指定 `overwrite`，覆盖还须给出目标 `expected_version`。不支持远程之间直传、目录同步或自动覆盖。先下载到本地，再用于聊天附件或助理头像。

远程 Shell 的 stdout/stderr 通过分段读取归档到本地 `/work/.local/state/molr/tool-shell-*.txt`。执行节点由 `SandboxID` 标识，输出节点为 `OutputSandboxID=local`；不要拿执行节点去读取本地日志。断线后按原任务读取日志，不重复执行命令。日志回收不完整时 `OutputComplete=false`。

远端内部输出缓冲按操作保存，不向助理作为存储目录开放。每次 Shell 输出最多 64 MiB（同时受部署文件上限限制），最多保留 128 个操作目录；非运行中目录在超过七天后于下次执行前清理，达到条目上限时先淘汰最旧的非运行中目录；全部在运行时拒绝新命令。回执仍独立保存，日志过期不代表命令未执行。媒体快照仍是有界私有缓存。

远程掉线只影响对应调用。有关任务可等待环境恢复，Runtime 探测恢复后为仍有效的等待任务提交继续事件；取消、暂停、解雇或用户停止的工作不自动恢复。恢复核对已保存的操作结果，不重放未知副作用。本地文件环境不可用时调度器停止领取新事件，消息和任务保留；单独的 Shell 能力故障不等于本地文件环境离线。

部署后由管理员按[连接远程沙盒](https://molr.ai/zh/docs/admin/resources/sandbox/#在-webui-连接)测试连接、添加资源和分配成员范围。
