# MIT 许可证与第三方分发

依赖许可审查日期：2026-10-16；工作区清单核对日期：2026-10-16。本文说明当前 MIT 授权范围、第三方兼容性、通知交付及不附带 BusyBox 的发行流程。

## 授权范围与目录约定

Molr 自有代码、文档和原创资源采用根目录 [MIT License](../../../LICENSE)。第三方代码、字体和品牌标志保留各自许可，不被重新授权为 MIT。根目录和七个 npm 工作区的 `license` 字段只描述 Molr 自有内容。

**不建立第三方源码目录，也不使用 Git submodule。** 完整的原始许可证、版权声明、必要 NOTICE 和对应源码入口集中放在 [THIRD_PARTY_NOTICES.md](../../../THIRD_PARTY_NOTICES.md)。该文件适用于当前前端与 Linux/macOS Runtime 分发内容的并集；列出某个组件不代表每个产物都包含它。

`package-lock.json` 和 `runtime/go.mod` 继续管理依赖版本。submodule 不能替代给二进制用户交付许可证；依赖上游的许可证也不因使用 npm/Go 包管理器而自动出现在最终产物中。

## 桌面框架通知

Electron 作为框架二进制分发。桌面包在 `resources/licenses` 中保留官方发行的 `LICENSE` 和 `LICENSES.chromium.html`，同时交付 Molr 汇总声明。后者包含 Chromium、Node.js 等组件的原始通知与源码引用，不能用 Electron 的 MIT 声明代替。这些平台相关的框架通知保留上游格式，不嵌入每个服务器 Runtime；打包脚本检查两份文件存在并显式复制。Electron 自身采用 MIT，完整正文也收录在汇总声明中。electron-builder 及其构建依赖不进入 app.asar；生成安装包保留其安装器与框架所需声明。缺少独立许可证文件的仅构建依赖，在清单中注明按包内许可声明和上游仓库核对。

## 兼容性核对

在当前独立应用、库调用和外部进程的使用方式下，未发现要求 Molr 自有代码改用其他许可证的依赖。各第三方部分的原始条件仍须遵守。

| 组件 | 当前许可和处理 |
| --- | --- |
| 普通 Go/npm 代码依赖 | MIT、BSD、ISC、Apache 等原始文本与版权收录在汇总文件；静态链接不免除通知义务 |
| `gopkg.in/yaml.v3` | MIT **AND** Apache-2.0，按文件分别适用；同时保留 NOTICE |
| `modernc.org/libc` / `sqlite` / `memory` | 除顶层 BSD 外，保留 musl、Go、go-netdb、Nixpkgs、公共领域 SQLite、MIT sqlite-vec 和 mmap 等附加声明；未使用模块的 logo 素材 |
| DOMPurify | 从双许可中选择 Apache-2.0；原始文件保留上游提供的条款 |
| elkjs 0.9.3 | 本体与 ELK 部分为 EPL-2.0；EMF-GWT 部分为 EPL-1.0；GWT/Guava 等另有 Apache 等许可。汇总原始声明并给出对应版本源码和构建入口 |
| elkjs 对应 ELK 来源 | 官方 0.9.3 发布说明明确基于 ELK 0.9.1，另包含 ELK #955 补丁；汇总文件同时提供标签、补丁和发布说明，不能把 `elkjs 0.9.3` 当成不存在的 `ELK 0.9.3` 标签 |
| DM Sans / KaTeX 字体 | 均保留 OFL-1.1；KaTeX 字体的原始版权和 Reserved Font Names 取自包内字体的 name 表，不能用 KaTeX JavaScript 的 MIT 代替 |
| Lucide | 保留 ISC，以及 Feather 衍生图标的 MIT 部分 |
| khroma | manifest 未填许可，但原始 `license` 文件明确是 MIT |
| remark-math / rehype-katex | npm 包没有附许可正文，补录同上游项目原始 MIT 与 Junyoung Choi 版权 |
| SILK / WASI SDK 34.0 | 保留 Skype SILK、wasi-libc 各选项、musl/cloudlibc/musl-fts/dlmalloc、LLVM compiler-rt 等完整声明。SILK 条款明确不授予专利权 |
| resvg / Lightning CSS 等构建工具 | 工具本身不进入发行包，使用工具不使生成产物改用 MPL。若以后分发工具或开发容器，需重新清点该产物 |
| GitHub 标志 | 用于官网 GitHub 项目链接，属于官方允许的常见用途；保留品牌授权例外，不暗示背书。现有链接指向 `https://github.com/molr-ai/molr`，不把它当成 Molr MIT 资源或开源阻碍 |
| BusyBox / Bubblewrap / 可选宿主工具 | 当前发行包不携带这些二进制；由用户系统提供，安装器可按授权通过 apt 安装。以后分发含这些程序的容器/rootfs，需要单独履行对应源码等义务 |

