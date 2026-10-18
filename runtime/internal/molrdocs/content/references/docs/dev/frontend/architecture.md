# 前端架构

WebUI 管理账号会话、导航与页面装载；SDK、Host 和共享 UI 包分别维护协议、主题解析及基础组件。

桌面入口位于 [`apps/desktop`](https://github.com/molr-ai/molr/blob/main/apps/desktop/README.md)。可信管理界面负责连接和本机服务；独立、启用沙盒的 Electron `WebContentsView` 加载所选 Runtime 自带的 WebUI，每个连接使用独立持久化会话。远程主页面仅通过独立 preload 获得受限的 `window.molrApp` 接口，不能调用桌面管理 IPC；插件子 frame 不获得该接口。Linux、macOS 和 Windows 包均携带独立原生 Runtime，由安装器管理专属实例。关闭桌面窗口不停止服务。

## 实现边界

- `packages/sdk` / `@molr/sdk`：Runtime 客户端、DTO、分页、幂等写入、上传下载、错误、SSE 和 UI/Host 协议，不依赖 React 或 `@molr/ui`。
- `packages/ui-host` / `@molr/ui-host`：清单校验、固定版本主题身份、主题拥有的页面映射、原型隔离适配器的权限策略及插件主题类型。
- `apps/webui/src/App.tsx`：登录、导航、主题解析与页面加载、故障恢复。
- `apps/webui/src/ui-host/ThemeSelector.tsx`：主题设置中的主题选择器。
- `apps/webui/src/interfaces/official`：默认主题和原有产品组件。
- `apps/webui/src/interfaces/test`：仅声明聊天页的测试主题。

`@molr/ui` 提供官方共享组件、CSS 变量和资源。修改共享契约时同步检查 WebUI、官网和 UI Design 的调用点；项目尚无稳定版，不保留额外的旧版兼容层。

## 共享状态与恢复

主题切换不影响 Runtime 中的助理、消息和任务。聊天草稿以 `{ text, files }` 格式按账号和助理 ID 存储于管理壳的内存和当前标签页 `sessionStorage`，默认主题与测试主题使用同一份草稿。其他临时页面状态由各页面管理。

默认主题和测试主题的当前助理均由路由 ID 决定；`/#/assistants` 保持未选择状态，不回退到首个助理。列表或选择器操作通过导航更新 URL，再打开对应会话。

页面加载失败时保留壳的导航，并提供恢复默认主题与主题设置入口。恢复默认主题会先更新账号设置，成功后重新载入应用，清除浏览器缓存的模块加载错误；Runtime 暂时不可用或保存失败时，保留当前会话内的默认主题恢复，不刷新页面。正常 API 错误由业务页面处理。

周日历由 WebUI 的 `PlanWeekCalendar` 渲染，计划页默认进入周模式，聊天内嵌计划仍使用清单。周／月模式按可见日期范围读取同一计划接口，重复发生沿用 Runtime 的投影与事件标识。时间轴使用浏览器本地时区，按本地日期拆分跨天事件，以最小可点击高度计算重叠分栏；无结束时间的事件仅标记发生时间。切换日期按日历日期运算，跨夏令时不假定每天都是 24 小时。颜色复用共享计划类型 token，并以图标和状态样式辅助辨识。

## 后续扩展预留

SDK 中的 `connectUI()`、独立页面清单和隔离 iframe 适配器保留为后续开发基础，产品外部页面使用 [插件机制](https://molr.ai/zh/docs/dev/extensions/plugins/) 的独立、较窄契约；早期通用 iframe HTTP 代理不作为插件接入入口。

插件主题可以使用或不使用 `@molr/ui`，只需遵守 SDK/Host 契约。远程页面必须经过来源与权限确认；不能仅修改浏览器配置就获得执行权限。页面映射始终由主题定义，用户仍然只选择主题。

已有隔离适配器使用不带 same-origin 权限的 iframe 和私有 MessageChannel；逐请求校验方法、规范路径和授权范围，不向页面传递宿主 Cookie、CSRF 或 Bearer。权限表位于 `packages/ui-host/src/permissions.ts`。发布者需维护不可变版本资产；仅固定入口 URL 不等于锁定远程资产内容。

## 桌面 App 模式

Electron 在连接前后保留内置连接管理界面。连接 Runtime 时，独立且启用 Chromium 沙盒的 WebContentsView 通过专用 preload 向主页面暴露 `window.molrApp`；接口类型定义在 `@molr/ui-host/app`。WebUI 的 `AppFrame` 在认证门禁和页面主题之外完成握手，显示拖动区域、窗口控制与断开连接。语言、配色和明暗模式继续使用公共 Provider。普通浏览器没有桥接接口，保持原有网页布局。

App 模式的完整字标位于标题栏，工作区的连接状态和重试功能随字标一起呈现，左侧导航从顶部开始排列。登录页背景延伸到透明标题栏。标题栏属于应用内容：对话框和抽屉使用覆盖整个窗口的模态范围，遮罩包含标题栏，打开期间标题栏控件与其余背景内容一同不可交互。

桌面主进程校验当前 WebContents、主 frame、已配置连接来源和参数。远程页面仅能读取当前连接、控制当前窗口及断开；连接增删和本机服务管理仍由内置 UI 的独立接口负责。插件及自定义主题 iframe 不获得桌面接口。远程视图填满无边框窗口，加载失败、崩溃或握手超时回到内置恢复界面。断开保留连接分区与登录状态，不停止 Runtime。
