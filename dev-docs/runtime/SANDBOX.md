# Agent Shell 沙盒实现与边界

更新：2026-10-01。状态：Linux Shell 已支持前台、后台和 TTY 会话。本文保留架构与跨平台演进设计；当前配置、限制和部署步骤以 [Linux Sandbox 使用说明](../../docs/deployment/sandbox.md) 为准。Windows/macOS 尚未实现。

## Full Access 权限与双路径执行

以下隔离机制适用于普通调用。`sandbox.allow_full_access` 默认 false；开启后，同一 Runner 可并发处理普通隔离调用和显式 Full Access 调用。节点没有全局执行模式。Full Access 必须同时满足部署允许、用户组允许（管理员默认允许）、有效的 `Scope.FullAccess` 计算授权和本次 `full_access: true`。

`direct_linux.go` 在服务环境中直接执行 `/bin/sh`，继承环境并设置 `MOLR_WORKSPACE`，支持后台、TTY、输入、取消和端口转发；不创建 namespace、seccomp、cgroup 或资源限额，不承诺文件、网络和进程隔离。文件工具使用绝对路径或工作区别名。Full Access 不依赖普通计算、文件、网络授权，不计入普通任务十个并发上限。调用者显式设置的超时、授权期限、输出分页和 API 请求大小边界仍保留；这些是调用生命周期和协议边界，不是资源配额。

普通调用永不自动升级；两类计算授权独立匹配。直接执行定期重查授权，撤销、过期、助理停用和远程租约失效会取消任务及其进程组；不承诺对脱离进程组的恶意进程提供强制隔离。Full Access 撤销只停止对应路径，普通任务继续运行。`Capabilities.FullAccess` 单独探测，即便隔离准备失败仍可为 true；五项隔离能力与 Reason 继续如实反映普通路径。

Full Access 设计用于外部容器／虚拟机已提供所需隔离的环境。进程继承外部环境现有的系统限制；Molr 不移除部署环境本身的限制。

## 1. 结论与一期范围

采用 **Bubblewrap + seccomp + cgroup v2 + 受控网络出口**，首期只支持 Linux。Go Runtime 继续是一个部署单元，负责授权、执行生命周期、透明网络出口与审计；Bubblewrap 和受信任的辅助进程由 Runtime 管理，不新增业务微服务，不引入插件系统。

