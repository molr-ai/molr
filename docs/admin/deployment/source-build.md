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

Linux 产物须为静态 ELF；macOS 产物为 Mach-O，使用系统运行库，不要求 ELF 静态链接。两者的构建信息均须包含 `CGO_ENABLED=0`，最终文件权限为 `0755`。部署主服务时还需核对目标提交及 `vcs.modified=false`。WebUI 产物位于 `apps/webui/dist/`，由静态服务器提供；不要将 Vite 开发服务当作公网生产入口。

构建后依次完成[配置与初始化](configuration.md#配置与初始化)、[服务安装](installation.md#linux-服务与命令环境)和[启动后的检查](installation.md#启动后的检查)。

## macOS 原生运行

Runtime 支持 macOS ARM64 和 AMD64。工作区、文件工具、附件和记忆不依赖命令执行；`sandbox.enabled: false` 只关闭本机命令执行。受控命令使用 Seatbelt，能力和部署配置见 [macOS 原生命令权限](../sandbox/macos.md)。安装、更新和卸载支持 macOS，见[安装指南](installation.md#macos-命令行安装)；应用内升级暂不可用。

按上节构建后，准备独立配置，替换所有绝对路径。最小本机配置示例：

```json
{
  "data_dir": "/Users/example/Library/Application Support/Molr/data",
  "http": {
    "listen": "127.0.0.1:8080",
    "public_url": "http://127.0.0.1:8080",
    "tls_mode": "local"
  },
  "sandbox": { "enabled": false }
}
```

在仓库根目录初始化并启动：

```bash
.cache/build/molr bootstrap -config /absolute/path/server.json
.cache/build/molr serve -config /absolute/path/server.json
```

前端开发联调见 [WebUI](../../../apps/webui/README.md)。需要同一服务提供构建后的页面时，将 `apps/webui/dist/` 的内容复制到 `runtime/internal/webassets/dist/`，并使用 `go build -tags webui` 重新构建。

需要受控命令时设置 `sandbox.enabled: true` 并授予助理计算权限，网络和外部文件另外授权。启用 `sandbox.allow_full_access: true` 后可独立授予 Full Access。两种模式使用同一个原生工作区；脚本使用实际路径或 `$MOLR_WORKSPACE`、`$MOLR_GLOBAL`、`$TMPDIR`，不会改写脚本文本。

长期运行可使用管理员自行管理的 launchd 服务：`ProgramArguments` 指向构建产物、`serve`、`-config` 和配置绝对路径；显式设置 `WorkingDirectory`、日志路径和 `PATH`，因为 launchd 不读取交互式 shell 配置。服务账户必须能够读写自己的配置和数据目录。更新时停止本实例、原子替换二进制后重启，保留配置、数据库和密钥。

启动后认证检查 `/api/sandbox`：关闭命令时要求 `Enabled=false`、`Storage=true`；受控执行就绪时要求 `Reason=available`、`Shell`、`FileIsolation`、`NetworkIsolation` 为 true，`NetworkBackend=seatbelt-toggle`。`ProcessIsolation` 与 `ResourceLimits` 为 false。另行开启 Full Access 时检查 `Capabilities.FullAccess=true`。文件、终端和撤权须在 Mac 原生验证；远程执行节点继续使用 Linux。

## 构建发行包

本地构建需要 Node.js、仓库指定版本的 Go、Python 3 和系统 tar：

```bash
npm ci
npm run build:release
```

构建先检查待发布源码和公开静态文件，再生成 WebUI，随后以 `-trimpath` 静态编译带 `webui` build tag 的 Runtime，将页面与 API 放在同一服务。产物在 `dist/releases/`，包含 `molr_<版本>_linux_<架构>.tar.gz`、同名 `.sha256` ；推荐版本清单按[发行维护](../../dev/releasing/releases.md)单独生成。包内包含 Runtime、安装器、`LICENSE` 和完整的 `THIRD_PARTY_NOTICES.md`，不附带 BusyBox。默认版本为 `0.1.0-local`；`MOLR_VERSION`、`MOLR_ARCH`、`MOLR_DIST_DIR` 可覆盖。跨架构构建不需要准备目标架构 BusyBox；安装目标机通过系统包管理器提供静态 BusyBox。`GO` 可指定 Go 程序路径。`molr version` 显示构建版本与提交。归档只收集 `scripts/release-policy.json` 中明确列出的文件，验证通过后才生成最终归档和下载清单；不要分发构建失败后留下的目录，也不要直接压缩整个工作目录。

如需导出源码包，在已提交并审查过的版本上执行 `npm run build:source -- --ref HEAD`。该命令只导出指定 Git 提交，工作目录中的修改、未提交新文件、`.cache/` 和运行数据不会被打包。详细发布范围及检查限制见[开源发布与私有文件隔离](../../dev/releasing/public-content.md)。
