# 操作 GitHub

通过官方 [GitHub CLI](https://github.com/cli/cli)（`gh`），助理可以查询仓库、Issue、Pull Request（PR）和 Actions 运行结果，也可以按委托创建 Issue、评论或 PR。

## 安装与授权

先完成[执行环境准备](https://molr.ai/zh/docs/user/best-practices/#准备执行环境)，再把下面的委托发给助理：

> [!PROMPT]
> 按 https://github.com/cli/cli#installation 的官方说明，把 GitHub CLI（gh）安装到你的工作区。检查运行环境和安装位置，引导我完成 GitHub 登录，把授权链接和设备验证码发给我。先验证指定仓库的 PR 列表。

让助理选择适合沙盒系统和架构的官方发行包，将程序放在 `/work` 内；涉及克隆、提交和推送时，还需检查 Git 是否可用。下面用 `gh` 表示安装后的程序，未加入 PATH 时使用其绝对路径。

通过 `GH_CONFIG_DIR` 将配置保存在工作区内，每次调用使用同一目录。首次在终端运行：

```bash
gh --version
GH_CONFIG_DIR=/work/.config/gh gh auth login --hostname github.com --git-protocol https --web
GH_CONFIG_DIR=/work/.config/gh gh auth status --hostname github.com
```

在自己的浏览器中打开 CLI 给出的链接，输入设备验证码并确认授权。没有系统密钥链时，`gh` 会回退到明文配置文件存储令牌；该目录只用于凭据配置，不放入仓库或任务产物。无交互环境也可以按官方说明通过 `GH_TOKEN` 提供令牌，每次调用都需可读取；它会优先于已保存的登录凭据。登录方式见[授权手册](https://cli.github.com/manual/gh_auth_login)，配置变量见[环境变量说明](https://cli.github.com/manual/gh_help_environment)。

## 可以这样委托

> [!PROMPT]
> 查看指定仓库最近更新的十条未关闭 Issue，按问题类型整理，保留编号、标题和链接。

<!-- -->

> [!PROMPT]
> 阅读这个 PR 的说明、代码差异和检查结果，概括改动、潜在问题及尚未通过的检查，附上来源链接。

<!-- -->

> [!PROMPT]
> 根据这份故障描述，为指定仓库起草 Issue，包含复现步骤、预期结果和实际结果，先给我确认后再创建。

提供仓库的 `OWNER/REPO` 或完整链接；针对单个 Issue 或 PR 时附上编号或链接。需要提交评论、推送分支、创建或合并 PR 时，说明目标仓库、分支和执行范围。

## 验证与排查

把下面的 `OWNER/REPO` 替换成目标仓库，先查询少量未关闭 PR：

```bash
GH_CONFIG_DIR=/work/.config/gh gh pr list --repo OWNER/REPO --state open --limit 5 --json number,title,url
```

显式指定 `--repo` 可以在仓库目录外查询，并避免选错项目。成功返回空列表也可能只是没有符合条件的 PR；需要扩大范围时调整状态和数量。参数见[PR 列表手册](https://cli.github.com/manual/gh_pr_list)。

登录成功但目标仓库不可访问时，检查当前账号、令牌的仓库范围和权限，以及组织要求的授权。身份不符合预期时，检查是否设置了覆盖已保存凭据的 `GH_TOKEN`。后续调用丢失登录状态时，核对 `GH_CONFIG_DIR` 是否一致、配置是否保留。

其他命令先用 `gh <命令> --help` 核对用法，完整能力见[官方手册](https://cli.github.com/manual/)。
