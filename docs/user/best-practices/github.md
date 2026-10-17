# 操作 GitHub

通过官方 [GitHub CLI](https://github.com/cli/cli)（`gh`），助理可以查询仓库、Issue、Pull Request（PR）和 Actions 运行结果，也可以按委托创建 Issue、评论或 PR。

## 安装与授权

先完成[执行环境准备](README.md#准备执行环境)，再把下面的委托发给助理：

> [!PROMPT]
> 按 https://github.com/cli/cli#installation 的官方说明，把 GitHub CLI（gh）安装到你的工作区。检查运行环境和安装位置，引导我创建 fine-grained personal access token，只选择我指定的仓库，先授予读取 PR 所需的权限并设置有效期。提供官方创建页面和配置步骤，让我在终端中保存令牌，不要让我把令牌发到聊天中。每次调用通过 GH_TOKEN 读取该令牌，先验证指定仓库的 PR 列表；需要增加权限时说明具体用途，由我决定。

让助理选择适合沙盒系统和架构的官方发行包，将程序放在 `/work` 内；涉及克隆、提交和推送时，还需检查 Git 是否可用。下面用 `gh` 表示安装后的程序，未加入 PATH 时使用其绝对路径。

### 推荐：限定仓库与权限的令牌

在 GitHub 的 [Fine-grained token 创建页面](https://github.com/settings/personal-access-tokens/new)选择仓库所属的 **Resource owner**，将 **Repository access** 设为 **Only select repositories**，只勾选任务所需仓库，并设置有效期，例如 30 天。在 **Repository permissions** 中按任务选择权限，未使用的权限保持关闭：

| 任务 | 权限起点 |
| --- | --- |
| 查询 PR 列表、说明和差异 | Pull requests: Read-only |
| 查询 Issue | Issues: Read-only |
| 读取或克隆代码 | Contents: Read-only |
| 查询 Actions 运行和日志 | Actions: Read-only |
| 创建或评论 Issue | Issues: Read and write |
| 创建或评审 PR | Pull requests: Read and write；需要推送分支时另加 Contents: Read and write |

这些权限按任务组合，不必全部勾选；Metadata 的必需只读权限保留默认值。具体命令可能调用多个接口，所需权限以 [GitHub 接口权限说明](https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens)为准。令牌仍可读取公共仓库，仓库选择主要限制私有资源访问和写入范围。组织可能要求管理员审批；外部协作者、跨组织操作等场景存在限制，见[令牌配置与限制](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)。

通过 `GH_TOKEN` 使用该令牌，无需执行 `gh auth login`；这是 CLI 官方推荐的 fine-grained PAT 用法。让助理准备仅用于凭据的目录 `/work/.config/gh`，再由你在终端中将令牌保存到 `/work/.config/gh/token`，文件只含令牌本身。目录权限设为 `0700`，文件权限设为 `0600`，不放入仓库或任务产物，不在聊天、命令历史或日志中输出令牌。该文件是明文存储，能读取它的助理或进程也能使用令牌。

每次调用显式从文件注入 `GH_TOKEN`，并通过 `GH_CONFIG_DIR` 使用同一配置目录。先检查程序和认证状态：

```bash
gh --version
(
  GH_TOKEN="$(cat /work/.config/gh/token)" || exit 1
  export GH_TOKEN
  : "${GH_TOKEN:?Token file is empty}"
  GH_CONFIG_DIR=/work/.config/gh gh auth status --hostname github.com
)
```

子 Shell 中的检查让文件缺失或为空时停止，避免回退到其他已保存的凭据。普通非交互命令不会加载 `/work/.profile`，不能依赖上一次终端的 `export`；后续调用沿用下文的显式注入方式。`GH_TOKEN` 优先于 `GITHUB_TOKEN` 和已保存的登录凭据，见[授权手册](https://cli.github.com/manual/gh_auth_login)和[环境变量说明](https://cli.github.com/manual/gh_help_environment)。

### 备选：浏览器默认授权

如果你接受较广的账号授权，也可以保留 CLI 默认的浏览器／设备验证码登录方式：

> [!PROMPT]
> 按官方说明安装 GitHub CLI（gh），使用浏览器默认授权方式登录。我了解这会申请 repo 等较广权限，授权范围不限定于本次指定的仓库。把官方授权链接和设备验证码发给我，由我在浏览器中确认；配置保存在工作区内，先验证指定仓库的 PR 列表。

该方式默认申请 `repo`、`read:org`、`gist` 等权限；其中 `repo` 包含公共与私有仓库的广泛读写能力，实际访问仍受账号权限和组织策略限制。令牌泄漏或命令误用可能影响任务以外的仓库。`--repo OWNER/REPO` 和 `GH_CONFIG_DIR` 都不能限制令牌权限，`gh auth refresh --remove-scopes` 也不能移除基础的 `repo` 权限。见 [OAuth 权限范围](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps)和[权限刷新说明](https://cli.github.com/manual/gh_auth_refresh)。

在子 Shell 中清除令牌环境变量，确保本次使用浏览器登录及其保存的凭据：

```bash
(
  unset GH_TOKEN GITHUB_TOKEN
  export GH_CONFIG_DIR=/work/.config/gh
  gh auth login --hostname github.com --git-protocol https --web &&
    gh auth status --hostname github.com
)
```

在自己的浏览器中打开 CLI 给出的链接，输入设备验证码并确认授权。没有系统密钥链时，`gh` 会回退到明文配置文件存储令牌，该目录同样不放入仓库或任务产物。如果之后改用限定权限令牌，旧 OAuth 授权不会自动失效；不再需要时到 GitHub Settings → Applications → Authorized OAuth Apps 撤销 GitHub CLI 授权，这也可能影响其他使用该授权的 CLI 环境。仅运行 `gh auth logout` 不会撤销服务端令牌，见[退出登录说明](https://cli.github.com/manual/gh_auth_logout)。

## 可以这样委托

> [!PROMPT]
> 使用限定仓库的令牌，查看 OWNER/REPO 最近更新的十条未关闭 Issue，按问题类型整理，保留编号、标题和链接。只需查询；若缺少 Issues 只读权限，说明所需权限。

<!-- -->

> [!PROMPT]
> 使用限定仓库的令牌，阅读这个 PR 的说明和代码差异，概括改动与潜在问题，附上来源链接。需要读取源码或 Actions 日志时说明对应的只读权限；无法读取的检查结果标为未验证。

<!-- -->

> [!PROMPT]
> 根据这份故障描述，为 OWNER/REPO 起草 Issue，包含复现步骤、预期结果和实际结果，先给我确认后再创建。创建前检查令牌是否已选中该仓库并具有 Issues 写权限，缺少时引导我调整该项授权。

提供仓库的 `OWNER/REPO` 或完整链接；针对单个 Issue 或 PR 时附上编号或链接。需要提交评论、推送分支、创建或合并 PR 时，说明目标仓库、分支和执行范围。

## 验证与排查

把下面的 `OWNER/REPO` 替换成令牌已选中的目标仓库，先用 Pull requests 只读权限查询少量未关闭 PR：

```bash
(
  GH_TOKEN="$(cat /work/.config/gh/token)" || exit 1
  export GH_TOKEN
  : "${GH_TOKEN:?Token file is empty}"
  GH_CONFIG_DIR=/work/.config/gh gh pr list --repo OWNER/REPO --state open --limit 5 --json number,title,url
)
```

如果选择的是浏览器默认授权，则使用保存的凭据验证：

```bash
(
  unset GH_TOKEN GITHUB_TOKEN
  GH_CONFIG_DIR=/work/.config/gh gh pr list --repo OWNER/REPO --state open --limit 5 --json number,title,url
)
```

显式指定 `--repo` 可以在仓库目录外查询，并避免选错项目，但它不是权限边界。成功返回空列表也可能只是没有符合条件的 PR；需要扩大范围时调整状态和数量。参数见[PR 列表手册](https://cli.github.com/manual/gh_pr_list)。

认证成功但目标仓库不可访问时，检查 Resource owner、已选仓库、令牌权限和有效期，以及组织审批状态。遇到 403、404 或接口不支持时，先核对具体接口要求，不自动改用全仓库或默认浏览器授权。`gh auth status` 成功不代表拥有目标操作所需的权限，应以实际只读查询验证。

身份不符合预期时，检查是否有 `GH_TOKEN` 或 `GITHUB_TOKEN` 覆盖已保存凭据，但不要输出其值。后续调用丢失认证状态时，检查是否每次注入令牌、令牌文件是否可读，以及 `GH_CONFIG_DIR` 是否一致、配置是否保留。令牌过期或撤销后，在 GitHub 更新授权并替换本地凭据文件。

其他命令先用 `gh <命令> --help` 核对用法，完整能力见[官方手册](https://cli.github.com/manual/)。
