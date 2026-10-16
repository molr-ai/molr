# 操作 Codex

助理可以通过 [Codex CLI](https://developers.openai.com/codex/cli/) 把读代码、修改文件和运行测试交给 Codex，再检查实际改动和测试结果。执行使用 Molr 现有的 Shell、后台任务和文件工具。

## 安装与授权

先完成[执行环境准备](https://molr.ai/zh/docs/user/best-practices/#准备执行环境)，再把下面的委托发给助理：

> [!PROMPT]
> 按 Codex 官方说明，将适合当前沙盒的 CLI 安装到你的 /work 内。检查程序版本、依赖和网络，使用独立的配置目录，引导我完成登录。先在一个人工示例项目上验证读文件、修改文件和运行测试，把结果给我。

可以使用官方发行二进制，或在 Node.js 和 npm 可用时通过 `@openai/codex` 安装到工作区。宿主机个人目录中的安装不会自动出现在沙盒内。以下命令用 `codex` 表示安装后的程序；未加入 PATH 时使用绝对路径，每次非交互调用都显式准备所需的 PATH。

```bash
codex --version
codex exec --help
codex login --device-auth
codex login status
```

在自己的浏览器中完成授权。也可以按[官方认证说明](https://developers.openai.com/codex/auth/)使用 API Key 或配置支持的模型服务。不要把密钥写入聊天、任务说明或可分享的脚本。Linux 普通沙盒的 HOME 是 `/work`，Codex 默认配置与会话保存在 `/work/.codex`；该目录应与项目产物分开，并在后续调用中保留。工作区中的登录凭据可被该助理及其运行的程序读取。

Codex 的账号、模型配置和用量独立于 Molr 助理的模型连接；外部 CLI 的模型费用不会自动计入 Molr 模型用量。使用本地服务时，Codex 的模型端点须支持其要求的 Responses 协议；Linux 沙盒访问宿主回环服务使用 `host.molr.internal`，并授予对应网络资源，不能用沙盒自己的 `localhost` 代替。

## 在 Molr 沙盒内执行

先确认所选 Linux 沙盒的命令、文件、进程、网络隔离和资源限制均可用，并授予任务需要的计算、网络和目录权限。以下自动执行示例要求使用普通隔离 Shell，`full_access` 保持省略或 false。

Codex 自己的 `workspace-write` 沙盒可能无法在 Molr 内创建所需的嵌套命名空间。已验证的 Linux 调用方式使用 `--sandbox danger-full-access`，让子 CLI 依赖外层 Molr 隔离。这个参数关闭的是 Codex 内层隔离；其子进程仍受 Molr 的挂载范围、网络授权和资源限额约束。不要把本节命令照搬到宿主机、关闭隔离的节点或 Molr Full Access 执行中。工作目录本身也不限制 CLI 访问同一助理的其他可见文件。

让助理准备一个独立项目和任务说明，明确可修改文件、测试命令及交付要求。例如将任务写入 `/work/outputs/codex/task.txt`，执行：

```bash
mkdir -p /work/outputs/codex
codex exec --sandbox danger-full-access --json \
  -C /work/projects/demo \
  -o /work/outputs/codex/result.txt \
  - < /work/outputs/codex/task.txt \
  > /work/outputs/codex/events.jsonl \
  2> /work/outputs/codex/stderr.txt
```

项目应已初始化为 Git 仓库；确实不使用 Git 时，按官方说明选择 `--skip-git-repo-check`。通过 `shell_execute` 设置 `background: true` 和合适的 `timeout_seconds`，助理即可在任务完成事件到达后读取结果，也可以通过 `shell_task` 查看输出或停止任务。无需在脚本中使用 `nohup` 或 `&` 让进程脱离管理。

`events.jsonl` 保存结构化过程事件，`result.txt` 保存最后回复，`stderr.txt` 用于诊断。检查 CLI 退出状态、实际文件或 Git diff，并独立重跑测试；只有回复声称成功不能算完成。使用方式见[非交互模式](https://developers.openai.com/codex/noninteractive/)。

## 继续同一个任务

从 JSONL 的 `thread.started` 事件保存 `thread_id`，在同一助理、同一配置目录和项目下继续：

```bash
cd /work/projects/demo
codex exec --sandbox danger-full-access resume SESSION_ID --json \
  'Review the changes, add any missing boundary tests, and run the tests.' \
  > /work/outputs/codex/follow-up.jsonl \
  2> /work/outputs/codex/follow-up-stderr.txt
```

将 `SESSION_ID` 替换为该任务的准确 ID。并行任务使用独立目录或 worktree，避免两个进程同时修改同一份文件。续接前检查上次执行留下的文件和状态；超时或停止不会回滚已经完成的修改。

## 可以这样委托

> [!PROMPT]
> 用 Codex 阅读这个仓库，解释登录流程和相关测试。只做分析，列出依据的文件位置。

<!-- -->

> [!PROMPT]
> 在独立 worktree 中让 Codex 修复这个失败测试。给它复现步骤和验收标准，完成后你检查 diff 并重新运行测试，把修改说明交给我。

## 验证与排查

接入后先用小项目验证“读取 → 修改 → 测试 → 后台完成 → 读取结果”，再验证按会话 ID 继续任务。Linux 沙盒调用已以 Codex CLI 0.163.0 和支持工具调用的本地模型验证；其他系统、发行版本和账号登录方式仍需在实际环境验证。

遇到命名空间错误，检查是否重复启用了 Codex 内层沙盒及外层 Molr 是否满足本节条件，不修改宿主安全设置来强行嵌套。认证错误检查 CLI 登录和配置目录；网络错误检查模型端点及授权；CLI 退出成功但产物不符合要求时，把实际失败信息作为下一轮任务输入。CLI、项目依赖和测试所需的工具必须分别检查。
