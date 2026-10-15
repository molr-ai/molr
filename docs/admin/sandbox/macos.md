# macOS 原生命令权限

macOS 受控命令通过系统 `/usr/bin/sandbox-exec` 加载 Seatbelt 策略，与 Full Access 使用同一个原生工具环境和助理工作区。计算、文件和网络仍分别使用现有资源授权、用户组范围、审批与有效期；Full Access 继续独立授权。

## 开启与授权

安装器默认关闭命令执行。在服务配置中设置 `sandbox.enabled: true`，重启后在「沙盒」中检查计算状态，再授予助理计算权限。受控命令不需要启用 `allow_full_access`。

```json
{
  "sandbox": {
    "enabled": true,
    "allow_full_access": false,
    "network_presets": ["network"],
    "tool_directories": ["/opt/homebrew", "/usr/local", "/Library/Developer/CommandLineTools"]
  }
}
```

`tool_directories` 是部署者选择的只读工具目录，不会自动安装工具。不存在的目录会跳过；工具目录不能包含 Runtime 私有数据、工作区或受保护安装目录。可缩小列表，只保留实际需要的工具。系统 Shell、系统库及基础工具由基础策略提供。

| 权限 | macOS 执行效果 |
| --- | --- |
| 计算 | 允许执行；可设置单次最长时长或不限时长 |
| 文件 | 自己的工作区可读写，账户共享目录只读；外部资源按获批路径授予读或读写 |
| 网络访问 | 单一 `preset:network` 资源；允许任意目的地址的 TCP/UDP、DNS，以及任意接口和端口的监听 |
| Full Access | 需要部署开关、用户组许可、独立授权以及调用中的 `full_access:true`；使用服务账户权限 |

无有效网络授权，或调用指定 `network_mode:none` 时，受控命令不能进行 IP 联网、绑定或监听。已有网络授权不会被文件权限隐式授予；旧的互联网、局域网或指定地址范围不会转换成任意联网授权。macOS 不提供目标地址、流量和连接数限制，也不使用 Molr 网络代理。任意 Unix socket 不在网络授权范围内，仅开放 DNS 所需的系统通信。

计算授权只填写运行时长，CPU、内存、进程数和临时盘字段为零；网络授权不带 `constraints`。界面按节点提供相应选项。CPU、内存、进程、临时盘、工作区容量／文件数、网络用量和 GPU 控制均不支持；配置或授权中提交这些限制会被拒绝。现有助理任务并发限制及用户组并发限制仍生效。

## 文件路径与任务

Shell 脚本使用 `$MOLR_WORKSPACE`、`$MOLR_GLOBAL`、`$TMPDIR` 和权限列表给出的资源 `ShellPath`。文件工具继续使用 `/work`、`/global`、`/resources/...`。工作目录参数可使用 `/work` 和 `/tmp` 别名；这些别名不是 Shell 中的挂载点，脚本文本不会被改写。外部文件直接使用获批的宿主路径。临时目录由任务创建并在受管进程结束后清理。

前台、后台和 TTY 复用现有任务管理。Runtime 不向受控进程继承自己的凭据环境变量、代理变量或额外文件描述符；TTY 只放行本任务实际分配的终端设备。进程不能读取其它进程的参数／环境或向它们发送信号，但 macOS 没有 Linux 进程命名空间。

任务启动时固定文件和网络范围。授权到期、撤销或相关范围变化会触发重查并终止受管进程组；新增权限需要启动新任务。另建会话的子进程可能存活，已经打开的文件和连接也不具备对象级即时撤销保证。需要整棵进程树强制清理或资源硬配额时使用 Linux 节点。

文件策略限制直接路径访问，不隐藏全部文件元数据，不提供硬链接别名的对象隔离。允许联网后，命令可以访问本机服务；这些服务若主动暴露文件，Seatbelt 不会对服务返回的数据再次执行文件权限检查。

## 端口转发

已有 `forward_ports` 提供到宿主 `127.0.0.1:<端口>` 的认证 HTTP/WebSocket 转发。仍需部署启用并满足管理员条件；受控命令还需计算和网络授权。禁用网络的调用不能声明转发端口。

声明的端口只选择转发目标，不限制进程实际监听的接口或端口，也不证明监听服务属于该任务。有网络权限的服务可以直接监听局域网；这种直接访问不经过 Molr 转发认证。任务结束或撤权后，Molr 关闭自己管理的转发入口和连接。

## 验证与限制

启动会实际验证工作区写入、私有文件拒绝访问以及网络开关。通过后，认证访问 `GET /api/sandbox` 应看到 `Reason=available`、`Shell=true`、`FileIsolation=true`、`NetworkIsolation=true`、`NetworkBackend=seatbelt-toggle`；`ProcessIsolation` 与 `ResourceLimits` 为 false。探针失败时受控执行不可用，不回退为 Full Access。

原生回归在 Mac 的 `runtime/` 目录运行：

```bash
CGO_ENABLED=0 go test ./internal/platform/sandbox ./internal/app ./internal/config -run 'TestSeatbelt|TestNativeSandbox' -v
```

受管浏览器和隔离音视频解码暂不支持。独立 macOS 节点使用相同的 Seatbelt 能力，部署见[远程沙盒](remote.md#macos-节点)。Linux 和 macOS Runtime 均可连接两种节点，按目标节点展示能力。
