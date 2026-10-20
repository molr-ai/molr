# Molr 开发文档

本目录面向贡献者与维护者。日常使用见[用户指南](../user/README.md)，安装、资源接入和运维见[管理员指南](../admin/README.md)。

[路线图](roadmap.md)列出近期要做、远期计划和尚未决定的功能，以及各项的范围和完成标准。

## 开始贡献

先阅读[开发协作与验收](contributing/development.md)，按 worktree 规范开展任务；文档内容归属、链接更新和官网发布检查见[文档维护约定](contributing/documentation.md)。[AGENTS.md](../../AGENTS.md) 列出编码任务必读规范。

本地启动和测试命令由各应用维护：[Runtime](../../runtime/README.md)、[WebUI](../../apps/webui/README.md)、[官网](../../apps/website/README.md)、[UI Design](../../apps/ui-design/README.md)。

[Gitea CI](contributing/gitea-ci.md) 说明本地 action 镜像、Linux/macOS/Windows runner 标签及独立安装验收配置。

## 理解架构与执行

从[架构总览](architecture/overview.md)了解系统组成、模块职责与执行链路，再按改动范围阅读[技术底座](architecture/tech-stack.md)、[上下文更新](runtime/agent-context.md)、[上下文存储](runtime/context-storage.md)、[记忆与自动整理](runtime/dream-memory.md)、[模型适配](runtime/model-adapters.md)或[多模态文件](runtime/multimodal-files.md)。排查执行过程见[开发工作台](runtime/workbench.md)。

## 修改权限与隔离

[安全模型](security/security-model.md)说明认证与信任边界；[用户与用户组](security/users-and-groups.md)、[资源额度](security/resource-quotas.md)和[沙盒实现](security/sandbox.md)分别维护身份隔离、额度计量和执行隔离机制。

## 查阅调用契约

客户端请求查[HTTP API](reference/http-api.md)，内部模块调用查[Go 模块接口](reference/module-interfaces.md)，模型文件与资源操作查[工具契约](reference/file-tools.md)。TypeScript 客户端入口见[SDK](../../packages/sdk/README.md)。

## 开发前端与扩展

- [前端架构](frontend/architecture.md)、[主题与页面解析](frontend/themes.md)：SDK、Host、页面装载与状态恢复。
- [品牌](frontend/brand.md)、[设计系统](frontend/design-system.md)、[交互约定](frontend/interaction.md)：视觉资源、语义颜色、无障碍与共用交互；组件属性查[组件 API](../../packages/ui/COMPONENTS.md)。
- [MCP 接入](extensions/mcp.md)：远程工具发现、执行与授权。
- [插件机制](extensions/plugins.md)、[微信渠道](extensions/weixin.md)、[飞书渠道](extensions/feishu.md)、[Telegram 渠道](extensions/telegram.md)、[Slack 渠道](extensions/slack.md)、[钉钉渠道](extensions/dingtalk.md)：扩展协议、授权、生命周期与消息投递。

## 维护发行

[发行维护](releasing/releases.md)管理版本、渠道和构建发布；提交与分发前核对[公开内容与文件隔离](releasing/public-content.md)及[许可证](releasing/licensing.md)。

[QQ 渠道](extensions/qq.md)介绍扫码、配对和消息投递。
