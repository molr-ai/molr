# 前端架构

WebUI 管理账号会话、导航与页面装载；SDK、Host 和共享 UI 包分别维护协议、主题解析及基础组件。

桌面入口位于 [`apps/desktop`](https://github.com/molr-ai/molr/blob/main/apps/desktop/README.md)。可信管理界面负责连接和本机服务；独立、启用沙盒的 Electron `WebContentsView` 加载所选 Runtime 自带的 WebUI，每个连接使用独立持久化会话。远程主页面仅通过独立 preload 获得受限的 `window.molrApp` 接口，不能调用桌面管理 IPC；插件子 frame 不获得该接口。Linux、macOS 和 Windows 包均携带独立原生 Runtime，由安装器管理专属实例。关闭桌面窗口不停止服务。

## 实现边界

- `packages/sdk` / `@molr/sdk`：Runtime 客户端、DTO、分页、幂等写入、上传下载、错误、SSE 和 UI/Host 协议，不依赖 React 或 `@molr/ui`。
- `packages/ui-host` / `@molr/ui-host`：固定版本主题身份、主题拥有的页面映射、插件主题类型。
- `apps/webui/src/App.tsx`：登录、导航、主题解析与页面加载、故障恢复。
- `apps/webui/src/ui-host/ThemeSelector.tsx`：主题设置中的主题选择器。
- `apps/webui/src/interfaces/official`：默认主题和原有产品组件。
- `apps/webui/src/interfaces/test`：仅声明聊天页的测试主题。

`@molr/ui` 提供官方共享组件、CSS 变量和资源。修改共享契约时同步检查 WebUI、官网和 UI Design 的调用点；项目尚无稳定版，不保留额外的旧版兼容层。

官方计划、资源、设置、工作台页面及聊天侧栏按需加载；工具栏按钮的轻量状态订阅与面板代码分离。通知提醒关闭或通知属于首次快照时，未展示的通知不再单独读取详情；同一通知的并发变化按版本串行合并，新版本保留后续读取。已经展示的通知仍同步已读、关闭和恢复状态。

## 共享状态与恢复

主题切换不影响 Runtime 中的助理、消息和任务。聊天草稿以 `{ text, files }` 格式按账号和助理 ID 存储于管理壳的内存和当前标签页 `sessionStorage`，默认主题与测试主题使用同一份草稿。其他临时页面状态由各页面管理。

默认主题和测试主题的当前助理均由路由 ID 决定；`/#/assistants` 保持未选择状态，不回退到首个助理。列表或选择器操作通过导航更新 URL，再打开对应会话。

页面加载失败时保留壳的导航，并提供恢复默认主题与主题设置入口。恢复默认主题会先更新账号设置，成功后重新载入应用，清除浏览器缓存的模块加载错误；Runtime 暂时不可用或保存失败时，保留当前会话内的默认主题恢复，不刷新页面。正常 API 错误由业务页面处理。

周日历由 WebUI 的 `PlanWeekCalendar` 渲染，计划页默认进入周模式，聊天内嵌计划仍使用清单。周／月模式按可见日期范围读取同一计划接口，重复发生沿用 Runtime 的投影与事件标识。时间轴使用浏览器本地时区，按本地日期拆分跨天事件，以最小可点击高度计算重叠分栏；无结束时间的事件仅标记发生时间。切换日期按日历日期运算，跨夏令时不假定每天都是 24 小时。颜色复用共享计划类型 token，并以图标和状态样式辅助辨识。

## 后续扩展预留

外部页面和主题统一通过 `connectPlugin()` 使用[插件机制](https://molr.ai/zh/docs/dev/extensions/plugins/)的能力契约。宿主支持内置页面和已安装插件贡献。

插件主题可以使用或不使用 `@molr/ui`，只需遵守 SDK/Host 契约。远程页面必须经过来源与权限确认；不能仅修改浏览器配置就获得执行权限。页面映射始终由主题定义，用户仍然只选择主题。

插件页面使用隔离 iframe 和私有 MessageChannel。`PluginFrame` 逐项检查命名操作及贡献声明的能力，不向页面传递宿主 Cookie、CSRF 或 Bearer；Runtime 校验已安装内容及权限。

## 桌面 App 模式

Electron 在连接前后保留内置连接管理界面。连接 Runtime 时，独立且启用 Chromium 沙盒的 WebContentsView 通过专用 preload 向主页面暴露 `window.molrApp`；接口类型定义在 `@molr/ui-host/app`。WebUI 的 `AppFrame` 在认证门禁和页面主题之外完成握手，显示拖动区域、窗口控制与断开连接。语言、配色和明暗模式继续使用公共 Provider。普通浏览器没有桥接接口，保持原有网页布局。

App 模式的完整字标位于标题栏，工作区的连接状态和重试功能随字标一起呈现，左侧导航从顶部开始排列。登录页背景延伸到透明标题栏。连接管理页和 WebUI 标题栏在所有平台、全屏与非全屏下都固定为 48 CSS px，Logo 和右侧操作按钮垂直居中。macOS 使用 Electron `titleBarStyle: hidden` 与 `titleBarOverlay: true` 保留原生红黄绿按钮。主进程启动时从可信内置页面的原生安全区域测得按钮高度，再按当前页面缩放计算等距的上、左边距，使原生按钮在 48px 标题栏内居中；原生坐标取整时允许不足 1 个逻辑点的误差。页面缩放引起的 viewport resize 由 preload 通知主进程，通知不携带尺寸参数，主进程读取真实缩放值。非全屏通过 `env(titlebar-area-x)` 为 Logo 避让按钮；全屏时取消按钮专用留白，Logo 使用普通的 16px 左边距。按钮实际尺寸不在业务代码中硬编码。Windows/Linux 继续使用右侧窗口按钮。

标题栏中的网页内容属于应用模态范围：对话框和抽屉遮罩覆盖整个窗口，打开期间网页标题栏控件与其余背景内容一同不可交互。macOS 原生窗口按钮由系统管理，仍可用于关闭、最小化和全屏。

内置连接管理页使用共享完整 Logo 的昼夜动态版本，遵守系统减少动态效果设置。桌面维护操作与 App 标题栏操作失败复用共享 Toast，不插入占用页面布局的错误块；安装器详细输出仍保存在维护日志中。

桌面主进程校验当前 WebContents、主 frame、已配置连接来源和参数。远程页面能读取当前连接、控制当前窗口、断开及发起下述 OAuth 流程；连接增删和本机服务管理仍由内置 UI 的独立接口负责。插件及自定义主题 iframe 不获得桌面接口。远程视图填满无边框窗口，加载失败、崩溃或握手超时回到内置恢复界面。断开保留连接分区与登录状态，不停止 Runtime。

主进程在创建窗口后读取连接存储中的 `lastConnection`，复用对应持久化分区恢复连接，覆盖进程重启和 macOS 关闭窗口后重开。仅发起连接时保存选择；主动返回管理页清除选择，关闭窗口及加载失败保留选择。新增连接不会自动选中，删除所选连接会清除选择。恢复使用正常连接与故障恢复流程，不自动启动已停止的本机服务。

## 桌面通知

WebUI 的 `NotificationAdapter` 在 App 中选择原生适配器，通过 `molrApp.notifications` 调用主进程的 Electron Notification；浏览器继续使用 Web Notifications。现有桌面通知开关触发系统通道初始化，主进程不再为 Runtime 弹出自定义通知授权框，也不依赖网页的通知权限。macOS 的通道初始化可能触发系统授权，因此不在页面加载时调用 `Notification.isSupported()`；该返回值仅表示通道支持，不能显示为系统已授权。

每次登录生命周期使用独立通知 scope。主进程校验当前页面、主 frame、连接来源、scope、文本长度和内部路由；退出、导航、断开及关闭开关清理通知。保留原生对象以支持已读撤回和过期点击隔离。发送等待原生 show／failed 事件并限制等待时长，失败回退应用内提醒；系统专注模式可能抑制展示。状态与测试结果放在对应设置行中。macOS 原生授权、展示和点击需使用签名 App 验收。

## 桌面 OAuth 回调

主页面通过 `window.molrApp.oauth.authorize(providerID)` 发起已注册服务的授权，通过 `oauth.cancel()` 取消等待。`authorize` 返回 `{ status, attemptId?, connectionId? }`；`status` 为 `connected`、`cancelled`、`failed` 或 `unknown`。调用不会返回授权码或 token。普通浏览器与 iframe 不拥有这一接口。

客户端先监听临时 `127.0.0.1` 端口，再用当前连接分区的会话和 CSRF proof 请求 Runtime 准备授权。Runtime 管理 state、nonce 和 PKCE，客户端原生对话框展示授权网站及目标连接，随后打开系统浏览器。监听器只接受匹配 Host、路径、state 的单次 GET 回调，将白名单字段直接提交到固定 Runtime 接口。主进程逐次检查当前视图和登录身份；切换连接、主页面导航、关闭窗口、取消或超时都会关闭监听器。请求体、回调地址与凭据不写日志，也不传给插件。

本机与远程 Runtime 使用同一流程，包括用户已明确启用的局域网 HTTP。授权码在客户端与 Runtime 之间传输，token 由 Runtime 服务适配器换取并保管；这不为 HTTP 增加传输加密。回调失败或状态未知不自动重试换取 token。服务适配器尚未接入，当前可通过独立模拟流程验收底座；契约及适配器职责见 [HTTP API](https://molr.ai/zh/docs/dev/reference/http-api/#桌面-oauth-授权)，运行方式见 [Desktop README](https://github.com/molr-ai/molr/blob/main/apps/desktop/README.md#local-oauth-callback-and-runtime-handoff)。
