# 操作 ZCode

助理可以通过 [Z.ai 官方 ZCode](https://github.com/zai-org/ZCode) 的 CLI 委托代码分析、修改和测试，再读取结果、检查文件并继续任务。调用复用 Molr 的 Shell 和后台任务。

## 安装与授权

先完成[执行环境准备](README.md#准备执行环境)，再把下面的委托发给助理：

> [!PROMPT]
> 从 Z.ai 官方来源安装适合当前沙盒的 ZCode CLI，将程序、Node.js 和配置保存在 /work 内。检查发行版本及其命令帮助，引导我连接模型。先用人工示例验证修改代码和运行测试，成功后再操作我的项目。

确认来源为 `zai-org/ZCode` 或 [ZCode 官网](https://zcode.z.ai/cn/docs/install)，不要仅按 `zcode` 这个名字安装同名第三方工具。保留同一版本的完整运行资源，不能只复制 `zcode.cjs`。Node.js 版本以该发行版要求为准。

Linux 已验证的安装方式是从官方 ZCode 3.15.1 AppImage 中提取 CLI 0.16.9，配合 Node.js 24.14.0；无需启动桌面界面或安装到系统目录。让助理下载适合架构的官方 AppImage，保存为 `/work/tools/zcode/ZCode.AppImage`，再执行：

```bash
cd /work/tools/zcode
chmod +x ZCode.AppImage
./ZCode.AppImage --appimage-extract 'resources/glm/*'
./ZCode.AppImage --appimage-extract 'resources/config/provider/*'
mkdir -p agent/provider
cp -R squashfs-root/resources/glm/. agent/
cp squashfs-root/resources/config/provider/zcode-builtin.json agent/provider/
node /work/tools/zcode/agent/zcode.cjs --version
node /work/tools/zcode/agent/zcode.cjs --help
```

上面的 `node` 应来自已准备好的工作区 Node.js。每次非交互调用都准备同一 PATH，确保 ZCode 启动的子进程也能找到 Node.js。新发行版若调整目录结构，按实际包内容和[官方运行说明](https://github.com/zai-org/ZCode#zcode-命令行版)检查，不混用不同版本的文件。

Linux 普通沙盒默认将登录、会话和用户配置保存在 `/work/.zcode`。将其与项目产物分开并跨调用保留；其中的凭据可被该助理及其运行的程序读取。使用平台账号时，在终端运行适合自己账号的登录命令，并在浏览器完成官方授权：

```bash
node /work/tools/zcode/agent/zcode.cjs login zai --no-browser
# For a BigModel account, use login bigmodel --no-browser
```

也可以按所用版本的 Provider 配置格式连接自己的模型服务。安装后的 CLI、个人模型配置和默认模型应一起核对；Molr 已配置的模型连接不会自动传给 ZCode。Linux 沙盒访问宿主回环模型服务使用 `host.molr.internal` 并授予对应网络资源。外部 CLI 的模型费用不会自动计入 Molr 模型用量。

## 委托任务与读取结果

先确认所选 Linux 沙盒的命令、文件、进程、网络隔离和资源限制均可用。下面的自动执行示例仅适用于普通隔离 Shell，`full_access` 保持省略或 false。

ZCode 的 `--mode yolo` 允许自主执行普通工具操作，隔离由外层 Molr 提供。不要在宿主机、关闭隔离的节点或 Molr Full Access 中照搬该命令；仅指定 `--cwd` 不会把访问范围限制为该目录。需要人工审批或只读规划时，先核对该版本的执行模式和非交互行为。权限模式的边界见[官方说明](https://github.com/zai-org/ZCode/blob/main/NOTICE.md)。

让助理在独立项目中执行任务，例如：

```bash
mkdir -p /work/outputs/zcode
node /work/tools/zcode/agent/zcode.cjs \
  --cwd /work/projects/demo --mode yolo --json \
  --prompt 'Read the project and fix the specified addition function without changing existing tests. Run the tests and report actual changes and results.' \
  > /work/outputs/zcode/result.json \
  2> /work/outputs/zcode/stderr.txt
```

通过 `shell_execute` 设置 `background: true` 和合适的 `timeout_seconds`，让助理在后台完成事件到达后读取结果。运行中可通过 `shell_task` 查看输出或停止任务，不需要 `nohup` 或额外后台化。

CLI 0.16.9 的 `--json` 输出是任务结束后的 JSON 对象，包含 `sessionId`、`response`、`usage` 和 `projection`。它不是 Codex 的 JSONL 事件流，运行时文件可能仍为空；先查看 Molr 任务状态和 stderr。检查退出状态、实际文件或 Git diff，并独立重跑测试。

## 继续同一个任务

保存结果中的 `sessionId`，在同一助理、项目和配置下继续：

```bash
node /work/tools/zcode/agent/zcode.cjs \
  --cwd /work/projects/demo --mode yolo --json \
  --resume SESSION_ID \
  --prompt 'Review the changes, add any missing boundary tests, and run the tests.' \
  > /work/outputs/zcode/follow-up.json \
  2> /work/outputs/zcode/follow-up-stderr.txt
```

将 `SESSION_ID` 替换为该任务返回的准确 ID。并行任务使用独立目录或 worktree；超时、停止或认证失败后，先检查已完成的修改，再决定是否续接。

## 可以这样委托

> [!PROMPT]
> 让 ZCode 在独立 worktree 中实现这个小功能。我会提供可修改范围和验收条件。完成后你检查 diff，重新运行项目测试，并整理交付说明。

<!-- -->

> [!PROMPT]
> 继续上次 ZCode 任务，用保存的会话 ID 把这份测试失败日志交给它修复。不要覆盖其他正在进行的任务。

## 验证与排查

Linux 沙盒调用已以官方发行包中的 CLI 0.16.9、Node.js 24.14.0 和支持工具调用的本地模型验证。平台账号的浏览器登录、其他系统和不同发行版本需要在实际环境验证。

找不到内置 Provider 配置时，检查 `agent/provider/zcode-builtin.json` 是否与 CLI 配套。自定义模型提示未选择模型时，核对该版本个人 Provider 文件的 schema、模型列表和默认选择；不能把其他 CLI 或仓库新版本的配置直接套用。需要定位失败原因时让助理读取 `/work/.zcode/cli/log/` 中对应任务的日志，避免把完整日志公开分享。

使用自己的配置路径时，按版本说明同时设置 `ZCODE_BUILTIN_PROVIDER_CONFIG_FILE` 和 `ZCODE_PERSONAL_PROVIDER_CONFIG_FILE`，每次调用保持一致。认证、模型端点、项目依赖和网络授权分别检查；保留命令退出状态和测试结果作为验收依据。
