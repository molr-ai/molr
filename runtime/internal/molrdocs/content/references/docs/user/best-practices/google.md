# 操作 Google 服务

通过 [Google Workspace CLI](https://github.com/googleworkspace/cli)（`gws`），助理可以查询 Google Drive 文件、读取 Gmail、整理 Calendar 日程，以及处理 Docs 和 Sheets。助理通过 Shell 调用 CLI，无需安装 Molr 专用插件。该项目位于 `googleworkspace` 组织下，但不属于 Google 正式支持的产品。

## 安装与环境

先完成[执行环境准备](https://molr.ai/zh/docs/user/best-practices/#准备执行环境)，再把下面的委托发给助理：

> [!PROMPT]
> 按 https://github.com/googleworkspace/cli 的安装说明，把 gws 安装到你的工作区。检查系统与架构，选择合适的发行包并核对校验值。先验证版本、Drive 查询的 schema 和 dry-run；然后引导我在自己的电脑上完成 Google 授权，把凭据配置到工作区，最后查询五份未删除文件的名称与链接。

从[项目发行页](https://github.com/googleworkspace/cli/releases)选择对应系统与架构的程序，解压到 `/work/.local/bin/` 等工作区目录。精简 Linux 沙盒可优先选择 `unknown-linux-musl` 包；直接使用发行二进制不需要 Node.js、npm 或 Rust。下面用 `gws` 表示安装后的程序，未加入 PATH 时使用其绝对路径。

```bash
gws --version
TOKIO_WORKER_THREADS=2 GOOGLE_WORKSPACE_CLI_CONFIG_DIR=/work/.config/gws gws schema drive.files.list
TOKIO_WORKER_THREADS=2 GOOGLE_WORKSPACE_CLI_CONFIG_DIR=/work/.config/gws gws drive files list \
  --params '{"pageSize":5,"fields":"files(id,name,webViewLink),nextPageToken","q":"trashed = false"}' \
  --dry-run
```

沙盒示例设置 `TOKIO_WORKER_THREADS=2`，限制异步工作线程数。宿主机 CPU 核数较多时，默认线程数可能耗尽沙盒进程配额，导致命令一直等待。

`schema` 查询命令参数与响应结构；`--dry-run` 预览请求，不执行目标 API 操作，也不证明账号已经授权。首次获取 API 定义需要联网，即使只是查询某个服务的命令帮助。安装需访问 GitHub 及发行文件下载地址，使用时需访问 Google Discovery、OAuth 和对应服务的 API；在助理网络授权中开放实际需要的范围。

## 在自己的电脑上授权

`gws auth login` 使用本机浏览器回调。浏览器在你的电脑、CLI 在远程 Molr 沙盒时，授权链接中的 localhost 指向不同环境。本节采用项目提供的[无界面环境授权方式](https://github.com/googleworkspace/cli#authentication)：在有浏览器的电脑登录，再将导出的凭据交给沙盒使用。

1. 在自己的电脑上也安装 `gws`。按照项目的手动 OAuth 配置说明，创建或选择 Google Cloud 项目，启用本次需要的 API，配置 OAuth 同意屏幕，并创建 **Desktop app** 类型的 OAuth 客户端。应用处于测试模式时，将自己的账号加入测试用户。
2. 下载客户端 JSON，按 `gws auth status` 显示的 `client_config` 路径保存为 `client_secret.json`。已安装并登录 `gcloud` 时，也可以使用 `gws auth setup` 辅助配置；普通 CLI 调用不要求安装 `gcloud`。
3. 先只授予 Drive 只读范围，在浏览器中确认账号和权限，再导出凭据。在自己的电脑终端运行：

```bash
gws auth login --readonly -s drive
umask 077
gws auth export --unmasked > credentials.json
```

通过已授权的文件目录，将 `credentials.json` 复制到助理的 `/work/.config/gws/credentials.json`。将目录权限设为 `0700`、文件权限设为 `0600`；凭据文件包含可续期授权，不要把内容粘贴到聊天、写入 Skill 或放入任务交付目录。文件传递方式见[文件与命令](https://molr.ai/zh/docs/user/guides/files-and-commands/)。

每次调用显式指定相同的配置目录与凭据文件，避免依赖某次终端临时设置的环境变量：

```bash
TOKIO_WORKER_THREADS=2 \
GOOGLE_WORKSPACE_CLI_CONFIG_DIR=/work/.config/gws \
GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE=/work/.config/gws/credentials.json \
gws drive files list \
  --params '{"pageSize":5,"fields":"files(id,name,webViewLink),nextPageToken","q":"trashed = false"}'
```

后续需要 Gmail、Calendar 或 Sheets 时，在自己的电脑重新登录并选择相应服务，例如 `gws auth login --readonly -s drive,gmail,calendar,sheets`，再重新导出、替换凭据。创建或修改内容时选择对应写入权限。服务名称只限制授权选择范围，最终以同意屏幕确认的 OAuth scopes 为准；Google 账号本身也必须能访问目标资料。

## 可以这样委托

> [!PROMPT]
> 查找指定 Drive 文件夹内与本周会议有关的资料，按主题整理名称、摘要和原始链接。

<!-- -->

> [!PROMPT]
> 整理过去七天需要我回复的 Gmail 邮件，列出发件人、主题和建议下一步，只生成回复草稿。

<!-- -->

> [!PROMPT]
> 查看明天的 Calendar 日程，按我指定的时区整理时间、参与人和准备事项。

<!-- -->

> [!PROMPT]
> 读取这份 Google Sheets 的指定工作表和单元格范围，汇总数据并指出缺失项，保留来源链接。

给出目标账号、文件或文件夹链接、工作表范围和时间范围。需要发送邮件、创建日程或修改文档时，说明目标和执行范围；希望先审阅时，要求助理先给草稿。常用操作可以先查 `gws gmail --help`、`gws calendar --help` 等命令，参数复杂时用 `gws schema <服务.资源.方法>` 查询。更多示例见[项目文档](https://github.com/googleworkspace/cli#helper-commands)。

## 验证与排查

首次授权后，执行上面的五条文件查询，核对返回内容属于预期账号。空列表可能只是没有符合条件的文件；`auth status` 显示已配置凭据，也不能代替一次实际 API 查询。其他服务需分别用少量只读请求验证。

| 现象 | 检查方式 |
| --- | --- |
| 程序存在却无法执行 | 核对架构、执行权限和发行包类型；精简 Linux 环境优先检查是否误用了依赖动态库的 GNU 包 |
| 版本可查，但服务命令一直等待 | 每次调用设置 `TOKIO_WORKER_THREADS=2`，并检查沙盒进程配额；线程也计入该配额 |
| schema 或服务帮助获取失败 | 检查 Google Discovery 网络访问；缺少账号授权通常不是 schema 查询失败的原因 |
| 浏览器授权后 CLI 一直等待 | 确认浏览器和登录命令在同一台电脑；远程沙盒按上述导出方式配置 |
| 缺少凭据或登录状态丢失 | 核对每次调用的配置目录、凭据文件路径和文件是否保留；`GOOGLE_WORKSPACE_CLI_TOKEN` 会优先于凭据文件 |
| API 返回权限不足或未启用 | 检查 Cloud 项目是否启用了该 API、OAuth scopes 和目标文件的共享权限；补充授权后重新导出凭据 |
| 续期失败或 `invalid_grant` | 检查授权是否撤销、OAuth 应用状态及刷新令牌有效期，重新登录并替换导出文件 |

程序升级后先用 `gws --version`、对应命令的 `--help` 和 `schema` 核对用法，再运行小范围只读查询。撤销连接时，除了移除工作区凭据，也在 Google 账号的授权管理中撤销相应应用访问。