Bubblewrap 负责构造隔离环境，并不提供完整安全策略；策略由 Molr 生成并验证。它与宿主共享内核，本方案不承诺达到虚拟机的隔离强度。[Bubblewrap 官方说明](https://github.com/containers/bubblewrap)

Shell 支持单次前台命令、显式或 60 秒后自动分离的后台任务，以及持久到退出的 TTY 会话。同一助理合计最多 10 个执行；每个执行建立独立沙盒，只有 `/work` 跨执行持久化。TTY 的系统 Shell 优先与 BusyBox 兜底策略和普通命令一致，默认交互会话加载 `/work/.profile`；非交互命令不加载。任务默认无墙钟截止时间，可选实际超时；资源、授权和隔离限制始终生效。支持显式任务 TCP 端口转发；暂不提供真实设备直通或跨平台执行。Python、Node 等解释器仍由部署环境提供，不开放宿主软件安装能力。

外部目录和网络默认拒绝；当前助理自己的 `/work` 由 Runtime 管理，文件工具默认可用；真实硬件默认不可见。操作系统或部署环境不能落实要求时，Shell 显示不可用，其他 Runtime 功能照常运行；绝不降级为宿主直接执行。

## 2. 当前实现与通用 Runner 的分工

Linux 后端已位于 `runtime/internal/platform/sandbox`，复用 `platform.Runner` / `Process` 并扩展可信 `Command` 的挂载、网络和限额信息。`runner.go` 管理授权与生命周期，`backend_linux.go`、`seccomp_linux.go` 构造隔离，`prepare_linux.go` 管理基础资产与委派，`network_*.go` / `helper_linux.go` 管理受控出口；其他平台由 `backend_other.go` 明确拒绝。

原通用 Runner 继续用于可信白名单程序，不具备同等隔离保证。普通 Agent Shell 只调用强制隔离入口；显式 Full Access 经独立权限检查后使用直接执行路径。资源装配会登记本机计算环境，但不自动授予助理执行权限。

文件工具的 `openat2` 检查不会自动约束任意程序；文件工具与直接挂载的差异见 [工具合同](FILE_TOOLS.md)。模型脚本交给沙盒内固定 Shell，宿主启动参数、资源路径与执行身份由可信代码构造。

## 3. 授权与执行流程

有效权限为：**部署静态上限 ∩ 助理当前授权 ∩ 本次请求资源范围**。

部署者在静态配置中决定运行环境、可暴露的资源和限额上限；WebUI 只在已登记资源内分配助理权限。普通调用只能提交资源 ID 和资源内相对路径，不能提交宿主路径、挂载参数或运行身份。`full_access` 是经授权的调用选项，不是可以自行打开的部署开关。

```mermaid
flowchart LR
    A[Agent Loop] --> B[内置 shell_execute]
    B --> C[权限与资源检查]
    C --> D[可信执行计划]
    D --> E[平台执行后端]
    E --> F[隔离中的 Shell 及子进程]
    F --> G[获准文件视图]
    F --> H[受控 TCP/UDP 出口]
    E --> I[限额、取消、回执与审计]
```

Shell 执行需要计算资源的 `execute`；只读挂载需要存储 `read`，读写挂载需要同时具有 `read` 与 `write`。仅有 `write` 时不挂载该资源，仍通过受控文件工具写入，不能隐式增加读取权限。网络资源声明其支持的连接动作。授权某个模型不会顺带授权 Shell 访问该 Provider 或取得 API Key。首期设备能力为空，不能保存看似生效的硬件授权。

计算资源包含部署者定义的执行环境与限额，助理授权关联该资源。一期不引入第二套货币预算；CPU、内存和磁盘额度属于计算容量限制，与已有助理模型费用预算区分。

## 4. Linux 执行边界

### 文件系统与运行环境

- 默认使用版本化的最小只读 rootfs。`host_tools: true` 只读共享宿主系统工具，执行身份始终继承整个 Runtime 的当前非 root 账户；安装器通过用户服务或系统服务的 `User=` 决定身份，不依赖特定用户名、无登录 shell 或独立资源服务。已有用户的附加组照常保留；共享范围内该账户可读的私有文件仍然可读。旧 `host_tools_user` 的 current/molr-sandbox 值只作为开启共享工具的兼容别名，不切换账户。私有数据和总工作区不能位于共享系统目录下；具体目录与配置范围见部署指南。正式方案不使用 `--ro-bind / /`，也不把宿主完整 `/etc`、HOME、`/run` 或 Runtime 数据目录整体映射进去。
- 使用独立 mount、user、PID、IPC、network、UTS 等命名空间；最小 `/dev` 只提供必要伪设备，`/proc` 限于沙盒进程。工作盘使用 `nodev` 等挂载限制，不暴露宿主 `/sys`、cgroup 控制文件或管理 socket。
- 所有者、助理和执行 ID 由服务端确定。助理工作区与资源挂载只呈现逻辑路径，当前工作区为 `/work`，外部目录由 `permission_list` 返回稳定 `ShellPath`；不得让模型推断宿主路径。
- 清理继承的环境、文件描述符与 socket；仅注入固定运行所需变量及 CPU 配额对应的 Go 并行度。Provider 密钥、登录令牌和 Runtime 私有数据库永不传入。
- Bubblewrap 路径及 rootfs 必须由部署配置固定并验证，不能从助理可写 PATH 查找；助理不能修改沙盒程序、策略文件或执行计划。

**一期默认直接挂载助理工作目录与获准资源子目录，不复制整棵目录、不在执行结束后扫描差异或统一回写。** 只读授权配置只读挂载，读写授权配置读写挂载；实际文件在程序访问时由文件系统读取。启动工作主要取决于挂载数量、路径验证与策略准备，不应随目录总字节数或文件总数进行全量复制/遍历。需要访问少量产物时只读取显式指定的文件。

直接挂载沿用普通文件系统语义：修改、创建、重命名和删除立即作用于原目录，失败或取消不会回滚；用户同时编辑时也不提供自动冲突检测。授权界面应说明读写包含这些操作。需要“只创建”“只追加”“仅写不读”或逐文件审核的场景继续使用受控文件工具，不映射成普通读写挂载。快照、变更预览和回滚可后续单独设计，不是一期 Shell 的前置条件。

挂载源由可信代码从资源 ID 与获准路径解析，防止符号链接及路径替换竞态；只呈现实际授权子树。隔离挂载传播，对未批准的嵌套挂载采取排除或拒绝策略，并验证只读限制不会被子挂载或另一个可写别名绕过。宿主根目录、Runtime 私有目录及它们的上级目录不能作为资源挂载。安全边界由挂载结构、系统调用限制与资源配置落实，不能以运行前扫描文件名代替。

现有文件工具“禁止所有链接与特殊文件”的规则不自动等同于 Shell 挂载语义。符号链接按沙盒文件视图解析，不能因此取得未挂载的宿主路径；硬链接则可能共享同一 inode，Bubblewrap 不会辨认其另一个宿主名称。直接目录授权包含该目录内已有文件对象，不能承诺硬链接别名不会影响其他名称。普通目录无需独立存储卷；用户预先放入授权目录的文件对象属于该目录授权范围。Runtime 私有目录及其上级路径仍禁止挂载，未暴露的宿主路径不能被 Shell 用来创建新链接。需要更严格逐文件限制时使用受控文件工具。不能通过先全量扫描一次来宣称持续解决此问题。

设备、宿主管理 socket 与其他跨边界 IPC 仍通过最小 `/dev`、`nodev`、socket/系统调用限制等阻断；文件可见不代表可以连接其中的 socket。产物导入聊天或资源库时仍经可信文件模块验证授权、普通文件类型、路径和容量，不因产物位于已授权目录就跳过检查。

### 进程、系统调用与限额

- 允许 curl 线程通知所需的匿名 Unix stream socketpair；禁止普通 Unix socket 与可向路径发送消息的 Unix datagram socketpair。
- 去除 Linux capabilities，保持 `no_new_privs`，禁止沙盒继续创建或加入能改变边界的 namespace。使用架构匹配、版本化 seccomp 配置限制 mount、ptrace、BPF、关键 ioctl 和绕过网络限制的系统调用；不能简单禁止所有 clone 而破坏普通子进程与线程。
- TTY 使用专属 PTY 与新 session，不继承宿主控制终端。非 TTY 仍无 stdin；stdout/stderr 有总量上限。Shell 运行所有下载程序、脚本和解释器时都继承相同边界。
- PTY 的 session 在 Bubblewrap 进入 PID namespace 前创建；内部 helper 的 `supervise` 在启动 TTY 子进程时设置 `SysProcAttr.Foreground`，在当前 PID namespace 内创建子进程组并设置终端前台，再执行 Shell。该操作使用父进程的 stdin FD，发生在恢复子进程信号掩码之前，避免 SIGTTOU 阻塞。初始化失败按现有 helper 启动失败路径返回 125，不继续执行脚本。非 TTY 路径不设置此属性。
- 每次执行在独立 cgroup 中，设置 CPU、内存、swap、PID 数等限制；在不可信代码开始之前完成限额配置及进程归属，不能先运行后补限制。输出、墙钟时长、并发和网络传输量另有限制。
- 取消、超时、助理停用和撤权均终止整个执行 cgroup，并等待其为空，再关闭输出和保存最终状态。进程组与父进程死亡信号只能作为附加手段。Runtime 重启先清理本实例遗留执行，不能只按过期 PID 杀进程。

Bubblewrap 可加载 seccomp BPF，并支持阻止沙盒再次创建 user namespace；cgroup 管理则需要由 Molr 与部署环境补齐。[Bubblewrap 参数](https://github.com/containers/bubblewrap/blob/main/bwrap.xml) · [cgroup v2 委派与控制器](https://docs.kernel.org/admin-guide/cgroup-v2.html)

**容量保护不要求用户准备独立卷。** 持久工作目录与授权目录直接使用现有文件系统；Runtime 设置单文件大小硬上限，并在启动前及运行中检查磁盘低水位，空间不足时终止执行。临时目录使用限容 tmpfs。这不是目录总容量硬配额，接口明确报告保护类型，不把定期监控冒充硬限制；额外硬配额属于高级部署选项。

### Shell 与工具选择

宿主工具模式下，静态 BusyBox 继续作为受管只读资产挂载到 `/molr/busybox`；准备 rootfs 时从已复制且验证过的静态二进制读取 `--list`，生成全部实际 applet 的 `/molr/bin` 链接，PATH 顺序为 `/usr/bin:/bin:/usr/sbin:/sbin:/molr/bin`。版本化 rootfs 同时包含只读 Shell 探针；启动不依赖助理可写 PATH，资产缺失不静默伪装成正常工具环境。最小环境继续使用原 `/bin/sh` 与 BusyBox 工具集。

内部 helper 在 namespace、挂载、seccomp、cgroup 限制完成后，使用固定无副作用脚本依次验证 `/bin/sh`、`/bin/dash`、`/bin/bash`、受管 BusyBox。探针检查外部命令查找与实际执行，识别 BusyBox standalone 模式绕过 PATH 的情况；不加载工作区 profile，不运行用户脚本。若系统 Shell 不兼容则尝试下一项，最后以独立启动探针确认 BusyBox 能工作；只能使用不满足优先级探针的 BusyBox 时继续提供 Shell，并向 stderr 输出明确限制。最终用户脚本只执行一次，非零退出码不触发换工具或换 Shell 重试。每次执行重新探测，以适应宿主程序更新；既有会话不动态更换 Shell。

该优先级是默认受管 Shell 的行为；Shell 内建命令、用户自定义 PATH/别名和显式启动的其他 Shell 继续遵循各自语义。不会为统一工具实现放宽安全边界，也不会安装宿主缺失软件。

### 任务生命周期与通知

`app.shellTasks` 在启动前持久化任务记录并占用助理名额，前台分离只改变模式。运行任务持有私有、不可从请求构造的 context capability，绑定原 owner/assistant/execution/operation；原 Loop 结束不使其失效，也不使任意新工具调用获得有效身份。计算、文件与网络授权仍由原计划持续重查，原计划不扩权。任务结束释放 capability 和名额；完成状态和 `shell_task_finished` 事件在同一事务提交，失败写入由管理器重试。启动时将未完成记录标记中断；不恢复或重放效果。

`platform.Command.Managed` 允许无默认运行截止时间，实际超时与授权到期仍取较早者。PTY 主端由 Runtime 持有，传入隔离进程的仅为专属从端。`interrupt` 发送终端中断字符，作用于前台进程组；`stop` 取消执行并清空 cgroup。后台非 TTY 的 stdout/stderr 分开归档，TTY 合并到 stdout。受控接口限制每次输入 8 KiB、输出读取 16 KiB，并提供字节游标；读档案仍校验助理归属、普通文件与路径。WebUI 将提交文本与控制字符写入区分：仅文本提交携带本地草稿版本，成功且版本仍相同时清空输入；编辑或切换任务会推进版本，避免迟到响应清除新的输入。

提醒按 owner/assistant 聚合（当前架构每助理一个串行 Loop），首个后台或 TTY 启动五分钟计时，新任务不刷新截止时间。触发时写入 `shell_tasks_running`，包含仍存活任务的模式和累计时长；待消费提醒更新原事件，避免排队累积，结束任务从待处理提醒移除。全部结束取消计时和未消费提醒。TTY 退出与同步完成不发送完成事件；后台完成独立于提醒立即发送。它们通过持久事件队列进入 Loop，不走上下文文件注入。

远程 Sandbox 在 info 中声明 `ShellSessions`；长期 Shell 仍必须每三秒续约，服务端十五秒租约失效即终止。只对 Shell 允许没有固定 deadline，文件操作维持原有截止时间限制。终端控制通过绑定原执行身份的 `shell_control`，输出读取走无副作用的 `shell_read`，不会把每次输出轮询写入操作账本。

### 网络

未指定网络资源时保持断网。指定资源时由 Runtime 建立执行专属的 user/net namespace，在其内部自动创建 TUN、地址、路由和只读 resolver 文件。最终 Bubblewrap 仅保留这个隔离网络，绝不共享宿主网络；继续使用 `--disable-userns`、`--assert-userns-disabled`、去除 capabilities 和 seccomp。

Ubuntu 的 Bubblewrap 子 AppArmor 配置可能禁止网络管理；使用独立可信初始化程序进入刚创建且已固定 FD 的 user/net namespace 建网，不修改全局安全策略。keeper 与网络栈位于独立、受限的执行 cgroup（256 MiB / 64 PID / 100% CPU）。通过匿名 SOCK_SEQPACKET 传递 TUN FD、就绪与授权消息，控制消息上限 512 字节，不向脚本挂载宿主控制 socket，也不接受脚本提供 namespace 路径或身份。初始化完成前不执行脚本，网络栈退出立即终止执行，回收失败会关闭 Runner。

gVisor Go netstack 将流量转换为普通 TCP/UDP 连接。传输协议、目标域名/IP、端口、排除项均按同一授权规则检查；其中主机名形式的排除项只在域名解析路径生效，直接以 IP 连接时仅按 IP/CIDR 条目检查。每条连接与 DNS 请求通过私有控制通道向 Runtime 回查授权；授权到期由执行截止时间约束，撤销/取消通过既有同步停止机制关闭所有连接。TCP 支持半关闭；UDP 会话空闲 60 秒回收。流量预算按实际转发的双向载荷计费，避免提前预占阻塞读缓冲饿死另一个方向。

内部 DNS 支持 TCP/UDP 的 A/AAAA 查询，使用系统解析器处理真实 CNAME。返回执行内虚拟地址，将域名身份绑定到连接；新连接重新解析，所有实际答案都须获授权，然后直拨已检查 IP。不会将域名直接转成全局 IP 白名单。虚拟映射限定 1024 项，TTL 30 秒，DNS 并发单独限定 8，不能从外部覆盖或跨执行复用。未授权域名、任意 DNS 记录类型和未知虚拟 IP 均拒绝。

网络授权只表达主机/地址、端口、TCP/UDP，不表达 TLS 内的 URL 路径或内容。内置 internet/lan/host（互联网访问／局域网访问／宿主机访问）三项预设只是候选资源，连接和助理授权仍显式进行；三项均开放 TCP/UDP 全部端口，host 包含本服务自身监听端口；lan 仅含 RFC1918/ULA、不含回环，宿主局域网地址仍按 lan 判断；部署可关闭预设。旧的未声明协议配置拒绝加载；远程协议使用 `/v2/sandbox/` 和 `Protocol=3`，连接和执行前拒绝旧服务。详细配置、内部宿主别名和限额见 [部署说明](../../docs/deployment/sandbox.md)。

就绪探针实际执行隔离命令，并在临时回环端点上验证 DNS、TUN 与 TCP 转发，不依赖公网。宿主工具环境通过标准 curl 下载；最小 BusyBox 环境不承诺 HTTPS 下载能力。工具参数不是强制边界，网络仍由 Runtime 授权。`NetworkBackend=transparent`、`NetworkProtocols=[tcp,udp]` 随能力接口报告；不得仅凭进程存活宣称网络就绪。

### Shell TCP 端口转发

`forward_ports` 在工具层及 platform 层校验，每次执行最多 4 个唯一有效端口。Runtime 在启动命令前分配监听器，回执携带任务 `Forwards`；目标固定为原执行的 IPv4 回环，不接受宿主地址、PID 或 namespace 路径。仅有转发而无出站授权时也建立执行专属网络会话，但出口规则为空，仍禁止外联。

网络 keeper 的 user/net namespace FD 在其生命周期内固定持有；启动脚本使用副本，不再提前关闭原 FD。每次转发由受限 helper 经固定 FD 进入对应 namespace，连接 `127.0.0.1:<port>`，通过私有 SCM_RIGHTS 通道将已连接 TCP socket 返回父进程。socket 保持其原 namespace，helper 不持续充当数据代理。helper 纳入执行的辅助 cgroup，创建过程有超时；最终脚本仍无 namespace 管理权。执行完整身份和声明端口在每次拨号校验，本机同时重查原授权。

本机调用 Runner.DialForward，远程经 `/v2/sandbox/forward` 的认证 HTTP/1.1 Upgrade（`molr-tcp-v1`）建立流。沿用受控拨号、固定 TLS 证书、服务令牌和拒绝浏览器 Origin；该端点仅在独立 Sandbox 服务。Manager 校验活动执行身份及租约，Runner 再检查原执行与端口。已建立的流使用有界帧传递原始字节及半关闭，支持 WebSocket、SSE 和需要写半关闭后读取响应的 TCP 协议；流不写入操作账本，也不占普通 JSON 请求的并发槽。

Runtime 每入口 32 个连接、全局 128 个；Runner 每执行 32 个目标连接，远程流入口全局 64 个。探测是独立的有超时 TCP 拨号。转发不使用原有出站累计流量预算。任务结束、撤权、租约失效会关闭目标连接；Runtime 同时关闭监听器和客户端连接。远程观察连接失败后回收入口，不自动转接其他任务或重放命令。

`Info.PortForwarding` 作为协议的能力字段，带转发请求必须先确认支持；不支持时明确拒绝，不静默忽略。工具回执、`shell_task list`、SDK 与任务界面保留相同字段，模型上下文裁剪也保留实际地址。用户操作、地址选择及无额外入口鉴权的边界见 [部署文档](../../docs/deployment/sandbox.md#shell-端口转发)。

### 权限变更与故障

构造计划后、释放启动屏障前重新检查授权和版本；活动执行登记与授权变更需要串行协调，覆盖“检查通过后立刻撤权”的竞态。撤销、缩小范围、资源断开和授权到期均关闭出口连接、终止相关执行，等待所有持有文件句柄的子进程退出；单纯卸载目录不能替代终止进程。撤权结果需区分授权已撤销和执行清理是否完成，不能在清理未完成时声称已停止全部访问。权限变化不在运行中的沙盒内动态增加挂载，下一次调用重建环境。

出口每次连接重新授权，并对已有连接设置受执行租约限制的生命周期；控制通道断开、租约失效或 Runtime 退出时拒绝继续访问。已经写入或已经发送的数据无法通过撤权撤回，这一点由操作回执说明。

Shell 有副作用。操作 ID、计划摘要与授权版本应在启动前持久化；重启后执行结果未知时先回收进程、核对产物与回执，不能自动重跑整段脚本。

## 5. 跨平台接口与目录

当前复用 `platform.Command`、`Runner`、`Process` 与能力报告，见 [平台接口代码](../../runtime/internal/platform/api.go)。早期草案中的 `ExecutionPlan` 是设计概念，并非已实现的同名公共类型；本文不再维护假设的目录树与接口表。

能力报告必须表达真实隔离探针结果，后端不能删除不支持的限制后继续执行。进程正常退出不代表业务任务自动验收成功；记录输出、退出分类和显式产物，不承诺完整文件变更清单。

宿主路径只在可信适配层存在。资源内路径使用独立逻辑表示，再由后端映射；不能在跨平台共享代码中假定 `/workspace`、盘符或大小写规则。未来 Windows 必须单独处理 UNC、重解析点、junction、ADS、命名管道及句柄继承。Shell 类型明确为 POSIX shell、PowerShell 等，不能把同一脚本字符串自动换解释器执行。

| 平台 | 实现方向 | 必须独立验证的差异 |
| --- | --- | --- |
| Linux，一期 | Bubblewrap + seccomp + cgroup v2 + 受控出口 | user namespace、AppArmor、cgroup 委派、内核与 CPU 架构兼容 |
| macOS，未来 | 评估 Seatbelt、受控 helper 与网络代理；必要时使用 Linux VM | 不直接假定有 Linux 式挂载和 cgroup 硬限额；App Sandbox 的签名/继承模型也不能直接替代任意脚本执行策略 |
| Windows，未来 | 评估 AppContainer／受限身份、ACL、Job Object 与受控网络策略 | 任意 CLI 兼容性、句柄和文件权限、网络强制出口；Job Object 负责生命周期/资源管理，不能单独充当文件与网络沙盒 |

Windows/macOS 暂仅返回不支持 Shell 沙盒；业务与权限结构仍可运行。WSL2 后端若未来提供，应明确显示为 Linux 执行环境，不算原生 Windows 后端。接口复用不代表各平台安全能力天然等价。[Microsoft AppContainer](https://learn.microsoft.com/en-us/windows/win32/secauthz/appcontainer-isolation) · [Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects) · [Apple App Sandbox 继承](https://developer.apple.com/library/archive/documentation/Miscellaneous/Reference/EntitlementKeyReference/Chapters/EnablingAppSandbox.html)

## 6. 部署与本机评估

生产部署依赖 Bubblewrap、合适的 Linux 内核、可用 user namespace、cgroup v2 委派，以及受控 rootfs。推荐使用独立非特权账号，通过 systemd 服务委派执行子树；安装程序负责依赖与服务集成；Runtime 自动生成运行资产、工作目录，自动发现委派或申请专用临时单元，并执行实际隔离探针。用户无需配置这些内部路径，WebUI 或 Agent 不得修改 sysctl、AppArmor、服务权限或挂载策略。运行时不调用 sudo，也不要求整个 Molr Server 以 root 运行。

在 Docker 中部署 Molr 时还需要评估嵌套 user namespace、seccomp/AppArmor 和 cgroup 委派。不能为了启动沙盒默认要求 `--privileged`、挂载 Docker socket 或关闭所有宿主安全限制；配置不满足时关闭 Shell 能力并给出诊断。

部署步骤、默认限额和故障原因统一见 [Linux 命令环境](../../docs/deployment/sandbox.md)。旧的单机最小探针记录已删除，它不能证明当前服务的隔离能力。

## 7. 验证

普通 Go 测试覆盖授权、操作幂等、初始化失败关闭和接口。真实 Linux 测试在 `runtime/` 目录执行：

```bash
bash scripts/test-sandbox-linux.sh -count=1 -v
```

脚本使用独立 systemd 用户单元和临时数据，验证自动资源申请、直接目录读写、单文件硬上限、进程回收、受控出口与 FD 隔离；依赖不足时失败，不能以 skip 代替通过。测试运行必须遵守工作区隔离约定。

每次实际服务启动还需认证查询 `/api/sandbox`，不能用普通单元测试、最小 Bubblewrap 探针或 HTTP 连通性替代。检查字段与期望值见部署指南。

## 8. 参考实现

[Anthropic Sandbox Runtime](https://github.com/anthropics/sandbox-runtime) 展示了原生沙盒结合代理过滤的路线，可参考其文件与网络边界。Molr 保持 Go 实现和固定模块装配，不直接引入它的 Node 运行时依赖；如实际复用代码，应先核对对应版本的许可证与安全边界。
