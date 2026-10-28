# Molr 开发文档

本目录面向贡献者与维护者。日常使用见[用户指南](https://molr.ai/zh/docs/user/)，安装、资源接入和运维见[管理员指南](https://molr.ai/zh/docs/admin/)。

[路线图](https://molr.ai/zh/docs/dev/roadmap/)列出近期要做、远期计划和尚未决定的功能，以及各项的范围和完成标准。

## 开始贡献

从[开发协作指南](https://molr.ai/zh/docs/dev/contributing/)按顺序阅读[开发环境](https://molr.ai/zh/docs/dev/contributing/environment/)、[协作流程](https://molr.ai/zh/docs/dev/contributing/workflow/)和[验收规范](https://molr.ai/zh/docs/dev/contributing/acceptance/)。[AGENTS.md](https://github.com/molr-ai/molr/blob/main/AGENTS.md) 列出任务开始前的必读规范。

文档归属、写作、中英文同步和官网发布见[文档维护约定](https://molr.ai/zh/docs/dev/contributing/documentation/)；CI 镜像、runner 与独立安装验收见[Gitea CI](https://molr.ai/zh/docs/dev/contributing/gitea-ci/)。

本地启动和测试命令由各应用维护：[Runtime](https://github.com/molr-ai/molr/blob/main/runtime/README.md)、[WebUI](https://github.com/molr-ai/molr/blob/main/apps/webui/README.md)、[桌面 App](https://github.com/molr-ai/molr/blob/main/apps/desktop/README.md)、[官网](https://github.com/molr-ai/molr/blob/main/apps/website/README.md)、[UI Design](https://github.com/molr-ai/molr/blob/main/apps/ui-design/README.md)。

## 理解架构与执行

从[架构总览](https://molr.ai/zh/docs/dev/architecture/overview/)了解系统组成、模块职责与执行链路，再按改动范围阅读[技术底座](https://molr.ai/zh/docs/dev/architecture/tech-stack/)、[上下文更新](https://molr.ai/zh/docs/dev/runtime/agent-context/)、[上下文存储](https://molr.ai/zh/docs/dev/runtime/context-storage/)、[记忆与自动整理](https://molr.ai/zh/docs/dev/runtime/dream-memory/)、[模型适配](https://molr.ai/zh/docs/dev/runtime/model-adapters/)或[多模态文件](https://molr.ai/zh/docs/dev/runtime/multimodal-files/)。排查执行过程见[开发工作台](https://molr.ai/zh/docs/dev/runtime/workbench/)。

## 修改权限与隔离

[安全模型](https://molr.ai/zh/docs/dev/security/security-model/)说明认证与信任边界；[用户与用户组](https://molr.ai/zh/docs/dev/security/users-and-groups/)、[资源额度](https://molr.ai/zh/docs/dev/security/resource-quotas/)和[沙盒实现](https://molr.ai/zh/docs/dev/security/sandbox/)分别维护身份隔离、额度计量和执行隔离机制。

## 查阅调用契约

客户端请求查[HTTP API](https://molr.ai/zh/docs/dev/reference/http-api/)，内部模块调用查[Go 模块接口](https://molr.ai/zh/docs/dev/reference/module-interfaces/)，模型文件与资源操作查[工具契约](https://molr.ai/zh/docs/dev/reference/file-tools/)。TypeScript 客户端入口见[SDK](https://github.com/molr-ai/molr/blob/main/packages/sdk/README.md)。

## 开发前端

- [前端架构](https://molr.ai/zh/docs/dev/frontend/architecture/)、[主题与页面解析](https://molr.ai/zh/docs/dev/frontend/themes/)：SDK、Host、页面装载与状态恢复。
- [设计系统](https://molr.ai/zh/docs/dev/frontend/design-system/)、[交互约定](https://molr.ai/zh/docs/dev/frontend/interaction/)：视觉、文案、操作与反馈标准。
- [品牌](https://molr.ai/zh/docs/dev/frontend/brand/)、[组件 API](https://github.com/molr-ai/molr/blob/main/packages/ui/COMPONENTS.md)：品牌资源与共享组件使用方式。

## 开发扩展与渠道

- [MCP 接入](https://molr.ai/zh/docs/dev/extensions/mcp/)、[插件机制](https://molr.ai/zh/docs/dev/extensions/plugins/)：工具发现、扩展协议、授权与生命周期。
- [微信](https://molr.ai/zh/docs/dev/extensions/weixin/)、[飞书](https://molr.ai/zh/docs/dev/extensions/feishu/)、[Telegram](https://molr.ai/zh/docs/dev/extensions/telegram/)、[Slack](https://molr.ai/zh/docs/dev/extensions/slack/)、[钉钉](https://molr.ai/zh/docs/dev/extensions/dingtalk/)、[QQ](https://molr.ai/zh/docs/dev/extensions/qq/)：渠道连接、身份绑定与消息投递。

## 维护发行

[发行维护](https://molr.ai/zh/docs/dev/releasing/releases/)管理版本、渠道和构建发布；提交与分发前核对[公开内容与文件隔离](https://molr.ai/zh/docs/dev/releasing/public-content/)及[许可证](https://molr.ai/zh/docs/dev/releasing/licensing/)。

- [持续关注与 watcher 协议](https://molr.ai/zh/docs/dev/runtime/watchers/)
