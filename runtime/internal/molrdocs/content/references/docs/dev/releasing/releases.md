# 发行维护

Runtime 发行包仅包含 `bin/molr` 与安装维护工具 `bin/molr-setup`。同一个 `molr` 通过 `serve --mode server|sandbox` 选择完整服务或沙盒节点；从已解压的发行包安装时使用 `./bin/molr-setup --mode server|sandbox`。模式保存在安装记录中，升级和修复沿用原模式。

发行包存放在 GitHub Releases，官网与发布清单通过 Cloudflare Workers 静态资源一起部署。当前产物是内嵌 WebUI 的 Linux/macOS Runtime；平台与环境要求统一定义在 [`release/platforms.json`](https://github.com/molr-ai/molr/blob/main/release/platforms.json)。


普通版容器镜像通过 Release／Nightly 流水线发布到 `ghcr.io/molr-ai/molr`，同时支持后端和独立沙盒模式，不提供容器内隔离。Dockerfile 以 Debian 13 slim 为基础，静态 Go 程序内嵌 WebUI 并去除调试信息；基础镜像固定多架构摘要。安装、权限与更新见 [Docker 部署](https://molr.ai/zh/docs/admin/deployment/docker/)。


源码仍保留 `isolated`、`sandbox-tests`、`isolation-test` 目标及配套策略，用于部署者本地构建与验证，不进入镜像自动发布矩阵。操作见 [Docker 源码隔离目标](https://molr.ai/zh/docs/admin/deployment/docker/#从源码构建容器内隔离目标)。

普通版默认命令为 `molr container`，由镜像在命名卷内准备配置和账号；`MOLR_MODE=sandbox` 自动准备独立节点证书、令牌及项目目录，所有服务过程保持 UID/GID 65532。`MOLR_PUBLIC_URL` 的显式值更新访问地址与端口，其余配置、账号和节点身份在重启时保留。原有显式 `serve -config` 和源码隔离目标沿用手工配置流程。

## 容器镜像发布

`release-container.yml` 在同版本 GitHub Release 发布成功后运行，或为已存在的 Release 补发。复用准备阶段确定的版本、commit 和发布日期；使用原生 AMD64、ARM64 runner 构建普通版，运行实际镜像的初始化、HTTP、认证能力、静态 ELF 和替换容器后数据／会话保留测试，再按 digest 推送各架构。两种架构全部通过后发布 OCI index，记录平台摘要和构建身份。

- 正式版发布固定版本标签，并仅在版本更高时推进 `latest`。首个正式版前没有 `latest`。
- Beta／RC 只发布固定版本标签，不发布 `preview` 或更改 `latest`。
- Nightly 只发布 `nightly`，不创建固定每日版标签；完整版本与 commit 保留在镜像与 OCI index 中。
- 发布工作流全局串行，旧版本重试不会回退移动标签。同固定版本重试复用已有镜像；身份不符时失败，不覆盖。单架构失败不更新公开标签，短暂认证／网络错误不视为镜像不存在。
- 发布摘要保存 image、version、commit、digest 和实际标签。无标签平台镜像不作为用户安装入口；Nightly 历史不承诺保留，不自动删除用户仍可能引用的 digest。

调用链授予 `packages: write`，使用仓库 `GITHUB_TOKEN`，无需额外 PAT。首次发布后在 GitHub Package 设置中将镜像改为 Public，并验证匿名拉取；源码仓库公开不等于 Package 自动公开。组织需允许仓库发布 package。正式启用前分别验证两种架构的真实 Actions 结果。

镜像更新检查统一使用 GHCR 匿名 pull token 读取 `latest`，仅接受纯 `X.Y.Z` 正式版，不因当前构建是 Nightly、Beta 或 RC 而切换渠道。校验 OCI 身份、架构完整性与当前架构 manifest 摘要后按 SemVer 比较；高于当前版本才报告更新，不推荐降级。`latest` 不存在时报告暂无适用正式版并清除旧推荐，不回退其他标签；网络或清单错误保留上次成功结果。应用不安装镜像，管理员在宿主替换容器。GHCR 检查独立于官网原生发行目录。

## 桌面安装包

Release 与 Nightly 共用 `build-assets.yml`、`native-build.yml` 和 `release-desktop.yml`，依次构建共享前端资源、原生 Runtime 和桌面安装包，在五个原生目标上产出 Linux AMD64／ARM64 DEB、macOS AMD64／ARM64 DMG 和 Windows AMD64 NSIS EXE。三个平台均附带同版本原生 Runtime；Windows payload 由 `apps/desktop/scripts/build-windows-runtime.mjs` 构建，通过桌面安装包分发，不改变 Unix `install.sh` 平台清单。桌面测试、Runtime 生命周期验收、打包及已配置的签名流程全部成功后，发布任务核对平台完整性、commit、版本和 SHA-256，再将安装包、校验文件及汇总 `desktop-manifest.json` 与 Runtime 附件一起上传同一个 GitHub Release 草稿。缺包或校验失败会阻止发布。PR 的桌面测试已纳入统一 `ci.yml`；原独立 `desktop.yml` 已移除，CI 评估包只上传 Actions artifacts。

`release` environment 配置 `MAC_CSC_LINK`、`MAC_CSC_KEY_PASSWORD`、`APPLE_ID`、`APPLE_APP_SPECIFIC_PASSWORD`、`APPLE_TEAM_ID`、`WIN_CSC_LINK` 和 `WIN_CSC_KEY_PASSWORD`。证书使用 electron-builder 支持的输入格式。桌面发行按平台与渠道执行以下策略：

| 平台 | Nightly | Beta／RC 预发布 | 正式 Release |
| --- | --- | --- | --- |
| macOS | ad-hoc 签名，不提交公证 | Developer ID 签名，公证 `optional` | Developer ID 签名，公证 `required` |
| Windows | 有凭据则签名，否则未签名 | 有凭据则签名，否则未签名 | 有凭据则签名，否则未签名 |
| Linux | 不要求签名或公证 | 不要求签名或公证 | 不要求签名或公证 |

macOS 对应用、内置 Go 二进制和 DMG 签名；Windows 对应用与安装包签名，不涉及 Apple 公证。Windows Nightly、Beta／RC 和正式版的签名均为 optional：完全未配置签名凭据时允许未签名；配置不完整或实际签名失败仍阻止发布。macOS Beta／RC 和正式版需要发布者签名，缺少或仅配置部分凭据时失败，不回退到未签名安装包。macOS Nightly 不注入签名或 Apple 凭据，打包后直接生成 SHA-256 和桌面清单，不创建公证中间 artifact，也不运行公证任务。Linux 打包流程不变。PR 和本地 `-local` 评估包不要求发布者签名或公证，不进入公开渠道。

清单的 `signed` 字段表示发布者签名，macOS ad-hoc 为 `false`；独立的 `notarized` 字段表示公证票据已附加且通过验证，非 macOS 平台为 `false`。官网和 Release 说明分别展示签名与公证状态，不从签名推断公证结果，并保留历史版本的实际状态。

官网构建和 `release:catalog` 从下文选择的最新正式 GitHub Release 读取桌面 manifest，核对同版本 Runtime 的 commit、五个平台及附件链接，生成 `/releases/desktop/catalog.json`。早于桌面支持的 Release 不产生桌面下载入口；选中 Release 的桌面清单损坏时构建失败，不回退旧版。生产构建不需要手工提供桌面 catalog。发布后的官网同步同时验证 Runtime 和桌面目录已更新，安装包仍由 GitHub 提供。

macOS 预发布和正式发布分为构建与公证完成两个任务。构建任务先签名应用、内置二进制和 DMG，将 DMG 提交给 Apple 后立即保存安装包、原始摘要、版本、commit 和申请 ID 到 `desktop-notarization-*` Actions artifact，保留 14 天，不保存凭据。Apple 为 DMG 及其嵌套应用生成公证票据。后续任务下载同一份产物，核对身份、摘要、DMG 签名和镜像完整性后查询申请；每次最长等待 30 分钟，短暂网络错误自动重试。审核通过后附加并验证票据，检查 Gatekeeper，再生成最终 SHA-256 和桌面清单。

Intel 公证任务在调用 Apple 工具前，按 [GitHub runner 网络故障 #14924](https://github.com/actions/runner-images/issues/14924) 的临时规避方案阻断出站 UDP 443，使票据附加与验证回退到 HTTPS/TCP。规则仅作用于该任务的 `macos-15-intel` 临时 runner；签名、公证票据和 Gatekeeper 校验仍须通过。该调整不应用于本地机器或 ARM64 runner。修复 workflow 后需运行包含修复的新提交；重跑旧版本任务仍使用原 workflow。

Beta／RC 预发布固定使用 `optional` 公证策略：等待满 30 分钟后，若到期时的查询仍返回 `In Progress`，保留已签名 DMG 的原始字节，生成 `signed: true, notarized: false` 清单并继续发布。正式 Release 固定使用 `required`，必须公证通过、附加票据并验证后才能发布。策略由准备阶段解析出的 `preview`／`stable` 渠道传给桌面工作流，GitHub environment 中的 `MAC_NOTARIZATION_POLICY` 变量不再生效。Nightly 跳过公证。

本地签名打包和手工恢复默认使用 `required`，预发布恢复可显式设置 `MOLR_NOTARIZATION_POLICY=optional`；恢复正式 Release 时保持 `required`。仅接受 `optional` 和 `required`。查询失败或状态无法确认、Apple 拒绝、身份／摘要／签名校验失败，以及通过后的票据／Gatekeeper 校验失败，都阻止发布，不按等待超时处理。

严格模式下公证仍在处理，或任一模式查询失败时，重跑原 workflow 的失败任务即可继续同一申请，无需重新构建或提交。不要为等待公证完整重跑 workflow；完整重跑会创建新产物和新申请。审核拒绝会输出 Apple 诊断并阻止发布，需要修复后使用新版本。artifact 超过保留期后不能继续原产物，需要重新构建。手工恢复可在同 commit 的原生 macOS 检出中下载该 artifact，设置原版本的 `MOLR_VERSION` 和 Apple 凭据，执行 `node apps/desktop/scripts/notarization.mjs <产物目录>`；目录包含 DMG 和 `notarization.json`。可选模式发布后仍保留原申请和中间产物，但后续公证通过不会自动覆盖已发布文件或改变目录中的状态；附加票据会改变 DMG 摘要，发布器拒绝同版本不同字节，需要使用新版本发布。

## 首次启用

在 GitHub 仓库启用 Actions，并创建用于发行的 `release` environment。官网保留 Cloudflare Workers Builds 的 GitHub 仓库授权与 `main` 生产分支自动部署，不需要在 GitHub 设置 `CLOUDFLARE_ACCOUNT_ID`、`CLOUDFLARE_API_TOKEN` 或 `production` environment。

Cloudflare Worker 的 Settings → Builds 使用以下设置：

| 设置 | 值 |
| --- | --- |
| 根目录 | 仓库根目录 |
| 生产分支 | `main` |
| 构建命令 | `npm run build:website:release` |
| 部署命令 | `npx wrangler@4.45.0 deploy` |
| 构建变量 | `NODE_VERSION=22.21.0`、`VITE_SITE_MODE=live` |

平台安装 npm 依赖后执行构建命令。它读取公开 GitHub Releases 的近期元数据，选择各类最新版本、验证下载链接，生成独立临时渠道目录，再构建官网。无需预装 `gh` 或手工提供 Cloudflare 部署 token；部署认证由 Workers Builds 管理。仓库默认 `molr-ai/molr`，fork 可设置 Cloudflare 构建变量 `MOLR_RELEASE_REPOSITORY`；公开发行信息默认匿名读取，如果遇到 GitHub API 限流，可在 Cloudflare 构建 secrets 中提供只读 `GH_TOKEN`。

默认发布源是 `https://molr.ai/releases`，使用其他域名时，在 Cloudflare 构建变量与 GitHub 仓库变量中同步设置 `MOLR_RELEASE_URL`，指向同一 Worker 的 `/releases` 路径。按现有 [`wrangler.jsonc`](https://github.com/molr-ai/molr/blob/main/wrangler.jsonc) 将 `molr` Worker 绑定官网域名。

发布 GitHub Release 不会修改 main。为让正式 Release 完成后也刷新官网，在 Cloudflare Worker → Settings → Builds → Deploy Hooks 创建绑定 `main` 的 hook，将 URL 保存为 GitHub 仓库 secret `CLOUDFLARE_DEPLOY_HOOK`。该 URL 自身具有触发构建的权限，不写入仓库或日志；无需额外 Authorization header。配置入口和原理见 [Cloudflare Deploy Hooks](https://developers.cloudflare.com/workers/ci-cd/builds/deploy-hooks/) 与 [Workers Builds 配置](https://developers.cloudflare.com/workers/ci-cd/builds/configuration/)。

`.github/workflows/website.yml` 只负责在正式版发布后或手动运行时触发 hook，并等待官网清单和安装脚本实际更新，最长等待 20 分钟；接受构建请求不等于部署完成。官网只展示正式版，因此 Nightly 和 Beta／RC 不再触发官网重建。main 的常规 push 由 Cloudflare 原生集成处理，Actions 不重复部署。可以单独手动运行 website workflow 重试同步，不需要重新发布二进制。未配置 hook 时，构建和 GitHub Release 仍可成功，但官网同步步骤会明确失败；main 的原生自动部署不受影响。

GitHub Releases 必须公开可下载。建议启用 immutable releases，并对正式版本 tag 设置创建权限与更新／删除保护。workflow 只在发布任务授予 `contents: write`，PR 检查不接触部署 secret。环境审批是否开启由仓库维护者决定。

PR、main push 和手动触发统一使用 `ci.yml`：通用检查运行一次；`native-build.yml` 覆盖 Linux AMD64／ARM64、macOS AMD64／ARM64 和 Windows AMD64。浏览器验证固定为 Linux AMD64（`ubuntu-24.04`）与 macOS ARM64（`macos-15`）。所有必需任务通过后，最终检查 `CI passed v2` 才成功，可将其配置为分支保护的必需检查。PR 的新提交可取消旧验证；main 不按分支取消旧 commit 的 CI。

首次执行真实流水线后，以 Actions 原生 runner、Cloudflare 构建历史及公开下载验证确认远端接通。本地测试不代表已完成 macOS／Windows 原生验收、签名、公证或线上发布。

官网测试按文件串行运行，使共用同一项目缓存的 Vite 服务和构建依次执行，避免并发依赖优化及浏览器负载导致原生 runner 超时。

CI 与发布使用同一个 `prepare-sandbox` action 准备 runner。Ubuntu 启用 AppArmor 非特权 user namespace 限制时，从 `apparmor-profiles` 包安装并加载 `bwrap-userns-restrict`，随后在 `NoNewPrivileges=yes` 的临时服务中运行 Bubblewrap 探针；失败时输出对应内核拒绝日志。该配置只作用于临时 runner，系统的 user namespace 限制保持启用。

### 官方 OSS 镜像

正式版发布完成后，`release-build.yml` 的 `mirror` 任务下载该 GitHub Release 已发布的文件，验证 Runtime、桌面包和源码包的 SHA-256，再使用固定版本且校验摘要的 ossutil 上传。Beta／RC 和 Nightly 不执行此任务。镜像失败会使 release workflow 失败；重新运行时，即使 GitHub Release 已存在，也会补传镜像，无需重新构建。

在 GitHub 的 `release` environment 或仓库中配置：

| 类型 | 名称 | 用途 |
| --- | --- | --- |
| Variable | `MOLR_OSS_BUCKET` | 目标 OSS bucket 名，不含 `oss://` 或目录 |
| Variable | `MOLR_OSS_REGION` | bucket 地域，例如 `cn-hangzhou` |
| Secret | `MOLR_OSS_ACCESS_KEY_ID` | 上传凭据 ID |
| Secret | `MOLR_OSS_ACCESS_KEY_SECRET` | 上传凭据密钥 |

凭据需要目标 bucket 的 `releases/*` 和 `install.sh` 对象读写权限（`oss:GetObject`、`oss:PutObject`）；不需要删除对象或管理 bucket 的权限。`cdn.molr.ai` 必须通过 HTTPS 提供这些对象，URL 路径与 OSS 对象 key 相同，不增加前缀、不重定向到 GitHub。Bucket 可通过 CDN 的回源授权提供读取，无需为上传任务修改公共 ACL。

- 包、同名 `.sha256` 和发布清单：`releases/download/v<版本>/<文件名>`。
- 历史版本清单：`releases/versions/<版本>.json` 与 `releases/versions/<版本>/download-<平台>.txt`。
- 正式渠道清单：`releases/stable/`；安装脚本：`install.sh`。

任务只上传明确列出的发布文件。确认包在 CDN 上可访问且大小一致后，才发布渠道清单；版本路径设置一年不可变缓存，渠道清单和脚本设置 `Cache-Control: no-cache`，CDN 应遵循这些响应头，不缓存缺失对象的 404。镜像任务串行执行，并读取 OSS 当前正式版清单，旧版本重跑只补齐版本路径，不回退最新渠道或脚本。

安装脚本和 Go 更新器对官方正式版包采用 GitHub 优先、OSS 回退，连接超时为 8 秒，下载持续 15 秒低于 32 KiB/s 时中止当前来源。Go 还对 TLS 握手和响应头设置 8 秒超时。换源重新下载并保持原清单摘要；校验失败直接停止。官网正式版清单或固定正式版清单请求失败时也可回退 OSS。非官方 URL 和预发布版本不自动改写；第三方浏览器组件下载不属于此镜像范围。手动使用镜像入口见[安装指南](https://molr.ai/zh/docs/admin/deployment/installation/)。

## CI 与构建复用

发布准备阶段固定 commit 和版本。`ensure-ci.mjs` 查询当前仓库 `ci.yml` 的 main push 或 main 手动验证，要求 `CI v2 <SHA>` 标题、最新 run 的最新 attempt 成功，以及 `CI passed v2` 中 `Validated <SHA>` 步骤成功。手动 CI 可通过 `commit` 输入验证 main 历史内的完整 SHA；其 workflow `head_sha` 可能不同，发布门禁以实际检出并汇总的 SHA 为准。旧版 CI、PR merge commit、其他仓库和不完整验证不能复用。

已有成功结果直接进入发行构建；进行中的 CI 最多等待 120 分钟；没有记录时通过 `workflow_dispatch` 补跑统一 CI。失败、取消、超时或 API 查询失败均停止发行，不退回任意历史成功记录。发布入口需要 `actions: write` 用于补跑 CI，成功记录的链接和 attempt 写入运行摘要。手动补验可运行 `gh workflow run ci.yml --ref main -f commit=<完整SHA>`；随后重试发布门禁即可。

`build-assets.yml` 每个版本只构建一次 WebUI 和桌面 JS／TS，记录 commit、版本、lockfile 摘要和完整输出文件摘要，归档供各平台下载。`MOLR_BUILD_ASSETS=1` 表示消费共享资源；身份、文件清单或摘要不一致立即失败，不能仅凭目录存在跳过构建。本地不设置该变量时，原有构建／打包命令仍自行构建所需前端资源。

`native-build.yml` 每个平台构建一次 Runtime。Unix 归档直接交给 `installer-acceptance.yml`；原生 payload 单独以 tar 和 SHA-256 保存，桌面打包恢复同一份 payload，不再重新编译 Runtime。签名作用于桌面包副本，不改动独立 Runtime 归档。Node/npm 与 Go 的依赖和编译缓存仅用于加速，不能代替 CI 成功记录或产物校验。

完整安装、修复、下载更新、保留数据重装及卸载集中到独立安装验收任务。`test-desktop-package.mjs` 校验最终安装包摘要，提取 DEB、挂载并复制 DMG 应用或静默安装 NSIS，启动实际打包的 Electron 应用，再对内嵌 Runtime 执行独立生命周期与沙盒验收；Windows 已签名包还验证安装器、应用和内嵌程序的 Authenticode。DEB 提取验证不等价于验证所有 dpkg 安装钩子。macOS 预发布／正式版在公证完成或 optional 等待结束后验收最终 DMG。

不同版本可并行构建；同 tag 和 Nightly 入口各自串行。相同 SHA 的 CI 门禁串行，避免同时补跑；实际发布 GitHub 附件的短任务使用全局发布锁。需要保留等待任务的锁设置 `queue: max`、`cancel-in-progress: false`。签名失败重跑桌面任务即可复用前端和 Runtime；公证失败重跑公证任务；上传失败重跑发布任务。中间 artifacts 保留 14 天，超过保留期需要重新构建；同版本已发布附件仍不可覆盖。

## 版本与渠道

| 类型 | Tag 示例 | 渠道 |
| --- | --- | --- |
| 正式版 | `v0.1.0` | stable；同版本正式版可进入 preview |
| Beta | `v0.2.0-beta.1` | preview |
| RC | `v0.2.0-rc.1` | preview |
| Nightly | `v0.2.0-0.nightly.20261011.30405.gabcdef123456` | nightly |

根 `package.json` 的 version 表示 main 当前开发周期的基础版本；进入下一周期时更新它及 lockfile 对应根版本。发布版本取自 tag，不要求修改内部 workspace 包版本。nightly 的日期、无前导零的 UTC 时分秒数字和 commit 由脚本生成，满足 SemVer；同基础版本顺序为 nightly、beta、rc、正式版。主分支不能使用 `-dev` 等版本作为基础版本。

正式版发布后，维护者将 main 基础版本提升到下一开发版本，例如 `0.1.0` 发布后改为 `0.1.1`；准备新功能周期时也可选择更高的 minor／major 版本。同步修改 `package-lock.json` 顶层及 `packages[""].version`，确认依赖未变化后更新许可证清单中的锁文件摘要。

GitHub 与 Gitea CI 的源码任务运行 `npm run check:base-version`：基础版本必须为纯 `X.Y.Z`、三个根版本字段一致，并严格高于 GitHub Releases 中按 SemVer 排序的最高正式版。检查遍历全部发布记录，仅读取元数据，忽略草稿、预发布和非正式版标签；没有正式版时允许首个开发周期。网络、权限、限流或响应格式错误均使检查失败，不作为“没有正式版”放行。本地可运行同一命令，使用 `MOLR_RELEASE_REPOSITORY=owner/repo` 指定 GitHub 仓库，默认 `molr-ai/molr`；`GH_TOKEN` 可提供只读访问凭据。该联网检查独立于 `npm run check`。

检查不修改文件或创建 PR；正式版发布后，在下一次 CI 执行时检查新的发布状态，已有成功结果不会自动失效。若基础版本未提升，错误会列出已发布版本、建议的下一补丁版本及需同步的文件。GitHub CI 查询当前仓库，Gitea 的发布源和可选访问凭据见 [Gitea CI](https://molr.ai/zh/docs/dev/contributing/gitea-ci/#网络与验证)。

正式版、beta 和 rc 的 tag 必须指向 main 历史内的 commit。推送 tag 触发 Release workflow；手动入口接受已有 tag，适合重试。Nightly 每日 UTC 19:17（北京时间次日 03:17）执行，也可手动运行。GitHub 调度可能延迟；相同版本已发布，或近期最高 nightly 版本对应同一 commit 时跳过重复构建，正式版已发布时仍可重试官网同步；Nightly 不触发官网部署。

## 发布步骤与失败恢复

两种入口共用 `release-build.yml`：固定 commit → 复用或补跑完整 CI → 共享前端构建 → 原生 Runtime 构建 → 安装验收与桌面打包／签名／公证／产物验收 → 汇总产物 → 创建草稿 → 上传并核对附件 → 发布 → 验证公开附件 → 正式版刷新官网。

构建要求干净检出、仓库指定 Go 版本与 `CGO_ENABLED=0`，检查 Linux 静态 ELF 或 macOS Mach-O/有效代码签名、架构、VCS commit 和 `vcs.modified=false`。构建时间取 commit 时间，tar 的时间、用户和组信息固定，便于同 commit 重试。四个平台必须全部成功；Linux 要求隔离沙盒，macOS 按无隔离能力验收，不能以编译成功替代安装验收。`0.1.0-local` 等本地构建允许未提交修改，不进入正式发布清单。

发布附件包含两个架构包及 SHA-256、经过源码发布检查的源码包及 SHA-256、`manifest.json`。`install.sh` 由官网统一提供，用于下载、校验并调用包内安装器；Release 不再附带脚本或在 manifest 中记录脚本 URL、摘要。手动安装直接校验并解压归档，运行 `./bin/molr-setup`。版本和附件保持不可变。已有同名附件时只接受摘要相同的内容，不覆盖；已完成发布的同 commit 版本直接跳过构建。创建草稿直接使用 GitHub API 返回的 release ID，附件上传和发布均按该 ID 操作，不依赖刚创建的草稿立即出现在列表中。正式版和预发布版创建前验证远端 tag 已存在；nightly 允许发布时创建 tag。草稿上传失败可重新运行原工作流的失败任务，复用构建 artifacts；重新完整运行会校验已存在附件，存在差异时需要检查原因或使用新版本。RC 转正式版使用新的正式版 tag 和构建。

`release-catalog.mjs` 的命令行入口与官网构建默认只检查 GitHub 列表前 100 条 Release 元数据。在下载附件前排除草稿、`prerelease` 不为 `false` 的记录、beta／rc／nightly 版本、没有 manifest 附件或版本号不符合协议的条目，只选择最高正式版并校验其 Runtime 和桌面清单。未选中的历史及预发布清单不参与校验；正式版清单损坏仍会使构建失败，不静默回退。没有正式版时输出两个空 catalog，官网正常构建并显示“即将推出”。目录结构保留三个渠道键：preview 可指向同一个正式版，nightly 为空。发布任务调用的共享收集函数仍默认按三类版本筛选，保留 nightly 去重与发布重试行为。

用正整数环境变量 `MOLR_RELEASE_SCAN_LIMIT` 调整检查条数 N；每页最多 100 条，最多请求 `ceil(N / 100)` 页，最后一页超出 N 的条目不参与筛选。Cloudflare 构建变量和 GitHub 仓库同名变量应保持一致，website workflow 默认使用 100。扫描范围内没有正式版时不再搜索更早记录。发布重试按确切 tag 查询，不受官网窗口限制。

Manifest 的顶层、平台条目及 requirements 必须包含规定字段，并通过字段值校验；额外字段不影响校验，原样保留在版本快照中。

官网由 Cloudflare 从 main 构建，每次构建生成所选版本的发行目录并部署静态资源，包括 `/install.sh`、`/releases/catalog.json`、各渠道 manifest 和 Runtime 使用的 `latest-<平台>.json`、所选版本的 `/releases/versions/<版本>.json`。官网不再保存所有历史版本的清单；未入选版本从 GitHub Releases 下载归档和校验文件后离线安装。上述入口使用 `Cache-Control: no-cache`。安装包仍从 GitHub 下载，不上传到 Worker。部署失败保留此前整个站点和渠道目录；检查 Cloudflare 构建日志，修复构建配置、hook 或网络后单独重跑 website workflow。

官网首屏和底部桌面下载只展示正式版安装包；后端与沙盒安装命令固定使用 `https://molr.ai/install.sh`，默认安装 stable 最新版，自动检测系统与架构。没有正式版时显示“即将推出”，不回退到 preview 或 nightly。页面不提供渠道切换；自定义发布源、渠道与指定版本的用法见安装文档。

## 平台维护

平台清单包含系统、架构、架构别名、可用渠道、原生 runner、归档格式、安装器、最低环境要求和验收类型。当前实现接受 Linux/macOS `tar.gz` + POSIX sh / Go 安装器。Linux 验证静态 ELF 与隔离沙盒，macOS 验证 Mach-O、ad-hoc 签名、launchd 和显式 Full Access；macOS 最低版本为 13。新增操作系统必须先实现安装器和验收流程，不能只新增一行配置宣布支持。

调整清单后运行：

```bash
npm run release:platforms
npm run check:platforms
```

生成文件供 Runtime 与包内安装器读取；引导脚本内的离线平台识别表同样生成。CI 校验生成结果，构建矩阵、官网实际可下载平台及依赖要求使用同一份定义。可先只给新平台开放 nightly，原生安装与沙盒验收稳定后再开放 preview／stable。

每个版本的 manifest 保存当时的平台及环境要求快照。官网按所选渠道版本的实际快照展示，不根据当前支持列表给历史版本补出下载链接。停止某个平台的新版本构建时保留旧 release 和安装包，用户仍可按旧版说明离线安装。

## 本地验证

```bash
npm ci
npm run check:platforms
npm run test:release-flow
npm run test:release
python3 scripts/test-install-download.py
cd runtime
CGO_ENABLED=0 go test ./internal/installation ./internal/updates
cd ..
npm run check:release
npm run check:licenses
npm run build:release
MOLR_TEST_ARCHIVE=dist/releases/molr_0.1.0-local_linux_amd64.tar.gz npm run test:release-install
```

原生安装测试需要当前用户的 systemd manager、cgroup v2 委派、静态 BusyBox 与 Bubblewrap；测试使用独立实例、配置和数据，完成后清理。源码导出和完整发行构建仍遵守[公开文件检查](https://molr.ai/zh/docs/dev/releasing/public-content/)。

准备本地目录可将已验证的 release manifests 放在专用目录后执行 `npm run release:promote -- --manifests <目录>`。命令只生成 `.cache/release-catalog/`，不上传文件。使用 `MOLR_CATALOG_DIR=.cache/release-catalog VITE_SITE_MODE=live npm run dev:website` 验收官网；未配置 catalog 时显示各渠道暂无版本，不伪造下载入口。浏览器回归使用人工 manifest 拦截网络，真实包验收单独执行。详细验收日期、环境和结果保存在本地内部记录中。

安装器先检查目标路径，组／其他用户可写的目录由用户选择修复或忽略；符号链接、所有者和受管理文件检查保持独立。安装依赖后以服务账户及 `NoNewPrivileges` 探测 Bubblewrap，按授权准备发行版 AppArmor 策略并复测，随后才写入实例和重启服务。策略分支使用人工文件与模拟系统命令测试；原生安装验收验证实际沙盒能力。

## 安装器维护与验收

发行归档包含两个静态 Go 程序：`runtime/cmd/molr` 启动 Runtime，`runtime/cmd/molr-setup` 管理安装、更新与卸载。安装流程位于 `runtime/internal/installation`；`host_linux.go` 集中依赖、systemd、AppArmor 与主机能力检测，`files_unix.go` 负责路径、权限、原子写入和锁，系统专用文件负责可执行文件与删除保护；macOS 使用 `service_darwin.go` 管理 launchd：注册后通过不带 `-k` 的 `kickstart` 显式启动，避免仅按需启动的登录域挂起 `RunAtLoad`；已登记实例沿用原域，不重复注册，恢复服务也走同一启动流程。公共流程不根据 Ubuntu 名称或版本判断支持；当前自动包管理适配仅实现 APT，CI 覆盖 Ubuntu 24.04 和 macOS 的 AMD64／ARM64。

安装包用 `release.json` 描述构建；程序目录的 `installation.json` 是实例状态、目录、账户、服务、构建与更新订阅的唯一记录。默认行为按可执行文件所在目录判断，不使用工作目录。安装记录损坏时停止，不退回包模式。`.setup.lock` 使用内核 advisory lock，文件存在不表示操作运行；保留 inode，不在释放时删除锁文件。

计划阶段读取并检查环境；交互和静默模式共用计划与执行代码。确认和目录策略在写入前完成，`--dry-run` 不执行变更。终端输入来自 `/dev/tty`，因此管道启动仍可交互。静默模式使用 `sudo -n`，APT 禁用交互并保留原配置。程序以临时文件和原子重命名更新，保留上版二进制与配置；中断可能留下 `installing` 状态，需要完整包重新执行。验证失败不会被报告为健康安装。

Linux 用户服务在成功启动后通过 `loginctl --no-ask-password` 检查并尝试开启 linger，成功提示以再次读取 `Linger=yes` 为准；命令共用十秒超时，不触发交互授权或 sudo。失败时保留服务安装并输出登出风险、管理员开启命令及验证命令。系统服务、`--dry-run`、`--no-start` 不改变 linger；卸载保留账号级设置。`service_linux_test.go` 覆盖已开启、自动开启、权限不足、命令不可用、超时、验证失败及跳过分支。

Linux/macOS 安装及恢复完成后，`commands.go` 在用户的 `~/.local/bin` 或 Linux 系统的 `/usr/local/bin` 创建指向实例程序的软链接。两个通用命令一起检查冲突，不替换已有文件或其他实例链接；升级保留目标路径，卸载仅清理仍指向本实例的链接。目录操作使用固定文件描述符且不跟随父目录软链接。用户 PATH 缺失时按 Shell 补充启动配置，保留内容与权限，拒绝软链接、硬链接及不安全权限；共享 PATH 配置在卸载后保留。命令入口配置失败只报告完整路径，不回滚已完成的服务安装。`commands_test.go` 覆盖冲突、重复安装、目标替换、Shell 配置、卸载归属与通过软链接识别实例。

`install.sh` 是 POSIX 下载器，使用已有 curl 或 wget。渠道目录额外生成 `download-<平台>.txt`，固定七行依次为协议标识、版本、commit、平台 ID、文件名、URL、SHA-256；同一 manifest 生成渠道与历史版本描述，避免 shell 解析 JSON 或执行远端文本。下载器逐个向固定路径提取五个白名单文件。Go 更新器读取 JSON manifest，通过标准库 HTTP、SHA-256 和 tar/gzip 验证下载，随后执行新包的 `molr-setup`。更新订阅由安装记录供 Runtime 和 WebUI 使用；WebUI 生成已安装维护程序的更新命令。

扩展发行版时，在主机适配模块增加包管理或服务实现，并向独立验收矩阵增加系统／版本／架构组合。已经手动满足能力要求的未知发行版不会被拒绝。增加矩阵项前须有能运行真实服务、cgroup 与沙盒探针的环境；容器内文件测试不能代替这部分验收。

`.github/workflows/installer-acceptance.yml` 复用实际构建归档，在独立 runner 移除 Bubblewrap、静态 BusyBox 和 AppArmor profiles，由安装器恢复依赖。同一任务覆盖首次准备依赖和后续已有依赖的安装／更新场景，构建任务不再重复整套安装验收。矩阵还包含 macOS ARM64／AMD64，直接验证用户级 launchd，无需安装 Linux 沙盒依赖；其他 Linux 发行版尚未声明通过。

PTY 驱动 `scripts/test-install-interactive.py` 只应答已知提示，资源预设提示按回车验收默认“个人”。修改安装提示时同步更新驱动；`python3 scripts/test-install-interactive-driver.py` 从安装器源码读取当前提示，覆盖分块输出、默认选择、失败退出码、退出后的残留子进程清理和未知提示超时，GitHub/Gitea 源码 CI 与各平台原生安装任务均执行。交互过程最多等待十分钟；清理先回收已退出的子进程并保留退出码，再发送 SIGTERM，最多等待两秒，最后以 SIGKILL 回收 PTY 进程组。macOS 对仅含僵尸进程的组可能返回 EPERM，此时仅在确认并回收刚退出的直接子进程后重试信号，其他权限错误继续报错。

回归覆盖权限选择、无变更预览、重复运行、安装记录校验、并发锁、路径与归档攻击、下载失败、数据保留及清除。原生包测试覆盖 PTY 交互安装、静默修复、配置保留、认证沙盒检查和卸载。Python 仅作为开发测试中的 PTY 驱动及发布检查工具，不进入安装包或目标机安装依赖。

```bash
cd runtime
CGO_ENABLED=0 go test ./internal/installation ./internal/updates
cd ..
python3 scripts/test-install-download.py
MOLR_TEST_ARCHIVE=dist/releases/molr_0.1.0-local_linux_amd64.tar.gz npm run test:release-install
```

完整依赖准备在一次性 Ubuntu 主机设置 `MOLR_TEST_PREPARE=1` 复现；不要移除开发主机依赖。安装故障先增加回归用例，修复随新包发布。日期、环境、日志和验收凭据保存在本地内部资料中。

macOS 发行包在对应架构的 Mac runner 构建，使用 `codesign --sign -` 后验证签名，再生成归档与摘要。`scripts/package-release.py` 在各系统生成相同规则的归档，保留 LICENSE 和 THIRD_PARTY_NOTICES。原生验收覆盖安装、内嵌页面、修复、下载更新、保留数据重装和清除。此流程不需要 Apple Developer ID 或公证凭据。

## 实例备份格式

`molr-setup backup / restore` 的归档和数据逻辑位于 `runtime/internal/instancebackup`，服务及安装事务由 `installation` 和 `installationwindows` 管理。格式版本 1 使用 tar.gz，首项为 `backup.json`；清单保存模式、构建身份、数据库 schema、受管路径及每项的类型、权限、大小和 SHA-256。内容分为 `program/`、`config/`、`data/` 和可选 `work/`。配置引用使用固定字段清单，不递归收集任意配置值指向的宿主路径。

读取时校验归档路径、重复项、父目录类型、文件大小、摘要和 gzip 完整性；符号链接最后创建且只允许出现在受管用户文件中。解包和数据库检查发生在目标停服前。Runtime 备份保留加密密钥，恢复撤销旧会话／令牌；Sandbox 数据库保持操作回执和额度状态。跨平台选择目标发行程序并转换沙盒配置，不执行归档中的异平台二进制。

验证入口为 `go test ./internal/instancebackup ./internal/installation ./cmd/molr`。Windows 服务控制需原生运行 `installationwindows` 测试；交叉编译只证明可构建，不代替原生迁移验收。操作范围与命令见[备份恢复](https://molr.ai/zh/docs/admin/deployment/maintenance/#备份恢复与升级)。
