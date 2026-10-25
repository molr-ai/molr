# 更新与备份恢复

## 版本检查与发布

用户设置 → 关于显示 WebUI 与 Runtime 的版本、commit 和一致性状态，同时展示发布源的推荐版本。登录后每五分钟读取服务端版本状态，页面重新可见或连接恢复时补查；关闭自动发布检查仍保留前后端一致性检查。版本或 commit 不一致时显示提示，由用户保存输入后自行刷新，或由管理员核对分离部署的版本。

Runtime 配置支持：

```json
{"updates": {"source": "https://downloads.example.com/releases/stable", "auto_check": true}}
```

通过安装包部署时，关于页使用程序目录 `installation.json` 中保存的发布源，并查询其 `stable` 渠道；上述配置供未使用安装器的源码部署使用。无发布源时不会联网。`auto_check: false` 关闭后台及关于页自动发布检查，仍可手动检查。开发／本地构建默认不自动检查。公网源要求 HTTPS；HTTP 源必须是明确配置的私网或回环数字地址。

后台启动后延迟 30～60 秒检查，随后每 24 小时加少量随机延迟检查，失败一小时后重试。进入关于页或页面恢复可见时，超过五分钟未检查则刷新发布信息。手动检查最短间隔一分钟；全实例合并并发请求。检查失败保留上次成功结果及时间，检查本身不下载或安装程序。

构建生成版本包和校验文件；发布流水线在所有目标架构通过后发布 GitHub Release，再部署官网与渠道清单。桌面 App、Docker 和 Runtime 的关于页统一只推荐正式版，当前运行 Nightly、Beta 或 RC 时也一样。Runtime 将发布源末尾的 `/preview` 或 `/nightly` 替换为 `/stable`；自定义无渠道路径的发布源仍使用原路径，但返回的版本必须为正式版。正式版高于当前版本时提示升级，相同时显示已是最新正式版，低于当前版本时显示当前版本高于最新正式版且不推荐降级。没有适用正式版时显示暂无适用正式版，不回退预发布或 Nightly；检查只提示更新，不自动下载或安装。