桌面构建新增 Electron 44.8.0、electron-builder 26.15.3 及其仅用于构建的 npm 依赖，未新增 Go 模块。Go 工具链更新至 1.27.2，与 1.27.1 的根 LICENSE 正文相同；四个平台目标的声明生成结果未增加或更改依赖许可证正文。macOS 系统运行库由操作系统提供，不复制进 Molr 产物。

飞书渠道新增官方 Go SDK v3.12.2（MIT）、Gorilla WebSocket v1.5.0（BSD-2-Clause）和 GoGo protobuf v1.3.2（BSD-3-Clause）。已核对原始许可证，完整声明进入汇总文件；当前清单包含 25 个 Go 模块。

## 清单与维护

[license-inventory.json](license-inventory.json) 记录 603 条 npm 第三方安装位置记录和 25 个 Go 模块，覆盖直接、传递、开发及平台可选依赖；七个工作区链接不计入第三方记录。Linux x64 实际安装 567 个位置；其他 36 个平台可选构建工具按同版本包族及锁文件核对，未逐个检验其二进制。当前 Linux amd64 Runtime 引用其中 23 个 Go 模块；通知收集遍历 Linux/macOS 的 amd64/arm64 实际依赖包目录，保留更具体的嵌套许可证。

更新方式：

```bash
npm ci
# GO 可以指定独立 Go 工具链路径，版本仍由 runtime/.go-version 决定。
npm run notices:update
npm run notices:check
npm run check:licenses
```

`notices:update` 从安装且与锁文件一致的 npm 依赖、Go 模块和指定 Go 工具链收集原始文本，更新汇总文件中的生成区；补充区保留在同一个文件内。Vite/Rolldown 通知也收录，以覆盖它们生成的运行时辅助代码，工具可执行文件不随产品分发。未找到原始许可文本的包会报错；已核对的 remark-math 两个包通过补充区提供正文。

依赖升级时必须先审查许可变化、附加组件和对应源码，再重新生成正文、更新补充区及清单输入摘要。不能只刷新摘要跳过审查。`notices:check` 比较原始依赖文本与已提交生成区；`check:licenses` 检查 MIT 元数据及审查输入/完整声明摘要。后者集成在发行构建入口，防止未审查输入直接打包。这些检查不自动作出法律判断。

## 构建与安装交付

- 三个 Vite 应用共用 `scripts/license-assets.ts`，构建与开发服务都提供 `/LICENSE`、`/THIRD_PARTY_NOTICES.md`，HTML 带许可证链接元数据。WebUI 文件随页面内嵌到 Runtime。
- `scripts/build-release.mjs` 在归档根目录复制这两个文件，不再打包 BusyBox，也不再要求 `MOLR_BUSYBOX` 或 `MOLR_BUSYBOX_LICENSE`。
- `molr` 与 `molr-setup` 均为静态 Go 程序；安装维护代码使用 Go 标准库及已有的 `golang.org/x/sys`、`github.com/vishvananda/netlink`，未引入新模块。目标系统依赖不随包复制。
- 安装器从完整发行包或源码树读取这两个固定文件，用原子写入保存到安装前缀根目录，权限为 `0644`；缺少完整材料会报错，不静默忽略。它不复制任意目录，也不改变原文件正文。
- 安装器检查系统 BusyBox 必须为静态、可执行且不允许组/其他用户写入的普通文件。只有动态版本时也会提示安装 `busybox-static`。提供 `--install-deps` 时可通过 apt 安装；不支持 apt 或离线时，用户提前安装系统依赖。
- BusyBox 不被复制到 Molr 的安装目录；Runtime 继续从系统标准位置准备隔离环境。没有所需依赖时，不自动关闭沙盒或开放 Full Access。
- 具体部署命令和安装后文件位置见 [部署指南](../../admin/README.md)。

