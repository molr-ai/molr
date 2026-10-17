# Linux 沙盒部署

本页面向部署者，说明 Linux 命令隔离所需依赖、资源边界和验证方式。日常操作见[文件与命令](https://molr.ai/zh/docs/user/guides/files-and-commands/)，管理员接入见[沙盒资源](https://molr.ai/zh/docs/admin/resources/sandbox/)。

## 安装和启动

`molr-setup` 默认安装当前用户服务；root 可通过 `--user USER` 安装指定账户的系统服务。整个 Runtime 和沙盒使用同一普通账户，系统依赖可由 `--install-deps` 授权准备。参数与更新流程见[安装指南](https://molr.ai/zh/docs/admin/deployment/installation/#安装时选择运行账户)。

当前自动安装路径面向具备 systemd 254+ 的 Linux；普通命令启动需要可访问的 systemd 用户管理器，安装服务则自带执行资源委派。在不支持这些条件的系统或受限容器中，聊天等功能仍运行，命令执行显示当前系统不可用，不降级为无隔离执行。现有 distroless Docker 镜像尚不包含这套自动命令环境，不应宣称容器部署默认支持 Shell。

构建使用 [runtime/.go-version](https://github.com/molr-ai/molr/blob/main/runtime/.go-version) 指定版本的 Go；在 `runtime/` 目录生成静态二进制，以便同一个 server 可在精简环境内运行内部 helper：

```bash
CGO_ENABLED=0 GOTOOLCHAIN="go$(cat .go-version)" go build -o /path/molr ./cmd/molr
chmod 0755 /path/molr
file /path/molr
go version -m /path/molr
```

检查最终二进制为静态链接、构建信息为 `CGO_ENABLED=0`、权限为 `0755`。通过具有委派的 systemd 用户或系统服务启动，设置 `Type=exec`、`Delegate=yes`、`DelegateSubgroup=supervisor`、`KillMode=control-group`，完整启动和验证步骤见 [安装与服务管理](https://molr.ai/zh/docs/admin/deployment/installation/)。安装脚本默认使用 `molr.service`，多实例验收使用 `--name molr-<名称>` 并分配独立端口、配置和数据目录。

正常配置可以完全省略 `sandbox` 对象。`configs/sandbox.example.json` 只展示可选的执行限额，不再要求填写路径。

## Runtime 自动处理什么

- 基础环境从发行包的 `libexec/busybox` 或已安装的静态 BusyBox 构造，包含常用 Shell 命令和 CA 证书。按内容版本缓存并原子发布；复制的是小型运行资产，不复制用户目录，也不在每次调用前扫描用户文件。
- 工作区自动保存在 Runtime 管理的数据目录下，每个所有者/助理使用独立目录，沙盒内显示为 `/work`。外部资源目录按授权直接挂载到稳定的 `ShellPath`，写入即时生效，失败或取消不回滚。
- 安装服务已经提供系统资源委派时，Runtime 自动发现并使用；普通命令启动则自动创建临时的 Molr systemd 用户单元，不要求用户找到或填写 cgroup 路径。专用监督进程通过连接租约存活，Runtime 退出或连接丢失时整组停止。
- 启动时执行无外部目录授权的隔离探针，以及使用临时本机端点的透明网络探针（不依赖公网）。只有探针成功才显示就绪；文件存在或配置开启不等于环境可用。
- 本机计算资源自动登记一次，保留已有名称、断开状态及授权。助理仍需获得计算 `execute`，文件与网络也分别校验。

最小环境只提供 BusyBox，不承诺完整 HTTPS 下载能力。宿主工具模式按 Runtime 服务账户的权限复用系统 curl、Python 等程序，见下文；不再提供 `molr-fetch`。

## GPU 计算权限

GPU 属于计算资源的可选能力，默认关闭。当前支持 Linux NVIDIA 整卡 CUDA 计算，要求服务账号可访问指定 GPU、宿主已安装匹配的 NVIDIA 驱动和 `/usr/bin/python3`，并使用宿主工具模式。Molr 不安装驱动，不以 root 运行，也不连接 NVIDIA 管理 socket。

部署者先用 `nvidia-smi --query-gpu=uuid,name --format=csv` 查看硬件 UUID，再显式配置允许暴露的设备：

```json
{
  "sandbox": {
    "host_tools": true,
    "gpu": {
      "enabled": true,
      "devices": ["GPU-00000000-0000-0000-0000-000000000001"]
    }
  }
}
```

示例 UUID 必须替换为实际硬件 UUID。新增或更换设备不会自动扩大已有授权。重启后，在「资源 → 沙盒 → 计算」查看 GPU 状态；启动探针验证隔离环境中的 CUDA 上下文、显存读写和执行内 IPC。GPU 不可用不影响无 GPU 授权的普通计算。

管理员按[沙盒资源管理](https://molr.ai/zh/docs/admin/resources/sandbox/)分配设备范围，用户再[批准助理 GPU 权限](https://molr.ai/zh/docs/user/guides/permissions-and-budget/#gpu-授权)。更换设备不扩大已有授权；计算授权变更会停止受影响任务。

GPU 为共享访问，不预留显存、不保证算力，不增加 GPU 任务数量配额；普通执行继续计入现有并发限制。界面显示的总显存是硬件信息。Full Access 按原有直接执行语义访问系统，不受普通 GPU 授权范围约束。

GPU 执行仍保留文件、进程和网络隔离。CUDA 所需的抽象 Unix SEQPACKET 通信限于当前执行的网络命名空间，路径 Unix socket 仅允许当前执行的私有 `/tmp`，宿主路径仍被拒绝。GPU 探针或设备准备失败时不自动退回 CPU 或 Full Access；查看设备原因码，修复后重启重新探测，或移除计算权限中的 GPU 后继续普通计算。

常见原因包括 `gpu_host_tools_required`（未启用宿主工具模式）、`gpu_driver_unavailable`（驱动或 nvidia-smi 不可用）、`gpu_device_missing`（配置的 UUID 不存在）、`gpu_probe_failed`（CUDA／IPC 隔离探针失败）、`gpu_device_changed`（硬件映射改变）和 `gpu_scope_denied`（超出部署或用户组范围）。

## 按需授予 Full Access

同一节点可以同时处理普通隔离调用和 Full Access 调用。Full Access（完全访问）是沙盒下与存储、计算、网络并列的权限类型，默认不开放；它不是节点运行模式，也不会在隔离失败时自动启用。

部署者在 `molr` 或 `molr-sandbox` 配置中开启：

```json
{ "sandbox": { "enabled": true, "allow_full_access": true } }
```

重启后，沙盒页面显示「可授权完全访问」，并提供「存储／计算／网络／完全访问」四个页签。普通成员还需管理员在用户组对应沙盒的「权限与配额」中允许授予 Full Access。在助理授权表单中依次选择「资源类型：沙盒」→「选择沙盒」→「权限类型：完全访问」即可授权；该授权没有计算、网络、存储或并发额度，表单不显示额度设置，仍可设置授权到期时间和随时撤销。助理也可以发起 Full Access 权限申请，由用户审批。

普通计算授权和 Full Access 授权可以分别保留。助理只有在文件或命令工具中显式传入 `full_access: true` 时才使用 Full Access；其他调用继续使用原有隔离和权限。Full Access 授权不替代普通计算授权，撤销 Full Access 不会停止同时运行的普通隔离任务。

Full Access 以服务进程的系统身份直接执行 `/bin/sh`，继承其环境变量，直接访问所在环境的文件、网络和进程；Molr 不提供隔离或资源限额，也不需要额外的目录、网络授权。它设计用于服务本身已运行在容器或虚拟机中的场景，访问范围由外部环境和系统账号决定。脚本使用真实路径或 `$MOLR_WORKSPACE`；文件工具仍接受 `/work` 别名，也可使用绝对路径。普通隔离调用依然使用下文的 `/work`、临时目录、出口与额度规则。

Full Access 无需 Bubblewrap、rootfs 或 cgroup 委派；若这些条件缺失，只有普通隔离能力不可用。`Capabilities.FullAccess` 独立报告直接执行能力，不表示其他隔离能力就绪。安装器对已配置 `allow_full_access: true` 的实例允许缺少隔离依赖；需要两种调用同时可用时仍须满足全部隔离条件。

## 默认限制与容量语义

| 限制 | 默认值 | 实施方式 |
| --- | --- | --- |
| 前台等待 | 最多 60 秒 | 未结束自动转后台，保留原任务 ID |
| 任务运行时长 | 不设默认截止时间 | 可选 `timeout_seconds`（1–604800 秒）；授权到期、撤销及资源限额仍会停止执行 |
| 同助理并发执行 | 10 个 | 前台、后台、TTY 合计，跨本机和远程沙盒统一计数 |
| 内存 / swap | 512 MiB / 0 | cgroup 硬限制 |
| CPU | 100%，即一个 CPU 的时间额度 | cgroup 配额 |
| 进程与线程总数 | 64 | cgroup PID 上限 |
| 临时空间 | 64 MiB | 每次执行的 tmpfs 大小限制 |
| 单个文件大小 | 1 GiB | 不可由脚本提高的 RLIMIT_FSIZE 硬上限 |
| 磁盘剩余空间 | 至少 256 MiB | 启动前检查，运行中低水位触发停止 |
| 输出 | 合计 64 KiB | 有界 stdout/stderr |

普通工作目录与外部授权目录均可使用现有文件系统，不再检查整个文件系统是否小于某个容量，也不要求与 Runtime 处于不同文件系统。

**单文件硬上限和磁盘低水位保护不是目录总容量硬配额。** 多文件写入或并发外部写入可能在监控间隔内越过低水位，Runtime 不承诺严格保留这些剩余字节。需要目录总容量硬配额的部署应另行使用文件系统配额，而不是依赖这里的保护。

Runtime 不统计每次执行的文件变更总量，也不遍历大目录；产物按显式引用经文件接口读取，工作台展示命令、输出和退出分类，不提供完整文件变更清单。

## 权限和边界

挂载源由资源 ID 与授权相对路径解析，不接受模型提交宿主路径或隔离开关。只读挂载需要 `read`，读写挂载同时需要 `read` 和 `write`；仅写权限继续使用受控文件工具。Runtime 私有目录及其上级、内核伪文件系统和未支持的嵌套挂载仍拒绝暴露，允许普通同文件系统目录不代表允许直接读取 Runtime 数据。

直接目录授权覆盖目录内实际文件对象，遵循普通硬链接语义；如果用户事先将某个文件硬链接到已授权目录，其内容也在该目录授权内。Shell 无法利用未挂载的宿主路径新建指向私有数据的链接。现有逐文件工具仍保留更严格的链接检查，不能将两种访问模型混为一谈。

原有 Bubblewrap、seccomp、去除 capabilities、最小设备集、空环境及 FD 隔离继续生效。Shell 禁止宿主 Unix socket、VSOCK、Netlink、新建 mount namespace、加入其他 namespace和相关绕过系统调用；GPU 按计算授权提供受控设备访问，其他硬件直通和宿主软件安装不提供；指定 Shell 端口可通过 [TCP 转发](https://molr.ai/zh/docs/user/guides/files-and-commands/#shell-端口转发)访问。

授权更新/撤销、资源断开与启动串行协调，返回成功前等待相关执行终止；当前保守地停止该所有者的全部活动 Shell。授权到期设置截止时间，其他执行条件和磁盘空间定期重查。取消覆盖另建 session 的子进程，未知结果不自动重跑。

## 网络与高级部署

Shell 默认没有外部网络。获得网络资源授权后，Runtime 自动选择当前助理在同一沙盒中的有效授权；`network_mode: "none"` 可关闭本次出站联网。允许联网时，Runtime 为本次执行创建独立的 user/net namespace 和 TUN 网卡，自动准备内部 IP、路由及 DNS。程序使用正常 TCP/UDP socket，无需 `HTTP_PROXY` 等环境变量。宿主默认网络命名空间的网卡、路由、防火墙、DNS 与 IP forwarding 不受修改；普通运行不需要宿主 root。首次安装系统依赖或系统缺少 TUN/user namespace 支持时，需要安装流程或管理员解决，运行中的 Runtime 不调用 sudo 或放宽全局安全策略。

网络命名空间由宿主内核管理，宿主管理员仍能检查它；“隔离”不表示对宿主不可见。Runtime 的网络辅助进程使用宿主现有网络创建普通出站连接。可信初始化过程仅在执行专属的用户/网络命名空间内拥有管理权限，最终脚本仍没有 capabilities，且保留 seccomp。允许创建嵌套 user／PID／network namespace 进一步收窄隔离，仍禁止新建 mount namespace 或加入其他 namespace。

### 内置可选预设

新配置默认提供以下候选项，用户在 WebUI「资源 → 沙盒 → 网络」中添加，再授权助理使用；出现候选项、添加资源均不代表已经授权。

| 预设 ID | 界面名称 | 范围 |
| --- | --- | --- |
| `internet` | 互联网访问 | `0.0.0.0/0`、`::/0`，排除下述私网、回环和内部 IPv4 映射网段；TCP/UDP 的 `1–65535` 端口 |
| `lan` | 局域网访问 | `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`、`fc00::/7`；TCP/UDP 的 `1–65535` 端口，不含回环 |
| `host` | 宿主机访问 | `127.0.0.0/8`、`::1/128`；TCP/UDP 的 `1–65535` 端口，包括 Molr 自身服务 |

互联网访问的完整排除列表是 `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`、`fc00::/7`、`127.0.0.0/8`、`::1/128`、`198.18.0.0/15`。共享地址 `100.64.0.0/10` 不属于此处的 RFC1918/ULA 局域网分类，归入互联网访问。局域网访问可以连接宿主机开放在其局域网地址上的服务；宿主机访问仅对应回环地址。

沙盒的网络网关工作在第四层（传输层），只支持 TCP/UDP 协议。不在此范围内的协议（如 ICMP）不会被转发。三项预设均开放 TCP/UDP 的全部有效端口。所有预设仍禁止未指定、组播、链路本地、带 zone 的地址及元数据地址 `100.100.100.200`、`168.63.129.16`、`fd00:ec2::254`。`198.18.0.0/15` 与 `fd00:6d6f:6c72:ffff::/64` 用于执行内 DNS 虚拟映射，不是可直接访问的真实目标网段。流量和并发连接按助理授权执行，并受部署能力边界限制。

`localhost` 永远属于沙盒自身。访问沙盒所在机器的回环服务使用 `host.molr.internal`（127.0.0.1）或 `host6.molr.internal`（::1），仍需对应地址、协议和端口授权。远程沙盒的宿主服务指远程机器。

部署者可以限制候选预设，也可以提供显式的自定义规则。普通用户无需编辑内部网段、路由、网卡或 DNS：

```json
{
  "sandbox": {
    "network_presets": ["internet", "lan", "host"],
    "network_bytes": 67108864,
    "network_connections": 16,
    "networks": [
      {
        "name": "内部接口",
        "protocols": ["tcp"],
        "addresses": ["api.example.com", "192.168.1.0/24"],
        "ports": ["443", "8000-8999"],
        "exclude_addresses": ["192.168.1.10"],
        "exclude_ports": ["8080-8089"]
      }
    ]
  }
}
```

`network_presets: []` 移除全部内置候选项；同时设置 `networks: []` 才完全不提供网络资源。预设配置缺省时使用上述三项。`preset:` 前缀由内置资源使用，不要创建同名自定义规则。修改部署范围后重启，WebUI 可连接与授权候选资源，但不能扩大部署边界。

每条自定义规则必须显式声明 `protocols`，只接受不重复的 `tcp` / `udp`。缺少协议或配置错误时拒绝加载，不自动替换成宽泛预设。

地址支持精确域名、IP 和标准 CIDR；端口支持单端口与闭区间，均为 1–65535。允许集合各支持 1–32 项，排除集合各支持 0–32 项。单个连接目标的协议、地址和端口必须命中同一条规则，多条已授权规则取并集；域名解析返回多个地址时，每个地址都需命中至少一条规则（可由不同规则分别覆盖），任一地址未命中即拒绝整个连接。排除优先于所属规则的允许。注意：**排除集合中的主机名条目只在域名解析路径生效**——直接以 IP 连接时不匹配主机名排除，仅按 IP/CIDR 条目检查；需要封禁某个域名的全部入口时，应同时排除其地址段。

### DNS、限额与撤销

DNS 服务仅在执行内部提供，支持 UDP/TCP 的 A/AAAA 查询，调用系统解析器处理真实域名及 CNAME。沙盒收到的是执行专属的虚拟 IP；每次建立连接重新解析域名、检查全部真实地址，再直拨已检查 IP，避免共享 IP 或 DNS 重绑定绕过授权。域名被排除或未授权时，不进行不必要的外部查询。IP/CIDR 规则需要解析后才能判断目标范围。记录 TTL 为 30 秒，虚拟地址只在当前执行内有效，不应保存到工作区供后续执行复用。

授权的是目标主机/地址、端口和传输协议，不检查 TLS 内的 URL 路径、HTTP 方法或请求内容。获得公网 TCP/UDP 权限的程序可以运行其他获准协议；也不提供原始 IP 或组播。Shell 的 HTTP/WS 端口转发独立于出站网络授权，见[转发监听地址](#转发监听地址)。

出站连接／UDP 会话和双向转发载荷按助理获批的独立额度执行（包括 TLS 与 DNS 数据，不是线速 IP 包计费）；不再保留无额度授权。DNS 使用独立的 8 个并发处理槽和最多 1024 个域名/地址族映射，避免占用程序的连接额度。UDP 空闲状态 60 秒回收。网络辅助进程单独受 256 MiB 内存、64 个进程/线程和一个 CPU 的 cgroup 上限保护；失败即终止对应执行，不回退到宿主直接执行。

每条新连接和 DNS 请求回查授权，Runtime 还持续检查授权有效期。撤销、断开、取消或到期会结束执行并回收辅助进程、TUN 与 cgroup，已有 TCP/UDP 连接也被关闭。工具结果明确报告目标被拒绝、DNS/连接失败、流量耗尽、并发限制或网关故障，失败命令仍可能已经产生副作用。

高级部署可显式设置 `sandbox.restrict_directories: true`，此时 `directories` 定义允许接入的根目录范围，包含其子目录；默认 false 时，现有存储资源与助理授权决定目录范围。`directories` 中显式设为只读的条目仍限制写入。网络目标范围、路径覆盖和宿主保护边界仍由部署配置决定。助理的计算、网络及工作区额度由用户在该边界内批准，WebUI 不能扩大部署边界。

## 只读复用宿主工具

无需 Debian 工具镜像，不复制 `/usr` 或个人软件环境。宿主工具是否共享与服务运行账户分开配置：

```json
{
  "sandbox": {
    "enabled": true,
    "host_tools": true
  }
}
```

沙盒继承 Runtime 的宿主身份。安装器默认使用当前用户；通过 `sudo … --user USER` 选择账户后，整个 Runtime 的文件操作、后台任务和沙盒都使用该用户。已有账户保留其组权限；新建账户没有附加组。安装参数及自动创建账户流程见 [安装时选择运行账户](https://molr.ai/zh/docs/admin/deployment/installation/#安装时选择运行账户)。本机执行不需要单独的 `molr-sandbox` 服务。

`host_tools: false` 或省略时，直接启动的 Runtime 保留最小 BusyBox 环境；安装器在首次安装且没有自定义 rootfs 时默认打开宿主工具。服务账户由部署决定，不能通过沙盒配置切换。

宿主工具模式拒绝 UID 0 或不匹配的实际／有效身份，保持只读系统挂载、文件范围、网络授权、seccomp、no_new_privs 和资源限额。已挂载范围中当前账户有权限的私有文件仍然可读，0600 不能隔离同 UID 的 Runtime 和沙盒；数据库、密钥等必须继续排除在沙盒挂载外。个人 HOME、完整 `/etc`、管理 socket 和 Runtime 私有数据不自动共享。私有数据目录或总工作区与共享系统目录重叠时拒绝启动。

共享范围为 `/usr`、宿主实际的 `/bin`、`/sbin`、`/lib*`，以及必要的 alternatives、字体、ImageMagick、OpenSSL 和动态加载器配置；全部只读，保留宿主的实际 DAC/ACL 权限。目录中公开可读的数据也会可见，不能把“只读”理解为保密。宿主 HOME、`/opt`、`/run`、完整 `/etc` 和 Runtime 私有目录不共享；Conda、用户 PATH、Snap 等位置的软件不承诺可用。挂载源经过固定描述符和嵌套挂载检查，不能通过修改用户 PATH 注入来源。

`/work` 与授权的外部资源是另外的文件范围。资源授权不会绕过所选执行账户的操作系统权限；需要访问私人目录时，由部署者单独安排目录权限，不自动 chmod、chown 或扫描整棵目录。程序文件保持原位，宿主软件升级会影响新执行及长期会话，因此它不是固定版本快照。前台／后台／TTY 使用相同的 Shell 选择策略：在完成隔离的环境内依次验证 `/bin/sh`、`/bin/dash`、`/bin/bash` 是否能运行且遵循 PATH；优先使用首个通过验证的系统 Shell，最后使用受管静态 BusyBox。Bash 以 `sh` 身份运行。TTY 由内部 helper 初始化有效的前台进程组，不依赖选择特定 Shell 绕过终端问题。seccomp、no_new_privs、capabilities、网络授权及资源限额保持有效。

默认工具查找顺序为 `/usr/bin:/bin:/usr/sbin:/sbin:/molr/bin`。最后的 `/molr/bin` 是只读的 BusyBox applet 链接目录，包含当前打包 BusyBox 实际提供的工具；系统缺少的命令可以由它补齐。正常 Shell 内建命令仍按 Shell 自身语义处理。系统命令运行失败时保留原退出码，不会用 BusyBox 重试。用户在 `.profile` 中主动修改 PATH 或定义别名时，按其设置执行。

如果宿主 `/bin/sh` 是会抢占命令的 BusyBox，Runtime 会先尝试兼容的 dash/Bash。若都不可用，仍按部署兼容约定使用 BusyBox，启动时在 stderr（TTY 中为终端输出）提示系统工具优先级无法保证；此时内置 applet 可能优先于 PATH，可用 `/usr/bin/wget` 等绝对路径明确选择宿主工具。显式启动其他 Shell、BusyBox 或执行它们解释的脚本时，命令查找由对应 Shell 决定，不承诺改变它们的编译行为。

宿主和 BusyBox 都未提供的工具仍不可用；工具已存在也需检查依赖和隔离兼容性。比如 Python 存在不代表 venv/ensurepip 已安装，在线 pip 需要网络授权，安装位置应在工作区；可使用虚拟环境，或通过 `pip3 install --target /work/packages ...` 配合 `PYTHONPATH=/work/packages` 使用独立目录；Tesseract 存在不代表 chi_sim 已安装。fd/rg 可显式限制线程数以避免多核宿主下超出执行限额。dig 使用受控 DNS，返回的虚拟地址不等于公网 DNS 原始记录；traceroute 不保证能反映真实外部路由。

使用独立远程 Sandbox 服务时，逐项功能检查可以通过已验证的服务连接执行，检查程序本身在宿主运行，但所有待测工具都通过认证 API 在真实沙盒执行：

```bash
python3 runtime/scripts/check-host-tools.py host-sandbox-connection.json tool-results.json
```

结果逐项记录退出状态、标准输出和错误，区分未安装与功能失败；不会自动安装缺失的软件。扫描 PDF/OCR 等功能只有在相应工具和语言数据存在时才可能通过。JSON 报告使用独立测试工作区，不会读取真实用户文件。

## 授权额度与部署边界

`sandbox.authorization_ceilings` 限定可批准的计算与网络上限；它与沙盒基础保护配置分开；计算和网络授权必须显式指定独立额度。`CPUPercent`、`MemoryBytes` 和 `Processes` 省略或为 `0` 时不额外设置部署上限，计算页按机器容量展示；显式配置正值时取配置值与机器容量中的较小值。只配置某一项不会为其他两项注入默认上限。当前嵌套字段采用能力 DTO 的名称，例如：

```json
{
  "sandbox": {
    "authorization_ceilings": {
      "CPUPercent": 800,
      "MemoryBytes": 17179869184,
      "Processes": 512,
      "TempBytes": 1073741824,
      "MaxFileBytes": 1099511627776,
      "NetworkBytes": 1099511627776,
      "NetworkConnections": 128
    },
    "workspace_project_quotas": false
  }
}
```

这些是可申请边界，不是每个助理已获授权的额度，也不预留物理容量。网关当前每个执行最多处理 128 个出站连接／UDP 会话，授权的共享连接数不能超过该基础设施保护上限。DNS 保持独立处理槽位，请求和响应计入解析时选中的有效网络授权；查询前先取得流量额度。端口转发仍与出站网络授权独立，不计入此处的出站网络额度。

网络账本保存在实际执行端的 SQLite 中：本机使用 `runtime.db`，独立 Sandbox 使用 `sandbox.db`。数据库快照包含历史消耗及未结算预留；远程执行端数据库和工作区须独立备份。异常中断的未结算流量仍占额度，不因跨周期或重启自动返还。

工作区硬配额为可选部署能力：文件系统必须已开启项目配额，每个助理工作目录必须配置独立、非零、可继承的 project ID，所有已有文件须归属于该项目；Runtime 运行身份须获准调用 `quotactl_fd(Q_SETQUOTA)`。完成这些前置配置后才设置 `workspace_project_quotas: true`。Runtime 不自动重新挂载文件系统、不更改已有文件的项目归属、不申请宿主提权。普通用户服务通常没有该管理权限；不能只打开配置开关就视为硬配额可用。

容量按文件系统分配空间计量，额度以 1 KiB 向下取整；文件数包含目录和其他 inode。到期降额只降低后续增长的上限，不删除既有数据。文件系统不支持、目录未分配项目或管理调用失败时返回 `workspace_quota_unavailable`，不会退回扫描监控并将其当成硬配额。

## 检查与验证

`GET /api/sandbox` 只供所有者查询状态和资源范围，不提供命令执行或配置写入。界面只展示就绪、组件缺失、系统不支持或空间不足等可理解状态，详细原因码留给诊断。

启动和每次重启后，通过已认证请求检查 `/api/sandbox`；配有 WebUI 时也要通过其同源代理验证。未明确关闭沙盒时，要求 `Enabled=true`、`Capabilities.Reason=available`，以及 `Shell`、`ProcessIsolation`、`FileIsolation`、`NetworkIsolation`、`ResourceLimits` 全部为 `true`。

| 原因 | 优先检查 |
| --- | --- |
| `sandbox_package_incomplete` | Runtime/helper 是否为静态构建，静态 BusyBox 是否可用 |
| `sandbox_helper_unavailable` | Bubblewrap/helper 是否为可执行普通文件，权限与路径是否符合要求 |
| `sandbox_network_probe_failed` / `sandbox_network_setup_failed` | TUN、nsenter、用户命名空间与 AppArmor 支持；保留原有限制，勿关闭网络隔离 |
| cgroup 或隔离启动失败 | 当前服务的委派、unit 配置与日志；实际隔离探针是否通过 |

不要通过关闭隔离、增加可写权限或改成宿主执行绕过问题。用户只需授予明确资源权限；系统依赖和部署策略由维护者处理。

真实隔离测试和实现边界见 [沙盒开发文档](https://molr.ai/zh/docs/dev/security/sandbox/)。备份时包含 Runtime 管理的 `sandbox/work`；数据库快照不包含这些文件。

独立远程沙盒的部署、连接与恢复语义见 [独立 Sandbox 服务](https://molr.ai/zh/docs/admin/sandbox/remote/)。`sandbox.enabled: false` 关闭内置本机沙盒，不影响已连接远程沙盒。

## 转发监听地址

正常单机局域网部署无需填写端口。默认只在任务明确请求时，使用 `0.0.0.0` 和操作系统分配的空闲端口；优先选择活动网卡的私有 IPv4 地址作为访问地址。多网卡、容器或公网部署应显式指定可达地址：

```json
{
  "port_forward": {
    "enabled": true,
    "listen_host": "0.0.0.0",
    "public_host": "<LAN_IP>"
  }
}
```

将 `<LAN_IP>` 替换为部署主机的实际可达局域网 IP，不能原样写入配置。

`listen_host` 是 Runtime 绑定的 IP，不带端口；`public_host` 是用户可访问的主机名或 IP，不带协议、路径或端口。只供本机使用可将两者设为 `127.0.0.1`。`enabled: false` 禁止创建带转发端口的任务，不影响普通 Shell。此设置由 Runtime 部署管理，不由模型修改，也不因为内置沙盒关闭而停止对远程任务提供转发。

`public_host` 只控制返回地址，不配置路由、防火墙或 NAT。部署者需保证用户能够连接 Runtime 自动分配的 TCP 端口；仅代理 Molr HTTP 页面不会自动代理这些端口。地址无法自动确定或端口分配失败时，拒绝启动本次任务。


公开 HTTP/WS 转发校验当前 Molr 登录会话与任务归属。转发地址必须与 WebUI 登录使用相同的主机名/IP，本机部署同样如此。任务链接通过端口标记登录入口，该标记不是凭据。退出或撤销所选会话会关闭已建立的连接。Runtime 转发前移除 Molr Cookie、Authorization 和 CSRF 头，并阻止上游响应设置 Molr 保留 Cookie。公开原始 TCP 模式已禁用；底层 namespace／远程传输仍使用 TCP。

local/lan HTTP 使用 HTTP 转发。HTTPS 使用 `port_forward.cert_file` 与 `port_forward.key_file`，直接 TLS 模式也可继承 Runtime 证书。可信代理部署必须额外配置转发证书；主 WebUI 的代理不会覆盖动态转发端口。证书需覆盖 `public_host`，不能通过降级 HTTP 传递凭据。

## 本地 IPC 与浏览器程序

所有 Shell 使用同一套 IPC 策略，支持 Unix STREAM／SEQPACKET socket 及匿名 socketpair，仍禁止 DGRAM。抽象地址由本次执行的网络命名空间隔离；路径 socket 必须使用私有 `/tmp` 下的绝对路径，不能经过符号链接或跨挂载。`/work` 及已授权资源目录中的 socket 仍不可访问。临时 socket 不跨执行保留，连续浏览器命令应放在同一前台脚本或受管后台 Shell 中运行。

启用宿主工具并具备 Chromium 系统库后，agent-browser 可在助理沙盒内运行。CLI 放在工作区或已授权程序目录，Chromium 程序目录授予只读权限。在该 Shell 中配置以下变量（可执行文件路径替换为实际获准路径）：

```sh
export AGENT_BROWSER_SOCKET_DIR=/tmp/agent-browser
export AGENT_BROWSER_EXECUTABLE_PATH=/resources/chromium/chrome
export AGENT_BROWSER_ARGS=--disable-dev-shm-usage
export TOKIO_WORKER_THREADS=2 RAYON_NUM_THREADS=2
mkdir -p "$AGENT_BROWSER_SOCKET_DIR"
```

优先保留 Chromium 原生沙盒。Molr 允许嵌套 user／PID／network namespace，继续禁止 mount namespace、挂载 API、setns、宿主 IPC 和特权进程检查调用。子命名空间中的 capabilities 不赋予父沙盒或宿主权限；联网仍经过授权网关，所有后代仍受执行 cgroup 约束。联网与断网 Shell 均保持服务账号的数字 UID／GID，只调整命名空间映射，不改变实际宿主身份或文件权限。IPC 代理也遵守调用者的内层 chroot，不能借助外层 `/tmp` 连回内层之外。

运行 `agent-browser open chrome://sandbox` 和 `agent-browser get text body` 验证 Namespace、PID／network namespace、Seccomp-BPF、TSYNC 已启用。不能仅凭页面能打开判断：agent-browser 检测到 root 时可能自动加入 `--no-sandbox`。宿主工具模式要求非 root 服务账号。

默认使用 `/usr/bin/bwrap` 时，Runtime 优先通过 `/usr/bin/aa-exec -p chrome` 复用宿主已加载的 `chrome` AppArmor profile。启动时先验证真实的断网执行与受控联网；工具缺失、profile 未加载或探针失败时，使用 Bubblewrap 原有 profile 并重新验证。两条路径都不可用时关闭 Shell 能力。选择仅发生在启动阶段，不会在业务命令失败后切换 profile 重跑，也不会自动给浏览器添加 `--no-sandbox`。Runtime 不安装、加载或修改 AppArmor 策略，不修改 sysctl。显式设置 `sandbox.bubblewrap` 为其他可信启动器时保留管理员的选择。

发行版的 `chrome` profile 通常是允许 userns 的应用级豁免；它自身不提供 Chromium 内层沙盒。相较 Bubblewrap 受限子 profile，它不再额外一律拒绝 capabilities，允许 Chromium 在自己的子命名空间中建立隔离，也增加了可访问的内核功能面。Molr 的文件挂载边界、受控网络、seccomp、零宿主 capabilities、`no_new_privs` 与 cgroup 仍然生效。可用性以实际探针和 `chrome://sandbox` 为准，不能只检查 `/etc/apparmor.d/chrome` 文件是否存在。

宿主无法支持嵌套沙盒时，仍可显式使用 `AGENT_BROWSER_ARGS=--no-sandbox,--disable-dev-shm-usage` 兼容。它关闭 Chromium 内层防护，浏览器各进程共享本次 Shell 的授权范围，由 Molr 外层隔离约束，不适用于宿主直接执行或 Full Access。已验证的计算额度起点为内存 2 GiB、512 个进程／线程、200% CPU、512 MiB 临时空间。浏览器任务已提供实时画面与用户接管；程序安装仍由部署者完成。

## 受管浏览器任务

通过 [组件安装](https://molr.ai/zh/docs/admin/administration/components/#命令行维护) 自动准备浏览器程序，并在宿主工具模式下提供 Chromium 系统库。也可以在 Runtime 宿主手动安装 agent-browser 0.39.0 与兼容的 Chromium 发行包。可执行文件必须是普通文件、具有执行权限、不能是符号链接，也不能允许组或其他用户写入。在 Runtime 私有数据目录以外配置只读程序目录：

```json
{"browser":{"agent_directory":"/opt/molr-browser/agent","chromium_directory":"/opt/molr-browser/chromium"}}
```

两个目录分别包含 `agent-browser` 和 `chrome`，Chromium 配套文件须完整保留。默认路径为 Runtime 二进制旁的 `libexec/browser/agent` 与 `libexec/browser/chromium`。程序只读挂载，这些可信资产不要求助理额外申请目录权限。未指定外部程序目录时，Runtime 优先读取 molr-setup 安装的组件记录。安装目录受到保护，不能通过普通目录资源授权给助理；只有受管浏览器启动器能挂载所需程序。

浏览器复用现有本机宿主工具沙盒及计算、网络授权。每个助理最多一个活动浏览器任务，可打开多个标签页。Profile 保存在 `/work/.local/state/molr/browser/profile`，不同助理隔离。内部指令和画面端口仅通过 Runtime 受管执行能力访问，不创建公开转发。受管启动器不向 API 客户端开放程序路径、启动参数、CDP 或 daemon socket，不添加 `--no-sandbox`。

在聊天的浏览器面板启动、查看或接管。正常关闭用于刷新持久数据；强制清理任务可能丢失尚未落盘的浏览器状态。撤权、时限和资源额度仍结束整个执行。不支持远程浏览器任务或 Full Access。使用说明见[浏览器](https://molr.ai/zh/docs/user/best-practices/browser/)。