维护者的版本命名、平台管理、首次远端配置与失败重试见[发行维护](https://molr.ai/zh/docs/dev/releasing/releases/)。源码部署者可通过 `updates.source` 指定正式版发布源。手动安装／更新仍可使用 `molr-setup update --channel preview|nightly` 显式选择其他渠道，关于页的正式版推荐不改变保存的 CLI 订阅；较低版本不会作为自动降级执行。

管理员在发现更新后可查看固定目标版本的升级命令。运行账户、实例名和程序路径取自安装记录；源码部署或缺少记录时按本指南操作。示例：

```bash
molr-setup update --version 0.2.0
```

升级前按本页备份流程保留数据。安装器核对下载包的版本和架构，保留已有账号、数据和配置，保存 `molr.previous`、`molr-setup.previous` 与 `server.previous.json` 后替换程序并重启。服务重启会中断运行中的任务。启动后先等待 HTTP 就绪，再执行一次认证检查，核对实际 Runtime 版本及沙盒能力；认证需要当前密码／MFA或被限流时，输出“验证待完成”和 `doctor` 命令，不循环登录。失败保留备份和错误信息，不自动回滚；上一份程序及配置不包含完整数据备份。

## 备份恢复与升级

使用 `molr-setup backup / restore` 迁移安装器管理的实例。`molr` 不再提供数据库快照 CLI。Linux/macOS 通过安装记录识别实例；Windows 使用当前用户的桌面 Runtime 安装目录和实例名。Docker 和自行启动的进程不由此命令自动停止。

### 创建备份

```bash
molr-setup backup --output /private/molr-backup.tar.gz
# Select another instance with --record /path/installation.json or --name molr-example.
# Keep the source stopped when switching machines:
molr-setup backup --output /private/molr-migration.tar.gz --keep-stopped
```

命令取得维护锁，正常停服并确认进程退出，在私有临时目录复制数据及配置、生成 `backup.json`，随后打包。默认在文件复制结束后恢复原运行状态，再压缩归档；原来停止的实例保持停止。失败时尝试恢复原运行状态并报告错误，不发布不完整归档。目标文件必须不存在且位于备份源目录之外。可通过 `TMPDIR` 选择有足够空间的暂存位置；暂存位置也必须位于备份源目录之外。

| 内容 | Runtime | 独立 Sandbox |
| --- | --- | --- |
| 程序 | `molr`、`molr-setup`、发行信息及许可证 | 相同 |
| 数据库与身份 | 完整数据库、加密主密钥 | 完整 `sandbox.db`、服务令牌及密钥；保留操作回执与网络额度账本 |
| 文件 | 数据目录，包括附件、记忆和默认工作区；自定义 `sandbox.work_root` 单独收集 | 不备份用户文件、命令输出缓存或外部目录内容 |
| 配置 | 部署配置目录及引用的 TLS 证书、私钥、价格／汇率文件 | 相同 |
| 外部资源 | 只保留目录引用、授权及远程 Sandbox 连接信息 | 只保留目录配置 |

历史备份、进程锁和可重建的 `sandbox/runtime` 运行环境缓存不进入归档。备份不连接远程 Sandbox 收集文件，也不打包宿主系统依赖、模型服务或额外安装的浏览器组件。恢复后按目标机器重新准备这些依赖。归档含主密钥与凭证，按私有数据保护；丢失 `keys/master.key` 无法解密 Runtime 已保存的凭证。

### 恢复到另一台机器

目标机器尚未安装 Molr 时，同平台可在空目录中从备份归档取出程序，再执行解出的维护工具；此时尚无 PATH 入口。不同操作系统或 CPU 架构使用目标平台、与备份版本及 commit 相同的完整发行包：

```bash
tar -xzf /private/molr-backup.tar.gz program
./program/bin/molr-setup restore --archive /private/molr-backup.tar.gz --name molr-restored --dry-run
./program/bin/molr-setup restore --archive /private/molr-backup.tar.gz --name molr-restored --yes
```

跨平台时不要执行上例中的源平台程序，改用目标发行包的 `bin/molr-setup`。归档仍完整校验，源平台二进制不安装；数据库、附件和工作区正常恢复。平台专用沙盒配置改为目标平台默认值并报告，服务账户及服务注册重新生成。外部目录引用和远程连接保留；不存在或不能在目标平台解析的目录不可用。文件名冲突、无法恢复的符号链接等错误会阻止应用，不静默丢弃文件。工作区中的平台专用程序不保证可以执行。

恢复支持：

- 新建实例：通过 `--name` 选择实例；`--program-root` 指定程序基目录，实例使用独立子目录。Linux 配置和数据沿用 XDG／系统布局，macOS 沿用用户布局。
- 覆盖实例：明确选择 `--record` 或该实例的安装工具，并增加 `--overwrite --yes`。目标模式须一致；程序和数据一起恢复到备份版本。停服后先在目标缓存目录保存 `before-restore-*.tar.gz`，再替换。应用失败且新服务尚未启动时恢复原文件；服务启动后不自动回退数据库，避免重复外部动作。
- 预览与延后启动：`--dry-run` 解包校验并显示计划，不修改目标；`--no-start` 恢复并注册服务但不启动，便于调整地址、证书和机器相关配置。局域网／本地 HTTP 可用 `--port`、`--public-host` 调整地址；直接 TLS 和代理模式先使用 `--no-start`，再编辑配置。

Windows 使用目标平台发行包中的 `molr-setup.exe`：

```powershell
.\bin\molr-setup.exe backup --name molr-desktop-0123456789abcdef --program-root C:\Molr\existing --output C:\Backups\molr.tar.gz
.\bin\molr-setup.exe restore --name molr-restored --program-root C:\Molr\restored --archive C:\Backups\molr.tar.gz --yes --no-start
```

Windows 当前管理 Runtime 桌面实例，独立 Sandbox 的服务注册不在该安装器范围内。桌面恢复使用回环 HTTP；恢复后会报告需要完成已认证的沙盒能力验证。

恢复不会复制源机器的系统 UID/GID 或服务注册文件。Runtime 旧会话、访问令牌及登录挑战会被撤销；账号及 MFA 配置保留。远程 Sandbox 按原地址、令牌和证书信任正常尝试连接，不承诺新机器网络可达。缺失外部目录不创建为空目录替代，不导致其他内置数据丢失。

数据恢复完成与服务验证成功分别报告。核对账号、助理、聊天、附件、工作区、模型连接以及沙盒能力后再完成切换；源机器保持停止，避免两边同时处理渠道消息和任务。运行进程不能恢复，未完成操作按已保存状态核对，不自动重放结果未知的外部动作。文件系统 project ID 和工作区配额需在目标文件系统重新验证。

忘记密码时，由有主机管理权的操作者停服后执行 `molr recover -password-stdin`；需要重置二次验证时另加 `-reset-mfa`。本机恢复会撤销全部会话与令牌。升级前保留完整备份；恢复和升级是两个独立步骤。


## 文件日志与轮转

Runtime 自己写入的文件日志统一使用 `internal/logging` 模块，包括 `--log-file` 指定的服务日志（HTTP 请求、启动及运行诊断）和可选的模型响应日志。记录格式由调用模块负责，文件权限、并发写入、轮转和旧文件清理由统一模块负责。标准输出／标准错误不经过文件轮转；journald、launchd 或 shell 对标准流的收集与重定向按部署环境管理。

服务配置可统一调整每组文件日志的策略，以下为默认值：

```json
{
  "logging": {
    "max_file_bytes": 268435456,
    "backup_files": 1
  }
}
```

`max_file_bytes` 是单文件字节数上限，必须为正整数；默认 **256 MiB**。`backup_files` 是旧文件数量（不含当前文件），范围为 0–100；默认保留当前文件与 1 份旧文件，每组最多约 **512 MiB**。写入将超限时先轮转，旧文件按 `.1`、`.2` 编号，`.1` 最新，超出数量的最旧文件自动删除；设为 0 时轮转只保留新的当前文件。单条记录超过文件上限时拒绝该条写入，不拆分记录。

修改配置后重启生效。服务日志在打开时、模型日志在启用后的首次写入时应用策略；缩小上限或保留数量会清理超出新策略的旧文件。新建日志目录权限为 `0700`，文件为 `0600`。轮转按大小和数量触发，没有按天过期清理；关闭模型日志后，已有文件需按部署方要求删除。

## HTTP 接口耗时排查

Runtime 默认为每次 `/api` 请求输出一条 `http_request` 结构化日志；`molr serve` 写入标准错误，使用 `--log-file /private/runtime.log` 时写入该文件，systemd 部署可通过对应服务的 journal 查看。日志包含 `request_id`、`method`、`route`（注册的路由模板）、`status`、`header_ms`、`duration_ms`、`bytes`、`stream`、`aborted`、`canceled` 与 `write_error`。不记录实际对象路径、查询参数、请求／响应正文、Cookie 或认证头；未匹配路由记录为 `unmatched`。

在浏览器开发者工具的 Network 中选择慢请求，查看响应头 `X-Request-ID`，据此查找同一次请求的日志。`Server-Timing: runtime;dur=...` 以毫秒显示从进入 Runtime HTTP handler 到提交响应头的时间，与日志的 `header_ms` 对应，可在支持 Server Timing 的浏览器 Timing 面板中查看。它包括认证、限流、业务处理及提交响应头前的请求体读取；不包含进入 handler 前的连接／TLS、代理排队，也不包含响应头提交后的传输。

- 浏览器等待首字节时间和 `header_ms` 都大：优先检查服务端处理及依赖；上传请求也可能在等待请求体。
- 浏览器等待首字节时间明显大于 `header_ms`：检查浏览器排队、连接、代理和网络链路，差值不是精确的纯网络耗时。
- `duration_ms` 明显大于 `header_ms`：请求在提交响应头后仍耗时，可能是响应生成或写出受阻，结合下载阶段、`bytes` 和 `write_error` 排查。

`duration_ms` 统计到 handler 退出，包括响应写出时的等待，不表示客户端已收完数据。SSE 在连接结束时才输出日志，`stream=true` 的总时长是连接存活时间，不能当作慢接口；连接建立时即可读取 `Server-Timing`。客户端取消时 `canceled=true`；handler 异常退出时 `aborted=true`，若尚未提交响应头则 `status=0`。

## 模型响应诊断日志

原始模型 HTTP／SSE 响应默认不保存，也不进入数据库。排障时可在服务配置中设置：

```json
{
  "diagnostics": { "model_responses": true }
}
```

重启后生效。响应写入 `<data_dir>/logs/model-responses/responses.jsonl`，文件权限和保留策略遵循上文的 `logging` 配置。日志包含响应正文及调用 ID，可能包含对话内容；不记录请求认证头。日志写入失败不影响模型执行。关闭开关停止新增记录，已有文件需要按部署方的保留要求删除。

此开关与 `debug.enabled` 独立；工作台仍可展示摘要、标准消息内容、HTTP 状态、耗时及用量，无需开启原始响应日志。

## 数据目录与加密密钥

Runtime 默认把数据保存在私有数据目录中；自定义工作区、外部授权目录和远程沙盒需另外管理。默认目录示意：

```text
数据目录/
├── runtime.db       # 主数据库：账号、聊天、任务、执行记录、网络账本
├── keys/            # 加密密钥：用来加密封存的模型访问密钥
├── files/           # 聊天附件与助理产出的文件
├── media/           # 图片、音频等处理缓存（可自动重建）
├── sandbox/         # 默认助理工作目录
└── backups/         # 数据库快照的临时维护目录（不进入实例备份）
```

需要了解的几点：

- 聊天内容和任务记录以原始形式保存在您自己的数据库里，不会发送给 Molr 官方。
- 模型的访问密钥等敏感凭证是加密保存的；加密密钥单独放在 `keys/` 目录，会随完整实例备份保存。请按秘密材料保护备份包；丢失密钥时，已保存的模型密钥将无法恢复。
- 与云端模型对话时，对话内容会发送给您选择的模型服务商；这一点与是否自己部署无关。

备份与恢复的具体操作见[部署指南](https://molr.ai/zh/docs/admin/)。
