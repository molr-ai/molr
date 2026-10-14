# 源码构建与部署

## 构建

使用 [runtime/.go-version](../../../runtime/.go-version) 指定的 Go 版本，以及根目录 [package.json](../../../package.json) 要求的 Node.js 版本。以下命令在自己的部署检出目录执行；仓库开发任务遵守 [worktree 约定](../../../AGENTS.md)。

```bash
mkdir -p .cache/build
cd runtime
CGO_ENABLED=0 GOTOOLCHAIN="go$(cat .go-version)" go build -o ../.cache/build/molr ./cmd/molr
chmod 0755 ../.cache/build/molr
file ../.cache/build/molr
go version -m ../.cache/build/molr
cd ..
npm ci
npm run build:webui
```

确认 Runtime 为静态链接，构建信息包含 `CGO_ENABLED=0`，最终文件权限为 `0755`。部署主服务时还需核对目标提交及 `vcs.modified=false`。WebUI 产物位于 `apps/webui/dist/`，由静态服务器提供；不要将 Vite 开发服务当作公网生产入口。

构建后依次完成[配置与初始化](configuration.md#配置与初始化)、[服务安装](installation.md#linux-服务与命令环境)和[启动后的检查](installation.md#启动后的检查)。

## 构建发行包

本地构建需要 Node.js、仓库指定版本的 Go、Python 3 和系统 tar：

```bash
npm ci
npm run build:release
```

构建先检查待发布源码和公开静态文件，再生成 WebUI，随后以 `-trimpath` 静态编译带 `webui` build tag 的 Runtime，将页面与 API 放在同一服务。产物在 `dist/releases/`，包含 `molr_<版本>_linux_<架构>.tar.gz`、同名 `.sha256` ；推荐版本清单按[发行维护](../../dev/releasing/releases.md)单独生成。包内包含 Runtime、安装器、`LICENSE` 和完整的 `THIRD_PARTY_NOTICES.md`，不附带 BusyBox。默认版本为 `0.1.0-local`；`MOLR_VERSION`、`MOLR_ARCH`、`MOLR_DIST_DIR` 可覆盖。跨架构构建不需要准备目标架构 BusyBox；安装目标机通过系统包管理器提供静态 BusyBox。`GO` 可指定 Go 程序路径。`molr version` 显示构建版本与提交。归档只收集 `scripts/release-policy.json` 中明确列出的文件，验证通过后才生成最终归档和下载清单；不要分发构建失败后留下的目录，也不要直接压缩整个工作目录。

如需导出源码包，在已提交并审查过的版本上执行 `npm run build:source -- --ref HEAD`。该命令只导出指定 Git 提交，工作目录中的修改、未提交新文件、`.cache/` 和运行数据不会被打包。详细发布范围及检查限制见[开源发布与私有文件隔离](../../dev/releasing/public-content.md)。
