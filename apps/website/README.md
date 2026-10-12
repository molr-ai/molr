# Molr 产品官网

包名为 `@molr/website`，用于 `molr.ai` 的独立官网。通过配置在正常主页、上线预告和网站更新页之间切换。上线预告和更新页沿用居中的动态 Logo；正常主页提供产品介绍、场景界面示意和导航。

在仓库根目录运行：

```bash
npm ci
VITE_SITE_MODE=live npm run dev:website
npm run build:website
npm run preview:website
```

开发服务监听 `0.0.0.0:5189`，构建预览监听 `0.0.0.0:5190`，均支持局域网访问。生产产物为 `apps/website/dist/`，可独立部署到静态托管平台的域名根路径。根目录 `npm run build` 构建所有提供 build 脚本的工作区。

## Cloudflare Workers 部署

仓库根目录的 [`wrangler.jsonc`](../../wrangler.jsonc) 部署 `apps/website/dist/` 到 `molr` Worker。保留 Cloudflare Workers Builds 对 GitHub 仓库的授权和 `main` 自动部署。

| 设置 | 值 |
| --- | --- |
| 根目录 | 仓库根目录 |
| 构建命令 | `npm run build:website:release` |
| 部署命令 | `npx wrangler@4.45.0 deploy` |
| 构建变量 | `NODE_VERSION=22.21.0`、`VITE_SITE_MODE=live` |

发布构建命令默认检查最近 100 条公开 GitHub Release 元数据，按版本号选取正式版、预发布及 nightly 各一个，下载最多三份 manifest 并验证渠道目录，再构建官网；不依赖 GitHub CLI。Cloudflare 自己管理部署认证，GitHub 无需配置 Cloudflare Account ID／API token。普通本地构建仍使用 `npm run build:website`，不会自动联网读取发行信息。`MOLR_RELEASE_SCAN_LIMIT` 可调整检查条数，Cloudflare 构建变量与 GitHub 仓库变量应保持一致；窗口内缺少的渠道不会触发历史扫描。

在 Cloudflare Worker 的 Settings → Builds → Deploy Hooks 创建绑定 `main` 的 hook，将 URL 保存到 GitHub 仓库 secret `CLOUDFLARE_DEPLOY_HOOK`。`.github/workflows/website.yml` 在 release 完成后请求重新构建，并等待官网清单及安装脚本更新；main push 仍由原生仓库集成触发。详细配置、变量和重试见[发行维护](../../dev-docs/releasing/releases.md)。Runtime 按[部署指南](../../docs/deployment/README.md)独立部署。

## 网站状态开关

将 `apps/website/.env.example` 复制为同目录下的 `.env.local`，设置 `VITE_SITE_MODE`：

| 配置值 | 展示内容 |
| --- | --- |
| `coming-soon` | 上线前的「即将上线 / Coming soon」多语言轮播，当前默认值 |
| `maintenance` | 网站更新时的「马上回来 / Be right back」多语言轮播 |
| `live` | 显示个人 AI 助理主页，包含产品场景、安装与文档入口 |

也可在仓库根目录直接指定环境变量：

```bash
VITE_SITE_MODE=maintenance npm run dev:website
VITE_SITE_MODE=live npm run build:website
```

修改 `.env.local` 后重启开发服务。此开关在构建时确定，生产环境切换时需要重新构建并部署 `apps/website/dist/`；`npm run preview:website` 展示已构建产物中的模式。未设置或留空时保持 `coming-soon`，其他非法值会在启动或构建时报错。

`src/App.tsx` 根据模式选择 `HomePage` 或通用的 `SiteStatusPage`。正常主页位于 `src/HomePage.tsx`，样式在 `src/home.css`，中英文文案在 `src/home-content.ts`；上线预告和更新页共用 `src/StatusMessage.tsx` 的轮播与 `src/BrandPage.tsx` 的视觉布局。配置校验位于 `src/site-mode.ts`，各模式文案集中在 `src/site-content.ts`。

## 正常主页

本地开发和验收通常使用 `VITE_SITE_MODE=live npm run dev:website` 查看完整主页。未设置 `VITE_SITE_MODE`（包括未在 `.env.local` 中配置）或留空时，仍使用 `coming-soon`。

首屏保留“你的个人 AI 助理”标签、“从好奇出发。”主标题与“长久相伴，与你一同成长。”副标题；移除长段概述后，通过响应式间距保留原区域的留白。主页依次介绍设备连接、全天候工作、反馈与记忆、模型选择，以及计划与权限边界。“连接你的设备，拓展助理的能力”展示多台设备接入及各自的权限边界，独立 Linux 沙盒服务已支持远程存储、计算和网络，场景示意不代表跨 Runtime 执行迁移；“全天候工作”以 Molr 服务持续运行为前提；“从你的反馈中学习”突出定期自动回顾与手动记忆管理；“本地模型，也是一等公民”独立展示云端与本地模型的助理体验；“让每次委托，都有安排与边界”整合计划、资源权限和预算。各段共用一致的图文组件与场景示意，手机上先文后图，中英文同步维护。主页不显示场景示意、示例内容或开发中提示；多端接续指浏览器登录同一账号、连接同一服务。导航以留白连接首屏，不使用底部分隔线。聊天／计划／资源场景按钮位于界面下方，相隔 32px，切换静态界面内容，不连接 Runtime；上方保留与按钮行等高的留白，保持界面与首屏操作区的原有间距，英文窄屏换行时同样保持。首屏背景光晕在底部渐隐，避免在界面演示下方形成硬切边；演示框阴影和按钮间距保持独立。