## Docker 隔离目标

`isolated` 构建目标从 Debian 仓库安装 systemd、Bubblewrap、静态 BusyBox、util-linux 与 CA 证书，保留镜像中的 `/usr/share/doc/` 许可材料。它们不加入原生发行归档，容器镜像当前由部署者本地构建，尚未接入镜像发布。

Docker 外层 seccomp 来自固定提交的 Moby 默认配置，原文件和 Apache-2.0 全文位于 `deploy/docker/vendor/`。派生配置仅增加 Bubblewrap 与受信任 socket broker 所需的三个受限规则；出处、修改说明和完整许可已加入 `THIRD_PARTY_NOTICES.md`，源文件与派生文件摘要纳入许可检查。助理命令的内层 seccomp 保持原有限制。

## 条款与源码依据

- [MIT 标准文本](https://opensource.org/license/mit)、[Apache-2.0 §4](https://www.apache.org/licenses/LICENSE-2.0)。
- [EPL-2.0](https://www.eclipse.org/legal/epl/epl-v20.html)、[EPL-1.0](https://www.eclipse.org/legal/epl-v10.html)、[MPL FAQ](https://www.mozilla.org/en-US/MPL/2.0/FAQ/)。
- [OFL-1.1](https://openfontlicense.org/open-font-license-official-text/)、[BusyBox 分发条件](https://www.busybox.net/license.html)、[GitHub 品牌用途](https://brand.github.com/foundations/logo#legal)。
- [elkjs 0.9.3 发布说明](https://github.com/kieler/elkjs/releases/tag/0.9.3)、[固定构建配置](https://github.com/kieler/elkjs/blob/0.9.3/build.gradle)。
- [WASI SDK 34](https://github.com/WebAssembly/wasi-sdk/tree/wasi-sdk-34)、[固定 wasi-libc 许可](https://github.com/WebAssembly/wasi-libc/blob/2e6fb9d8ee0cdf9e431fbcabe8af3115de000a13/LICENSE)。

本次范围是当前组成方式下的版权许可和材料交付，不是专利、商标注册或全部源文件权属的法律鉴定；新的目标平台、依赖版本、复制/链接方式和外部分发产物需要重新审查。

2026-10-11 核对版本检查及发布清单调整：未新增第三方依赖，发行包和安装器继续交付原有 `LICENSE` 与 `THIRD_PARTY_NOTICES.md`，静态 BusyBox 仍由目标系统提供。发布推荐清单仅使用 Node.js 标准库处理版本元信息，构建和安装脚本的通知复制范围不变。对应输入摘要已更新。

2026-10-12 核对文档目录重组：清单与本说明移入 `docs/dev/releasing/`，通知文件只更新清单引用路径；依赖版本、原始许可证及 NOTICE 正文未变。同步更新路径检查和通知文件摘要。

2026-10-13 核对浏览器组件交付：发行包不携带 agent-browser 或 Chrome for Testing 二进制，安装器按需下载固定版本的官方产物，安装 agent-browser 的原始 Apache-2.0 许可证并保留 Chrome 包内声明。agent-browser 原始许可证同时收录在汇总通知中，并嵌入安装器供安装时交付。Chrome for Testing 是基于 Chromium 的 Google Chrome 发行物，不按仅适用 Chromium BSD 许可的产物处理。未变更 npm 或 Go 依赖。

内置文档构建检查仅使用 Node.js 标准库，Runtime 资料包来自 Molr 自有中文文档；未新增第三方依赖或改变原有许可证和 NOTICE 交付范围。
