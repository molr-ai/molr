# 资源与文件工具合同

更新：2026-10-06。本文面向工具与 Runtime 开发者；用户授权操作见 [资源与权限](../../docs/guides/permissions-and-budget.md)，运行环境见 [Linux 命令环境](../../docs/sandbox/linux.md)。

## 显式 Full Access 调用

`shell_execute` 及 `file_*` 接受可选 `full_access`，省略／false 保持普通隔离与范围检查。true 要求所选节点允许该能力且助理持有有效 Full Access 计算授权；文件调用不以目录授权代替此权限，也不因已有 Full Access 授权而自动切换路径。Full Access 文件路径可以是绝对路径，`/work` 保留为当前助理工作区别名；普通调用仍拒绝超出工作区和目录授权的路径。多模态快照保留 FullAccess 标记并在加载时重新授权。

`Requestable.FullAccessAvailable` 表示可申请，`Resources.FullAccess` 和 `Sandboxes.FullAccessComputeIDs` 表示显式调用当前已获授权；`Grants[].Scope.FullAccess` 区分授权类型。模型结构化投影保留这些字段及申请中的 `full_access`，防止上下文丢失权限语义。

## 模型如何理解资源与权限

`permission_list`（原 `resource_list`）分组查询当前助理已获得的授权、可申请的权限目录和内置能力，关闭 Shell 后仍可使用。旧工具名不再发布或接受。它按当前助理的有效授权返回计算、存储、网络及具体模型，不返回宿主路径或凭证。每个资源范围的 `Grants` 保留有效授权的 ID、Actions、Scope 和 ExpiresAt，单独提供 `Operations`：操作、使用途径、当前是否可用和不可用原因。能力快照不是授权凭证，实际执行仍重新检查权限、有效期和部署限制。

- `Resources` 的 `Source: "user_grant"` 表示用户授权资源。存储 `Path` 采用资源内路径，`/` 明确标记为 `resource_root_not_host_root`，不是系统根目录。一个目录的写权限不能扩展到另一个目录；不同模型的授权也不合并成 Provider 全部模型的授权。
- `Requestable` 提供共享资源目录中当前账号可申请、已连接且未断开资源的安全元数据，包括 ResourceID、Name、Kind、SandboxID、支持的 Actions；模型精确到 ModelID/ModelName，存储 PathPrefixes 的 `/` 表示资源内最大范围。只描述可以申请什么，不代表已获授权或当前可用。最多 200 项，超限标记 RequestableTruncated；CurrentTime 用于确定申请的到期时间。
- `BuiltInStorage` 的 `Source: "runtime"` 或 `"sandbox"` 表示计算环境自带能力。`/work` 属于当前助理，跨执行保留；`/global` 是当前账号共享的只读文件目录（AGENTS.md、skills、记忆），文件工具与本地 Shell 使用相同文件；`/tmp` 仅属于本次命令执行，下一次执行重新创建。工作区默认通过 `file_*` 可读写，不依赖额外存储或计算授权，但需要目标沙盒存储可用，普通成员还需对应沙盒和工具的组引用；计算隔离是否就绪不决定文件能力；`Operations` 单独说明 Shell 途径所需的计算授权。临时目录仅通过当前 `shell_execute` 使用。这些内置目录都不作为用户授予的外部目录列出。
- 不通过目录权限位或文件是否存在推断用户授权；`/molr` 等内部路径不属于业务资源目录。
- `file_read`、`file_write` 和 Shell 共用存储绑定、路径授权及部署目录限制。写入文件工具可以使用仅写授权，但 Shell 的可写挂载同时暴露读取，因此必须同时具备读、写授权；查询会分别说明这两种操作的可用性。

Shell 只挂载请求中选择的资源范围。`permission_list` 返回稳定的 `ShellPath`，形式为 `/resources/r_<资源 ID 摘要>/<资源内路径>`，同一资源与路径不受名称或请求参数顺序影响。不同资源的同名目录不会冲突。脚本直接使用返回的路径，不需要自行计算摘要。重叠或重复的挂载范围被拒绝；仅授权子目录时，不暴露宿主父目录及兄弟目录。

## 授权请求

