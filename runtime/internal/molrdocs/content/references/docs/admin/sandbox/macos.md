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
| 网络访问 | 单一 `preset:network` 资源；允许任意目的地址的 TCP/UDP、DNS，以及监听（浏览器保留控制端口除外） |
| Full Access | 需要部署开关、用户组许可、独立授权以及调用中的 `full_access:true`；使用服务账户权限 |

无有效网络授权，或调用指定 `network_mode:none` 时，受控命令不能进行 IP 联网、绑定或监听。已有网络授权不会被文件权限隐式授予；旧的互联网、局域网或指定地址范围不会转换成任意联网授权。macOS 不提供目标地址、流量和连接数限制，也不使用 Molr 网络代理。任意 Unix socket 不在网络授权范围内，仅开放 DNS 所需的系统通信。

计算授权只填写运行时长，CPU、内存、进程数和临时盘字段为零；网络授权不带 `constraints`。界面按节点提供相应选项。CPU、内存、进程、临时盘、工作区容量／文件数、网络用量和 GPU 控制均不支持；配置或授权中提交这些限制会被拒绝。现有助理任务并发限制及用户组并发限制仍生效。

## 文件路径与任务

Shell 脚本使用 `$MOLR_WORKSPACE`、`$MOLR_GLOBAL`、`$TMPDIR` 和权限列表给出的资源 `ShellPath`。文件工具继续使用 `/work`、`/global`、`/resources/...`。工作目录参数可使用 `/work` 和 `/tmp` 别名；这些别名不是 Shell 中的挂载点，脚本文本不会被改写。外部文件直接使用获批的宿主路径。临时目录由任务创建并在受管进程结束后清理。

前台、后台和 TTY 复用现有任务管理。Runtime 不向受控进程继承自己的凭据环境变量、代理变量或额外文件描述符；TTY 只放行本任务实际分配的终端设备。进程不能读取其它进程的参数／环境或向它们发送信号，但 macOS 没有 Linux 进程命名空间。

任务启动时固定文件和网络范围。授权到期、撤销或相关范围变化会触发重查并终止受管进程组；新增权限需要启动新任务。另建会话的子进程可能存活，已经打开的文件和连接也不具备对象级即时撤销保证。需要整棵进程树强制清理或资源硬配额时使用 Linux 节点。

文件策略限制直接路径访问，不隐藏全部文件元数据，不提供硬链接别名的对象隔离。允许联网后，命令可以访问本机服务；这些服务若主动暴露文件，Seatbelt 不会对服务返回的数据再次执行文件权限检查。

## 端口转发

已有 `forward_ports` 提供到宿主 `127.0.0.1:<端口>` 的认证 HTTP/WebSocket 转发。仍需部署启用并满足管理员条件；受控命令还需计算和网络授权。普通命令禁用网络时不能声明转发端口；`39200–39263` 是浏览器控制保留范围，受控命令不能访问、监听或通过通用转发连接这些端口。

声明的端口只选择转发目标，不限制进程实际监听的接口或端口，也不证明监听服务属于该任务。有网络权限的服务可以直接监听局域网；这种直接访问不经过 Molr 转发认证。任务结束或撤权后，Molr 关闭自己管理的转发入口和连接。

## 受管浏览器

本机浏览器支持 Apple Silicon 和 Intel macOS，在浏览器面板选择「安装并启动」即可准备组件，详见[组件管理](https://molr.ai/zh/docs/admin/administration/components/)。无需关闭 SIP、修改系统配置或手动授予屏幕录制权限。管理员自行配置程序目录时，agent 目录包含 `agent-browser`，Chromium 目录包含完整的 `Google Chrome for Testing.app`，不能只复制主可执行文件。

沙盒模式下，浏览器使用 Molr 外层 Seatbelt；Chromium 通过 `--no-sandbox` 启动，不启用内部渲染进程沙盒。macOS 不允许在有效外层 Seatbelt 中重新初始化 Chromium 的 Seatbelt，因此隔离边界是整个浏览器任务。工作区、临时目录、获批文件和网络开关仍受外层策略约束，不提供 Linux 的进程命名空间或资源硬配额。

每个任务使用独立控制端口和私有 daemon socket。普通受控命令与其他浏览器任务不能访问这些控制端口；WebUI 的命令与画面连接经过 Runtime 认证。无网络授权时内部控制仍可用，网页不能联网。Full Access 和宿主其他进程不属于此控制隔离边界。

正常关闭先请求 Chromium 刷新并退出，再回收控制进程；授权失效或子进程异常会关闭整个浏览器任务。Runtime 意外退出会触发管道看护，终止两个受管进程组。网站 Profile 按助理持久化，进程被强制终止时未写入的数据可能丢失。

## 验证与限制

启动会实际验证工作区写入、私有文件拒绝访问以及网络开关。通过后，认证访问 `GET /api/sandbox` 应看到 `Reason=available`、`Shell=true`、`FileIsolation=true`、`NetworkIsolation=true`、`NetworkBackend=seatbelt-toggle`；`ProcessIsolation` 与 `ResourceLimits` 为 false。探针失败时受控执行不可用，不回退为 Full Access。

原生回归在 Mac 的 `runtime/` 目录运行：

```bash
CGO_ENABLED=0 go test ./internal/platform/sandbox ./internal/app ./internal/config -run 'TestSeatbelt|TestNativeSandbox' -v
```

隔离音视频解码暂不支持。受管浏览器只在本机 Runtime 执行，不在远程节点启动。独立 macOS 节点使用相同的 Seatbelt 能力，部署见[远程沙盒](https://molr.ai/zh/docs/admin/sandbox/remote/#macos-节点)。Linux 和 macOS Runtime 均可连接两种节点，按目标节点展示能力。

浏览器原生回归使用事先安装的固定版本组件和当前构建的 Runtime：

```bash
MOLR_TEST_BROWSER_AGENT=/absolute/agent-directory \
MOLR_TEST_BROWSER_CHROMIUM=/absolute/chromium-directory \
MOLR_TEST_HELPER=/absolute/molr \
CGO_ENABLED=0 go test ./internal/platform/sandbox -run TestSeatbeltNativeBrowser -v
MOLR_SANDBOX_INTEGRATION=1 MOLR_TEST_AGENT_BROWSER=/absolute/agent-directory \
MOLR_TEST_CHROMIUM=/absolute/chromium-directory \
CGO_ENABLED=0 go test ./internal/app -run 'TestBrowserRealTask|TestBrowserProxyReal' -v
```

受管浏览器的 Full Access 模式不加载外层 Seatbelt，并移除 `--no-sandbox`。Runtime 通过私有 CDP 连接取得渲染进程身份，再通过系统 `sandbox_check` 检查其 Seatbelt 状态；检查失败则结束启动，不回退为无沙盒。检查通过系统自带的 JXA C 桥接运行，Runtime 仍以 `CGO_ENABLED=0` 构建。画面、接管、独立 Profile、代理及撤权清理继续使用受管任务。
