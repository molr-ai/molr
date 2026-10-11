# 发行维护

发行包存放在 GitHub Releases，官网与发布清单通过 Cloudflare Workers 静态资源一起部署。当前产物是内嵌 WebUI 的 Linux Runtime；平台与环境要求统一定义在 [`release/platforms.json`](../../release/platforms.json)。

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

平台安装 npm 依赖后执行构建命令。它读取公开 GitHub Releases，验证下载链接，生成独立临时渠道目录，再构建官网。无需预装 `gh` 或手工提供 Cloudflare 部署 token；部署认证由 Workers Builds 管理。仓库默认 `molr-ai/molr`，fork 可设置 Cloudflare 构建变量 `MOLR_RELEASE_REPOSITORY`；公开发行信息默认匿名读取，如果遇到 GitHub API 限流，可在 Cloudflare 构建 secrets 中提供只读 `GH_TOKEN`。

默认发布源是 `https://molr.ai/releases`，使用其他域名时，在 Cloudflare 构建变量与 GitHub 仓库变量中同步设置 `MOLR_RELEASE_URL`，指向同一 Worker 的 `/releases` 路径。按现有 [`wrangler.jsonc`](../../wrangler.jsonc) 将 `molr` Worker 绑定官网域名。

发布 GitHub Release 不会修改 main。为让 release／nightly 完成后也刷新官网，在 Cloudflare Worker → Settings → Builds → Deploy Hooks 创建绑定 `main` 的 hook，将 URL 保存为 GitHub 仓库 secret `CLOUDFLARE_DEPLOY_HOOK`。该 URL 自身具有触发构建的权限，不写入仓库或日志；无需额外 Authorization header。配置入口和原理见 [Cloudflare Deploy Hooks](https://developers.cloudflare.com/workers/ci-cd/builds/deploy-hooks/) 与 [Workers Builds 配置](https://developers.cloudflare.com/workers/ci-cd/builds/configuration/)。

`.github/workflows/website.yml` 只负责在发布后触发 hook，并等待官网清单和安装脚本实际更新，最长等待 20 分钟；接受构建请求不等于部署完成。main 的常规 push 由 Cloudflare 原生集成处理，Actions 不重复部署。可以单独手动运行 website workflow 重试同步，不需要重新发布二进制。未配置 hook 时，构建和 GitHub Release 仍可成功，但官网同步步骤会明确失败；main 的原生自动部署不受影响。

GitHub Releases 必须公开可下载。建议启用 immutable releases，并对正式版本 tag 设置创建权限与更新／删除保护。workflow 只在发布任务授予 `contents: write`，PR 检查不接触部署 secret。环境审批是否开启由仓库维护者决定。

启用前手动运行 CI，检查两个平台的 runner 可用；当前使用 `ubuntu-24.04` 和 `ubuntu-24.04-arm`。首次执行实际发布与 Cloudflare 部署后，以 Cloudflare 构建历史和 workflow 的公开下载、站点验证结果确认远端接通。本地测试不代表已经配置远端账户或执行过线上发布。

## 版本与渠道

| 类型 | Tag 示例 | 渠道 |
| --- | --- | --- |
| 正式版 | `v0.1.0` | stable；同版本正式版可进入 preview |
| Beta | `v0.2.0-beta.1` | preview |
| RC | `v0.2.0-rc.1` | preview |
| Nightly | `v0.2.0-0.nightly.20261011.30405.gabcdef123456` | nightly |

根 `package.json` 的 version 表示 main 当前开发周期的基础版本；进入下一周期时更新它及 lockfile 对应根版本。发布版本取自 tag，不要求修改内部 workspace 包版本。nightly 的日期、无前导零的 UTC 时分秒数字和 commit 由脚本生成，满足 SemVer；同基础版本顺序为 nightly、beta、rc、正式版。主分支不能使用 `-dev` 等版本作为基础版本。

正式版、beta 和 rc 的 tag 必须指向 main 历史内的 commit。推送 tag 触发 Release workflow；手动入口接受已有 tag，适合重试。Nightly 每日 UTC 19:17（北京时间次日 03:17）执行，也可手动运行。GitHub 调度可能延迟；相同 commit 已成功发布 nightly 时跳过重复构建，但仍可修复官网部署。

## 发布步骤与失败恢复

两种入口共用 `release-build.yml`：固定 commit → 基础检查和测试 → 平台矩阵静态构建 → 原生安装、重装、认证和沙盒验收 → 汇总产物 → 创建草稿 → 上传并核对附件 → 发布 → 验证公开附件 → 部署官网。

构建要求干净检出、仓库指定 Go 版本与 `CGO_ENABLED=0`，检查实际静态链接、架构、VCS commit 和 `vcs.modified=false`。构建时间取 commit 时间，tar 的时间、用户和组信息固定，便于同 commit 重试。两个架构必须全部成功；缺少沙盒能力的 runner 会失败，不降低验收要求。`0.1.0-local` 等本地构建允许未提交修改，不进入正式发布清单。

发布附件包含两个架构包及 SHA-256、经过源码发布检查的源码包、安装脚本快照及 SHA-256、`manifest.json`。版本和附件保持不可变。已有同名附件时只接受摘要相同的内容，不覆盖；已完成发布的同 commit 版本直接跳过构建。草稿上传失败可重新运行原工作流的失败任务，复用构建 artifacts；重新完整运行会校验已存在附件，存在差异时需要检查原因或使用新版本。RC 转正式版使用新的正式版 tag 和构建。

`release-catalog.mjs` 从全部已发布、带有效 manifest 的 GitHub Releases 重建目录，忽略草稿。按 SemVer 选择 stable、preview、nightly；发布完成顺序不会让旧版本覆盖新版本。正式版可被 preview 订阅者发现，nightly 不能进入其他渠道。

官网由 Cloudflare 从 main 构建，每次构建重新收集完整发行目录并部署静态资源，包括 `/install.sh`、`/releases/catalog.json`、各渠道 manifest 和 Runtime 使用的 `latest-<平台>.json`、历史 `/releases/versions/<版本>.json`。这些可变入口使用 `Cache-Control: no-cache`。安装包仍从 GitHub 下载，不上传到 Worker。部署失败保留此前整个站点和渠道目录；检查 Cloudflare 构建日志，修复构建配置、hook 或网络后单独重跑 website workflow。

## 平台维护

平台清单包含系统、架构、架构别名、可用渠道、原生 runner、归档格式、安装器、最低环境要求和验收类型。当前实现接受 Linux `tar.gz` + Bash/Python 安装器。新增操作系统必须先实现安装器和验收流程，不能只新增一行配置宣布支持。

调整清单后运行：

```bash
npm run release:platforms
npm run check:platforms
```

生成文件供 Runtime 与包内安装器读取；引导脚本内的离线平台识别表同样生成。CI 校验生成结果，构建矩阵、官网实际可下载平台及依赖要求使用同一份定义。可先只给新平台开放 nightly，原生安装与沙盒验收稳定后再开放 preview／stable。

每个版本的 manifest 保存当时的平台及环境要求快照。官网按所选渠道版本的实际快照展示，不根据当前支持列表给历史版本补出下载链接。停止某个平台的新版本构建时保留旧 release 和脚本快照，用户仍可按旧版说明离线安装。

## 本地验证

```bash
npm ci
npm run check:platforms
npm run test:release-flow
npm run test:release
python3 scripts/test-install-download.py
python3 runtime/scripts/test-install-linux.py
python3 runtime/scripts/test-uninstall-linux.py
npm run check:release
npm run check:licenses
npm run build:release
MOLR_TEST_ARCHIVE=dist/releases/molr_0.1.0-local_linux_amd64.tar.gz npm run test:release-install
```

原生安装测试需要当前用户的 systemd manager、cgroup v2 委派、静态 BusyBox 与 Bubblewrap；测试使用独立实例、配置和数据，完成后清理。源码导出和完整发行构建仍遵守[公开文件检查](public-content.md)。

准备本地目录可将已验证的 release manifests 放在专用目录后执行 `npm run release:promote -- --manifests <目录>`。命令只生成 `.cache/release-catalog/`，不上传文件。使用 `MOLR_CATALOG_DIR=.cache/release-catalog VITE_SITE_MODE=live npm run dev:website` 验收官网；未配置 catalog 时显示各渠道暂无版本，不伪造下载入口。浏览器回归使用人工 manifest 拦截网络，真实包验收单独执行。详细验收日期、环境和结果保存在本地内部记录中。
