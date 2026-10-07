# 独立 Linux Sandbox 服务

Runtime 内置本机 sandbox_manager，无需额外启动本机服务。`sandbox.enabled: false` 关闭本机存储、计算和网络能力，不影响连接远程沙盒。工作区数据保留；重新启用后继续使用。文件操作无需计算授权，但仍受工作区归属与目录权限限制。

远程电脑运行 `molr-sandbox`，只提供受控执行与已授权目录，不初始化账号、Agent、会话、助理工作区或模型服务。助理及其唯一 `/work` 始终位于 Runtime 本地。当前只支持 Linux。两种运行方式复用文件隔离和 Linux 进程隔离实现；公共协议不包含 Bubblewrap 或 cgroup 路径。

「在 WebUI 连接」一节面向管理员，其余内容面向部署者。普通成员使用管理员通过用户组分配的沙盒，并为自己的助理配置授权。

远程节点同样支持按需 [Full Access 权限](sandbox.md#按需授予-full-access)：在远端配置 `sandbox.allow_full_access: true`，再为助理授予该节点的 Full Access 计算权限。本机配置不会替远端开放该能力。普通隔离与显式 Full Access 调用可以共存；后者直接使用远端服务的系统身份和环境。

本次能力协议为版本 5（端点仍为 `/v2/sandbox/`）；Runtime 与远程节点需一同升级，版本不匹配会拒绝连接。

## 构建与启动

在部署检出目录使用 `runtime/.go-version` 指定的 Go 版本：

```bash
mkdir -p .cache/build
cd runtime
CGO_ENABLED=0 go build -o ../.cache/build/molr-sandbox ./cmd/molr-sandbox
chmod 0755 ../.cache/build/molr-sandbox
file ../.cache/build/molr-sandbox
go version -m ../.cache/build/molr-sandbox
```

确认静态链接、`CGO_ENABLED=0` 和 `0755` 权限。与 Runtime 相同，需要 Bubblewrap、静态 BusyBox、user namespace 和 cgroup v2 委派。完整要求见 [Linux 沙盒部署](sandbox.md)。

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

远程服务始终只暴露 `directories` 中的目录，`restrict_directories: false` 不扩大远程协议的范围。实际路径仅留在服务端，Runtime 引用不透明的目录 ID。私有数据目录、工作区和允许的外部目录不得重叠。

使用独立的 systemd 用户服务：

```ini
[Service]
Type=exec
ExecStart=/path/molr-sandbox -config /private/sandbox.json -token-file /srv/molr-sandbox/private/service.token
Delegate=yes
DelegateSubgroup=supervisor
KillMode=control-group
Restart=on-failure
```

服务名使用 `molr-` 前缀，每个实例使用不同的 unit、端口和数据目录。启动日志输出证书指纹，但不会输出服务令牌。当前每个独立服务的凭证信任一个 Runtime；不要把同一服务凭证交给相互不信任的 Runtime。需要不同信任域时运行隔离实例。

## 在 WebUI 连接

1. 进入「资源 → 沙盒 → 连接沙盒」，填写名称、HTTPS 服务地址、证书指纹和令牌，先「测试连接」，通过后保存。修改地址、指纹或令牌后需重新测试。
2. 进入该沙盒的「存储 / 计算 / 网络」分区，添加服务端已开放的资源。计算限制只读展示；目录与网络范围由部署配置管理，页面不能扩大它们。
3. 通过资源卡片「授权给助理」授予外部存储读写、计算执行或网络使用权限，并可查看、修改和撤销已有授权；也可继续在助理权限页管理。连接远程电脑不会自动授予任何资源权限，也不会创建助理工作区。
4. 助理默认在本地工作。远程文件和 Shell 调用必须显式传 `sandbox_id`，并使用该节点的资源；省略节点但引用远程资源会被拒绝。远端不存在助理 `/work`，也不发现远端 AGENTS.md、skills 或记忆。

页面分别显示连接、存储和计算状态；连接成功不代表计算隔离就绪。「重新检查」刷新实际能力，离线时保留连接时的配置快照。沙盒菜单中「重命名」只更改显示名称，无需再次输入令牌；「更新连接凭证」和「断开沙盒」独立处理。桌面端使用沙盒列表，手机端使用选择框；当前沙盒和分区会在同一浏览器标签内记忆。

Runtime 的 `network.allowed_private_ips` 继续限制它连接私网服务。远程 Sandbox 位于私网时，部署者需明确允许该地址；WebUI 无法修改此限制。禁止重定向，解析后拨号仍经过现有网络 guard。Sandbox 的网络资源描述的是执行机器所处的网络，其中回环地址指该执行机器。

本机旧资源继续保留 ID、授权和目录绑定，归入 `local` 沙盒。现有本机 `restrict_directories: false` 保留兼容行为；新页面添加目录只选择部署配置公开的条目。将旧部署切换为严格白名单前，应先把需保留的目录加入配置。

## 协议、恢复与限制

Runtime 和服务使用 `/v2/sandbox/*` HTTPS 协议，证书指纹固定信任目标，服务令牌验证调用方。JSON 端点为 `info`、`submit`、`get`、`renew`、`cancel`、`revoke`、`read`，TCP 转发另使用认证 Upgrade 端点 `forward`；浏览器 Origin 请求被拒绝，这些接口不挂载到 Runtime 的用户 API。

- 文件编辑、Shell、附件导入在执行前记录稳定操作 ID、身份与参数摘要；重复请求返回已有状态，参数冲突拒绝执行。
- Runtime 保存目标沙盒与原执行身份；网络异常先查询结果，不自动生成新操作重试。两端独立数据库，不共享 Runtime SQLite。
- 运行中的操作持有 15 秒授权租约，Runtime 每约 3 秒重新校验授权并续租。撤权提高授权版本、发送终止通知；失联时租约到期终止。网络分区下不承诺瞬时撤权。
- 取消停止进程树，不回滚已写文件。服务重启后的未完成操作标记 `unknown`，不猜测成功或自动重放。
- `/read` 只允许文件下载、输出分段与媒体快照读取；大块临时内容不写入操作账本。附件导入保持现有 20 MiB 上限；媒体沿用现有读取限制。
- Shell 支持前台、后台和 TTY，声明 `ShellSessions` 能力；输出归档与控制沿用 `shell_read` / `shell_control`。端口转发要求 `PortForwarding=true`，通过 Runtime 统一提供 TCP 地址；远端仅需原有 TLS 服务端口，详见 [Shell 端口转发](sandbox.md#shell-端口转发)。
- 文件操作有版本检查和原子替换，但任意 Shell/外部进程写入不是同一个事务，不能把它们当成分布式文件 CAS。

连接令牌在 Runtime 的私有数据库中加密保存，页面和模型只能读取连接元数据。连接管理、资源添加和连接变更记录审计。备份远端时也需要保存它自己的数据库、授权目录和私有配置，Runtime 的备份不包含远端授权目录与操作记录。

启动、升级或重启后，必须认证检查 Runtime `/api/sandbox` 以及 `/api/sandboxes` 中远程实例的能力。Linux 计算就绪要求 `Reason=available`，且 Shell、进程、文件、网络隔离和资源限制均为 true；HTTP 连通不能代替隔离验证。

未来 Windows/macOS 后端的设计要求与平台差异见[沙盒设计](../../dev-docs/runtime/SANDBOX.md)。

## 网络协议升级

本地工作区与显式远程操作使用 Sandbox 能力协议 6（包含通用文件搜索）（传输路径仍为 `/v2/sandbox/`）。旧版本直接拒绝连接或执行，请同步升级 Runtime 和远程 Sandbox；没有代理后端兼容分支。网络配置必须显式声明 TCP/UDP，内置候选预设与限额见 [网络配置](sandbox.md#网络与高级部署)。网络规则与辅助进程在远程机器执行，不改 Runtime 所在机器的网卡、路由或 DNS。

## 文件与执行生命周期

普通远程 Shell 默认从本次命令的 `/tmp` 开始，持久文件只能写入显式挂载的授权目录。远程文件工具必须指定 `sandbox_id` 与目录 `resource_id`；Full Access 调用继续检查授权并使用远端绝对路径，没有 `/work` 别名。插件 Shell 当前只绑定本地计算资源。

`file_transfer` 在本地与一个显式远程节点之间传输单个文件，支持二进制，单次最多 20 MiB。源和目标独立授权，父目录必须存在；必须指定 `overwrite`，覆盖还须给出目标 `expected_version`。不支持远程之间直传、目录同步或自动覆盖。先下载到本地，再用于聊天附件或助理头像。

远程 Shell 的 stdout/stderr 通过分段读取归档到本地 `/work/tool-shell-*.txt`。执行节点由 `SandboxID` 标识，输出节点为 `OutputSandboxID=local`；不要拿执行节点去读取本地日志。断线后按原任务读取日志，不重复执行命令。日志回收不完整时 `OutputComplete=false`。

远端内部输出缓冲按操作保存，不向助理作为存储目录开放。每次 Shell 输出最多 64 MiB（同时受部署文件上限限制），最多保留 128 个操作目录；非运行中目录在超过七天后于下次执行前清理，达到条目上限时先淘汰最旧的非运行中目录；全部在运行时拒绝新命令。回执仍独立保存，日志过期不代表命令未执行。媒体快照仍是有界私有缓存。

远程掉线只影响对应调用。有关任务可等待环境恢复，Runtime 探测恢复后为仍有效的等待任务提交继续事件；取消、暂停、解雇或用户停止的工作不自动恢复。恢复核对已保存的操作结果，不重放未知副作用。本地文件环境不可用时调度器停止领取新事件，消息和任务保留；单独的 Shell 能力故障不等于本地文件环境离线。