`permission_request` 创建申请，不直接授予权限。必填 `resource_id`、`resource_kind`、`resource_description`、`actions`、`reason` 和未来的 RFC3339 `expires_at`（组合申请只在顶层设置，`additional_requests` 中各项继承同一时间）；存储必须填写资源内 `path_prefixes`。必须从 `permission_list.Requestable` 选择真实的 `resource_id`；模型申请还需所属 `model_id`。未知资源或只有描述的申请在创建前被拒绝，不生成卡片或等待状态；需先由管理员连接并分配资源。计算、网络与受管工作区申请还须提供额度，组合申请及校验规则见[资源授权额度](../security/resource-quotas.md#数据与审批)。工具描述要求最小权限：只申请当前任务需要的资源、最少操作、最窄范围和最短合理授权时间，不默认整资源或永久授权。整个请求最多 4096 字节，单个助理最多 20 项待审批请求；身份和任务由执行上下文确定，不接受模型传入所有者或助理 ID。

请求、助理聊天卡片和工具操作回执在同一事务内提交为 `pending`，提交失败两者一起回滚，通过操作回执和申请记录防止重复创建；同一助理、任务和规范化需求（不含理由文本）复用待处理请求及其 MessageID，不重复发卡。卡片类型为 `permission_request`，TargetID 指向真实请求；显示内容与当前处理状态由授权 API 读取。点击卡片直接打开预填表单。请求完成后执行保存 `PermissionPending`，同批未执行工具返回 `not_started`，当前执行以 `waiting / permission_pending` 结束，不再调用模型。重启恢复保留这一边界。

所有者 API 以版本和幂等键提交 `approve` 或 `reject`。用户可以修改资源、模型、操作、目录范围和期限；助理建议遵循最小权限，所有者最终确认值才决定授权，不把助理建议当作所有者的权限上限。实际授权继续通过原有 Grants 服务检查资源、模型、部署边界并创建，申请状态、授权和恢复事件在同一事务内提交。拒绝不会创建授权。重复审批不会生成第二份授权或第二个恢复事件。

`permissions` 自动上下文每次模型调用前读取有效授权，新增、修改、撤销、到期导致版本变化并替换旧快照；不含宿主路径或凭证，也不发起远程沙盒探针。最多 200 项且 128 KiB，超限显式标记 Truncated，并保留全量授权摘要检测变化。

`permission_requests` 自动上下文来源提供所有待审批及近期决定（最多 40 项）；这只是审批证据，不是授权凭证或指令。决定写入 `resume_requested` 事件并保留原任务 ID，批准与拒绝均恢复 loop，下一次模型调用携带最新决定与有效授权；模型需检查实际范围后继续。手动停止边界、暂停或终态任务阻止旧申请自动唤醒。用户在等待期间仍可发送新消息；新消息作为独立事件处理。

## 文件工具

文件工具默认操作 Runtime 本地当前助理的唯一持久工作区：省略 `resource_id`，使用相对路径（如 `notes.txt`）或 `/work/notes.txt`，列工作区根目录使用 `.` 或 `/work`。此能力不需要额外存储或计算授权，但普通成员需对应沙盒和文件工具的组引用。计算隔离不可用时可继续使用就绪的存储；`sandbox.enabled: false` 则关闭本机存储、计算和网络，访问本机工作区会失败，不影响另行连接且可用的远程沙盒。访问外部存储时必须显式传 `resource_id`，此时 `path` 才解释为该资源内路径并检查授权。`/tmp` 不属于持久工作区，只能在当前 Shell 执行中使用。工作区由服务端按所有者与助理身份绑定，文件工具和 Shell 共用同一目录；工具参数不能选择其他助理。文件工具和 Shell 共用存储授权及部署目录限制；当前文件工具在 Linux 上执行，其他平台在具备同等路径隔离实现前拒绝执行。

工作区在自动上下文准备或首次文件访问时初始化以下缺失目录，使用受限描述符操作并拒绝符号链接；不覆盖已有内容：

| 路径 | 用途 |
| --- | --- |
| `/work/inbox/` | 原始附件和导入材料 |
| `/work/projects/` | 按语义事项组织的工作文件、脚本和草稿；没有新增 project 业务实体 |
| `/work/outputs/` | 交付文件；保存不等于发布，仍通过 conversation_publish 发送 |
| `/work/.agents/skills/`、`memory/`、`reviews/` | Skills、详细记忆和做梦回顾；摘要仍在 `/work/MEMORY.md` |
| `/work/.local/state/molr/` | 按 XDG state 语义保存大工具结果和 Shell 全文输出等执行记录，跨命令和重启保留；当前不自动清理 |
| `/work/.cache/` | 可重新生成的缓存，跨命令保留；当前没有自动清理 |

业务目录是默认约定，用户可以指定其他位置；系统目录保留既定用途。默认目录不需要 Shell 授权即可使用。写入或移动到新子目录时，可显式设置 `create_parents=true`；每个缺失父目录都须位于已授权的写入范围内，否则应先通过其他已授权方式创建。目录用途和生命周期通过 workspace 自动来源说明，具体文件列表、正文及运行产物继续按需读取。指令加载范围见[上下文更新](../runtime/agent-context.md#来源与加载)。

| 工具 | 参数和行为 | 外部存储权限（自身工作区默认允许） |
| --- | --- | --- |
| `file_list` | 列出单个目录，`offset` 从 0 开始，默认 `limit=100`、最多 200。返回 `Entries`、`Version`、`Truncated`、`NextOffset`。下一页必须传上一页的 `Version` 作为 `expected_version`；使用文件系统顺序，不递归、不跟随符号链接 | read |
| `file_read` | 读取 UTF-8 文本，`offset` 为从 1 开始的行号，默认 `limit=200`、最多 2000。返回原始换行格式的 `Text`、`StartLine/EndLine`、`Version`、`Truncated`、`NextOffset`；只有读到末尾才给出 `TotalLines` | read |
| `file_write` | 创建或完整替换 UTF-8 文件。必须显式传 `overwrite`；`false` 禁止覆盖已有文件，`true` 允许覆盖。可携带 `expected_version` 防止覆盖读取后发生的变化；默认要求父目录存在，`create_parents=true` 可创建缺失且有写权限的父目录 | write |
| `file_edit` | 使用 `old_text`、`new_text` 精确替换，必须传读取返回的 `expected_version`。零匹配、版本过期或多处匹配均拒绝修改；显式 `replace_all=true` 才替换所有匹配，不使用正则或模糊匹配 | 同一路径的 read + write |
| `file_move` | 原子移动或重命名单个普通文件；`to_path` 使用与源路径相同的工作区／存储资源，目标必须不存在，必须传源文件当前 `expected_version`。不移动目录或跨资源移动；可用 `create_parents=true` 创建有写权限的目标父目录 | 源路径及目标路径的 write |
| `file_delete` | 按 `expected_version` 删除单个普通文件；版本过期、符号链接及目录均拒绝，不提供递归删除 | 目标路径的 write |

读取输出最多 64 KiB、单次扫描最多 64 MiB；单行过长、二进制和无效 UTF-8 返回明确错误。写入、局部编辑的文件与文本最多 1 MiB，更大的批量或二进制处理使用 Shell。目录分页的偏移上限为 100,000，超大目录可用 Shell 分批处理。

文件修改先写入同目录临时文件并同步，再重新检查授权和目标版本，最后原子替换。显式拒绝覆盖、版本冲突及匹配失败不会修改目标文件；写入成功后同步失败则保留结果待核对，不盲目重试。读取拒绝符号链接、硬链接和特殊文件；列目录可显示链接条目的名称与类型，但不读取目标。Runtime 内同一路径的文件修改串行执行；等待锁时可取消，取得锁后再次检查取消状态，此时已取消或超时的请求会归还锁令牌，不继续执行受保护的文件操作和媒体缓存更新。Shell 或其他宿主进程不参与这个锁，版本检查不是对任意外部进程的全局文件事务。

多模态读取补充见 [多模态文件](../runtime/multimodal-files.md)。文件与 Shell 的访问模型不同；不要从目录权限位或能力快照推断当前授权。

## 长工具结果

所有送入模型的工具文本结果统一限制为 16 KiB。超长结果完整保存到独立助理工作区 `/work/.local/state/molr/tool-result-<sha256>.json`，返回确定性结构摘要、有限首尾预览、执行/分页元数据、字节数、SHA-256 和全文路径。保存使用受限路径检查与原子写入，禁止链接和覆盖其他文件；操作回执保留原文，保存失败后的恢复复用回执，不重新执行副作用。

`file_read` 新增 `byte_offset`（0 起始）和 `max_bytes`（默认及最大 4096），返回 `NextByteOffset`，可逐段读取 JSON 长行。字节读取不能与行范围、metadata_only 或媒体选项混用；起点须为 UTF-8 字符边界。普通文本按行读取仍使用原有 64 KiB 页上限；超出统一内联限额的页同样以文件返回。单次 JSON 工具结果保留 64 MiB 异常输出防护，多媒体内容继续沿用独立的能力/媒体预算。

Shell 在预览截断之前将 stdout/stderr 分别流式写入 `/work/.local/state/molr/tool-shell-<digest>-stdout.txt` 和 `-stderr.txt`，结果返回路径、实际字节数和 `OutputComplete`。预览保持原部署限制（默认 64 KiB）；全文捕获每次执行合计最多 `min(64 MiB, sandbox.max_file_bytes)`，超出配额或写盘失败取消该次进程并明确标记保存不完整，不能把部分文件当成全文。文件随助理工作区保留，当前不自动按时间清理；长期高输出使用需按磁盘预算管理。`Exit.OutputTruncated` 仅表示预览被截断，不代表已保存全文不完整。

## 网络范围查询

网络权限范围由 `permission_list` 在已授权 `Resources` 和可申请 `Requestable` 两组的 `Network` 字段中返回：`Target` 包含实际部署的 `Protocols`、`Addresses`、`Ports`、`ExcludeAddresses`、`ExcludePorts`，`Restrictions` 明确沙盒网关工作在第四层（传输层）、只支持 TCP/UDP，不在此范围内的协议（如 ICMP）不会被转发；还说明系统地址及 DNS 虚拟映射限制，`LoopbackAccess` 说明沙盒回环与宿主别名，授权额度和共享用量见 `Grants[].Constraints`、`Usage`，可申请上限见 `Requestable[].AuthorizationCeilings`。名称可由用户修改，不能从名称推断范围；字段取自对应本地或远程沙盒的当前规则。`Known=false` 时通过 `Reason` 说明离线、禁用或目标已移除，不把旧快照当作当前授权范围。可申请项不是已授权项，实际执行仍需有效授权；Runtime 自动选择当前助理在同一沙盒内的有效网络授权。

## 显式远程目标与文件传输

助理不配置默认沙盒。省略 `sandbox_id` 固定解析为 `local`；资源 ID 不参与隐式选节点，目标与计算、目录、网络资源所属节点必须一致。远程普通文件操作须提供目录资源；远程 Shell 默认 `/tmp`，没有持久助理工作区。文件、Shell、模型媒体、工具回执和端口转发保留明确的节点来源。远程上下文发现与 `/api/assistants/{id}/sandbox` 接口已移除。

`file_transfer` 接受 `source`、`destination`、`overwrite`、可选 `source_version`、`expected_version`。端点使用 `sandbox_id`、`resource_id`、`path`，只允许一端本地、一端显式远程。源必须可读，目标必须可写；最大 20 MiB，覆盖必须带目标版本。下载检测读取过程中的版本变化；写入使用原子替换。任意 Shell 或宿主进程仍不参与跨节点文件事务。操作回执及远端上传操作身份用于判断结果，不自动重试未知写入。

聊天附件和头像使用本地文件；远程文件须先显式下载。通用长结果始终写到本地 `/work`。远程 Shell 日志按操作游标回收到本地，`SandboxID` 描述执行节点，`OutputSandboxID` 描述日志所在节点；日志不完整不改变已发生命令的执行结果。

## 通用 file_search

file_search 沿用 FileTarget：默认本地 `/work`，`/global` 只读，外部目录需读取授权，远程显式 sandbox_id，Full Access 仍需逐调用明确请求和计算授权。搜索使用受控文件遍历与 Go RE2，不拼接 Shell 命令，不需要普通 Shell 计算授权。

必填 path。pattern 默认普通字符串，regex 开启逐行正则；case_sensitive 默认 true。glob/exclude_glob 为相对路径模式，支持 **、*、?、字符组；不解释 .gitignore。结果的 TargetKind 标明实际搜索目标为 file 或 directory。mode=content 返回路径、行号、片段及 context_lines（0–5）上下文；mode=files 只返回匹配路径，不传 pattern 时按路径筛选。include_hidden 默认 false，但显式选择的隐藏目录会遍历，例如 `/work/.agents/memory`。

每次搜索先拆分 glob 的路径段，遍历期间复用模式；`**` 按路径段逐步回退匹配，不为每个候选建立递归状态表。模式仅在本次搜索内保留，包含／排除顺序、目录剪枝和逐路径授权检查保持不变。

内容扫描在整份文件通过版本、大小和 UTF-8 校验后按行遍历，直接引用文件字节，只保留当前行和最多五行前文；命中时按需读取后文。达到结果上限时停止行遍历，不提前为整份文件建立行索引。

默认 50 条、最多 200 条结果；最多扫描 10,000 项、32 层目录、每文件 2 MiB、累计 64 MiB，5 秒预算及 32 KiB 结果预算。超过 4096 字节的行、非 UTF-8、二进制和链接按原因计入 Skipped；长行和过大文件使 Complete=false。Complete、Truncated、StopReason 及扫描统计明确结果边界，不提供可能漏项的分页游标。目录或文件扫描期间变化也报告不完整。结果只用于定位，修改前 file_read 获取当前内容与版本。

文件搜索不解析记忆状态，也不特殊处理某个目录为“记忆库”。记忆规则由系统提示词说明，memory_* 工具均已移除。远程搜索随 Sandbox 协议 6 提供，需要配套更新节点。

Dream 直接复用上述通用文件工具，仅通过执行上下文限制可见工具和记忆路径范围；普通助理也可使用 `file_move`、`file_delete`，本机与远程存储使用相同参数及权限规则。移动、删除与其他有副作用工具共用操作回执，成功操作重放不会再次移动或删除后来出现的文件。Dream 的撤销与来源记录由观察回调维护，不另设工具 schema 或执行器。详见[做梦与文件记忆](../runtime/dream-memory.md)。

## Shell 执行与端口转发

`shell_execute` 使用 `compute_id`，远程调用另需显式 `sandbox_id`。普通命令最多同步等待 60 秒，未结束时沿用原任务转后台；`background: true` 立即后台返回，`tty: true` 创建终端，两者互斥。`timeout_seconds` 为可选运行上限，0 或省略不设置；授权到期、撤销和资源限额仍生效。

`forward_ports` 接受最多四个不重复的 TCP 端口（1–65535）。服务监听执行内的 IPv4 回环或 `0.0.0.0`。回执 `Task.Forwards` 含 `Protocol`、内部 `Port`、外部 `Address`、HTTP 便利链接 `URL` 和 `State`；`URL` 不表示自动识别服务协议。运行管理接口见[HTTP API](http-api.md#shell-任务管理)。

端口在 Runtime 上监听，远程任务通过固定证书与服务令牌认证的 TLS Upgrade 通道转发，要求远端声明 `PortForwarding=true`。每个入口最多 32 个连接，Runtime 总计最多 128 个，执行内最多 32 个目标连接；连接探测另计，不消耗出站网络累计额度。转发不提供用户认证或 TLS 终止，也不形成不同端口间的 Cookie 隔离。

后台任务结束生成助理事件，前台同步完成及 TTY 退出不重复生成完成事件。存在后台任务或 TTY 时，每五分钟聚合提醒一次，计时从首个任务进入后台或终端打开开始。TTY 在沙盒内建立前台进程组，文本输入追加换行，Ctrl-C 指向前台任务。每次终端使用新沙盒，启动读取 `/work/.profile`；非交互命令不加载该文件。

GPU 沿用计算权限，无额外 Shell 参数。申请中的 `constraints.compute.gpu.device_ids` 必须来自可申请设备清单；临时范围使用 `temporary.limits.compute.gpu.device_ids` 并保留完整计算额度。详细范围校验见[资源额度](../security/resource-quotas.md)。