导航包括主页、文档、GitHub、语言和明暗切换。首屏安装按钮仅保留下载图标，点击后以约 420 ms 的缓出动画滚动至底部安装区；滚轮、触摸或按键可中断，启用减少动态效果时直接定位。刷新及浏览器前进／后退沿用原有位置恢复；文档入口按当前语言连接 `/zh/docs/` 或 `/en/docs/`；安装和 GitHub 的地址统一放在 `src/site-links.ts`。安装入口指向快速开始文档；GitHub 入口统一指向 `https://github.com/molr-ai/molr`。首屏提供“安装 Molr”和“查看文档”入口；“查看文档”链接到当前语言的文档概览。底部安装面板按 stable／preview／nightly 渠道读取已发布清单，只展示对应版本实际可用的平台。支持复制渠道最新版命令（通过管道运行，无需保存安装脚本）、下载安装包及校验文件；手动安装校验并解压归档后直接运行包内 `molr-setup`，无需另行下载脚本；空渠道与加载失败分别显示提示，详细安装指南链接到快速开始文档。渠道说明和状态提示保留独立留白，手机端适当收紧。

主页装饰配色复用 `@molr/ui` 的 `paletteBrandColors`：安装按钮使用标准 primary 配色，青柠用于品牌装饰与安装区标题，海盐蓝、鸢尾紫与暖砂用于首屏装饰。场景示意使用统一中性表面；安装区保留森林深色背景，平台选中态、安装方式与禁用操作复用主题语义颜色。各段场景统一使用主题中性底色与界面表面色；场景按钮组选中态使用中性底色，不使用青柠强调色。首屏界面示意的彩色背景在界面底部之前收住；下方场景共用一致的背景和内边距。配色仅作用于主页。

主页语言保存到 `molr-website-locale`，首次和无法读取偏好时跟随浏览器；存储不可用时仍能在当前页面切换。外观复用 `@molr/ui` 的 `molr-theme` 偏好，默认跟随系统，按钮手动切换浅色／暗色。导航前有键盘跳转到正文的入口，320px 起适配小屏。

## 语言、动画与主题

两种状态页按固定顺序轮播中文、英文、日语、法语、韩语、西班牙语、德语、意大利语、葡萄牙语和阿拉伯语。首句优先匹配浏览器语言，无匹配时从英文开始；每句 5.2 秒，包含柔和显现、停留和隐去。文字使用轻微位移与虚化衔接，固定高度避免不同文字带来布局跳动，阿拉伯语使用从右向左的排版。正常主页首次按浏览器中英文偏好显示，可通过导航按钮切换并保存选择，不轮播。保存的主页语言不改变两种状态页的浏览器语言行为。

后台标签页暂停轮播，恢复后重新开始当前句的显示过程。系统启用减少动态效果时，停留在浏览器首选语言，不进行文字过渡。轮播文字对读屏软件隐藏，标题保持稳定的可读名称，避免循环播报。

`src/use-locale.ts` 根据当前模式和浏览器的中英文偏好设置页面标题、描述、Open Graph 元信息、HTML `lang` 与 Logo 的读屏操作名称；这些信息不会随每一帧轮播反复变化。Vite 将对应模式的标题、元信息和中英文兜底文案写入 HTML，HTML 启动脚本在 React 加载前设置同样的页面语言。

官网通过 `@molr/ui` 复用动态 Logo、`useTheme` 和语义颜色变量，外观沿用共享主题偏好，默认跟随系统。Logo 按本地 08:00–20:00 播放安静模式，其余时间播放活泼模式，等待时点击可继续；文字轮播与 Logo 时间轴独立。启用减少动态效果时使用静态 Logo。

Vite 的 `publicDir` 指向共享 UI 包的 `public/`。`/theme-init.js` 在首次绘制前设置配色，`/brand/` 提供 Logo 和 favicon；字体与样式直接从 HTML 加载，关闭 JavaScript 时显示中英文双语兜底内容。页面包含独立标题、描述、canonical 和 Open Graph 元信息。

品牌与组件的展示应用位于 [`apps/ui-design`](../ui-design/README.md)，不属于官网页面内容。

## 验证

在仓库根目录运行 `npm run test:website`。首次使用需 `npx playwright install chromium`；也可通过 `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` 指定已安装的 Chromium。

测试分别构建三种模式，在监听 `0.0.0.0` 的临时预览服务上检查中英文、桌面与手机、深浅色、元信息和无 JavaScript 兜底，并验证状态页的十种语言轮播、后台暂停与减少动态效果，以及主页场景切换、语言／主题刷新持久化、存储不可用、占位入口、安装锚点、平台禁用状态与安装方式切换。构建产物使用临时目录，测试结束后清理；截图保存在忽略提交的 `apps/website/test-results/site-modes/`。

