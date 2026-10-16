# 前端架构

WebUI 管理账号会话、导航与页面装载；SDK、Host 和共享 UI 包分别维护协议、主题解析及基础组件。

桌面入口位于 [`apps/desktop`](../../../apps/desktop/README.md)。可信管理界面负责连接和本机服务；独立、启用沙盒的 Electron `WebContentsView` 加载所选 Runtime 自带的 WebUI，每个连接使用独立持久化会话。远程页面不注入 preload，也不能调用桌面管理 IPC。Windows 包仅包含客户端；Linux/macOS 包携带独立 Runtime，通过现有安装器管理专属实例。关闭桌面窗口不停止服务。

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

## 后续扩展预留

SDK 中的 `connectUI()`、独立页面清单和隔离 iframe 适配器保留为后续开发基础，产品外部页面使用 [插件机制](../extensions/plugins.md) 的独立、较窄契约；早期通用 iframe HTTP 代理不作为插件接入入口。

插件主题可以使用或不使用 `@molr/ui`，只需遵守 SDK/Host 契约。远程页面必须经过来源与权限确认；不能仅修改浏览器配置就获得执行权限。页面映射始终由主题定义，用户仍然只选择主题。

已有隔离适配器使用不带 same-origin 权限的 iframe 和私有 MessageChannel；逐请求校验方法、规范路径和授权范围，不向页面传递宿主 Cookie、CSRF 或 Bearer。权限表位于 `packages/ui-host/src/permissions.ts`。发布者需维护不可变版本资产；仅固定入口 URL 不等于锁定远程资产内容。