## 产品文档

`live` 模式提供 `/zh/docs/`、`/en/docs/`、`/zh/dev-docs/` 与 `/en/dev-docs/`，在构建时只读取 `src/docs-routes.ts` 登记的 `docs/` 与 `dev-docs/` Markdown，无需复制正文。`src/docs-routes.ts` 管理阅读顺序、分组及 URL；新增文档时在此注册入口。中文正文保留原位置，英文正文位于 `docs/en/` 和 `dev-docs/en/`，使用相同的相对文件名和章节层级。路由清单为同一篇文档定义中英文标题和分组；两种语言都必须有正文。URL 中的语言优先于保存偏好和浏览器语言，切换语言使用 History API 跳转到同篇文档，并按对应章节保留定位，支持刷新和前进／后退。没有语言前缀的入口按偏好补全路径。首页文档入口、正文链接、侧栏、搜索、前后篇与页面提示均保持当前语言。明暗偏好与官网共享。

文档支持标题与正文关键词搜索；搜索文本在文档加载时统一规范化，空查询不扫描全文。搜索输入与移动端目录开关保留当前正文和已绘制的图表，切换文档时更新正文，切换明暗模式时更新图表。阅读导航提供桌面侧栏、移动端折叠目录、本页章节锚点和前后篇导航。侧栏标题与搜索框固定在导航滚动区域之外，长目录和搜索结果在剩余高度内独立滚动；手机展开目录沿用同样的分区。左右目录的原生滚动条使用透明轨道，滚动时显示滑块，停止约 700 ms 后隐藏；保留滚动区域宽度，显隐不引起布局跳动。顶部导航按当前栏目分别选中用户文档或开发者文档。导航下方仍有内容时显示缓慢上下移动的向下箭头，滚到底或内容能完整容纳时隐藏；搜索、窗口缩放和手机展开目录会重新判断。箭头不占用导航高度，系统／浏览器启用减少动态效果时保持静止。官网与文档间的站内链接通过 History API 无刷新切换，文档切页保留侧栏和搜索词，并收起移动端目录；普通切页回到顶部，章节链接定位目标，浏览器前进后退恢复阅读位置。修改键、新标签页、下载和外链保留浏览器原生行为。Markdown 相对链接转换为官网路径，并保留章节片段；引用未登记官网路由的仓库文件时展示仓库路径，避免链接到未公开的仓库或不存在的网页。原始 HTML 不执行。

构建会为每篇已登记文档生成 `dist/{zh,en}/docs/**/index.html` 或 `dist/{zh,en}/dev-docs/**/index.html`，包含对应语言的 HTML `lang`、标题、描述、Open Graph、canonical 及双向 `hreflang`，可在支持目录索引的静态服务器直接访问和刷新。正文由 JavaScript 渲染。静态托管建议为未知路径配置 404；支持 SPA fallback 的托管会显示文档未找到提示。`coming-soon` 与 `maintenance` 模式不开放文档页面，也不生成文档 HTML 入口。

运行 `npm run test:website` 会检查桌面和手机逐字搜索时正文与图表的节点保留、搜索结果切页及图表主题更新，并检查各篇文档、生成的 HTML 元信息、相对链接与深层锚点、搜索、语言和主题、手机目录，以及 320px 至 1440px 的布局。

## 本地安装分发

`installer-plugin.ts` 从 `scripts/install.sh` 输出 `/install.sh`。生产构建使用 `MOLR_CATALOG_DIR` 指定经过验证的渠道目录，输出到 `/releases/`。目录只含公开版本信息与 GitHub 下载地址，二进制不进入 Worker。安装区运行时读取 `/releases/catalog.json`，按 stable／preview／nightly 展示实际发布的平台、渠道安装命令及下载入口；无版本时显示空状态，读取失败时可重试。

`MOLR_RELEASE_URL` 配置脚本默认发布源，默认为 `https://molr.ai/releases`；安装区默认使用当前站点 `/releases`，跨域源可用 `VITE_RELEASE_SOURCE` 指定。验收使用 `MOLR_CATALOG_DIR=.cache/release-catalog VITE_SITE_MODE=live npm run dev:website`。未指定 catalog 时输出空目录，不把开发包当作公开发行版。保留 `MOLR_RELEASE_DIR` 供本地校验包下载，放在 `/packages/`，不能与 catalog 同时配置。

## 公开文档范围

`src/docs-routes.ts` 是经审查的文档入口清单。`docs-plugin.ts` 仅从这些条目读取公开 Markdown，为页面内容和搜索提供同一份数据；新文件不会因为落在文档目录就自动打包。新增路由前审查内容并更新文档索引，不登记内部规划或本地验收记录。

`internal-docs/` 不进入 Git、静态资源或构建产物。共享发布插件拒绝导入该目录，开发服务器也拒绝直接读取；文档路径不允许目录越界和符号链接。目录调整后检查旧路由、全文搜索、正文链接和生成的 JavaScript，不能仅从导航中隐藏页面。
