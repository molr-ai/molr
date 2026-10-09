# Molr 开发文档

本目录面向贡献者与维护者。用户操作和自托管部署的当前说明在 [产品文档](../docs/README.md)；各应用／包的 README 保留启动、测试及局部实现入口。

## 文档分工

| 位置 | 内容 | 维护约定 |
| --- | --- | --- |
| `docs/` | 当前产品使用、部署与排障 | 写已支持的操作和限制，不把原型或规划写成可用能力 |
| `dev-docs/PRODUCT_SCOPE.md` | 当前产品范围与实现入口 | 与用户指南和代码一致，不承诺未定功能 |
| `dev-docs/runtime/` | 架构、接口、执行与安全边界 | 以实际代码为依据，专项文档各维护一个主题 |
| `dev-docs/frontend/` | 品牌、组件与前端架构 | 与应用 README 和组件 API 分工，避免重复实现清单 |
| `internal-docs/`（本地忽略） | 内部规划、研究、历史提案和私有验收记录 | 不提交、不打包，不作为公开构建或阅读依赖 |

更新功能时，同步相应用户指南与负责该主题的开发文档，直接替换过期正文，避免只追加修正记录。新增产品指南或现行 Runtime 专题时，同时登记 `apps/website/src/docs-routes.ts`，核对构建入口和正文链接；仅已审查的公开主题登记到官网；内部材料不得加入官网路由或内容导入。仓库协作规范可保留为仓库引用。用户文档维护操作，HTTP API 维护请求字段，专项开发文档维护执行机制；各 README 以启动、验证和索引为主。详细验收结果保存到本地内部目录，不与现行规范混排。测试执行结果必须带日期；旧测试数量、公开价格与调研结论不视为持续有效的最新状态。

## 开发入口

- [开源发布与私有文件隔离](RELEASE_SAFETY.md)：忽略规则、静态资源清单、源码导出和归档检查。

- [许可证与第三方兼容性](LICENSING.md)：MIT 范围、依赖版本清单、单文件分发材料与维护方式。
- [开发协作与验收](DEVELOPMENT.md)：worktree 隔离、验收入口、Runtime 构建与沙盒、合并生效、服务与端口约定。
- [真实模型测试](DEVELOPMENT.md#真实模型测试)：优先使用主环境已配置的本地模型，保持验收数据隔离，并区分真实模型与模拟测试结果。

- [Runtime 开发与测试](../runtime/README.md)、[WebUI 开发](../apps/webui/README.md)、[UI Design](../apps/ui-design/README.md)、[官网](../apps/website/README.md)。
- [产品范围](PRODUCT_SCOPE.md)：当前能力、原型边界和实现入口。
- [Runtime 架构](runtime/ARCHITECTURE.md)：运行结构、模块职责、事件与恢复。
- [技术底座](runtime/TECH_STACK.md)、[Go 模块接口](runtime/INTERFACES.md)、[HTTP API 与公共协议边界](runtime/HTTP_API.md)。
- [微信渠道](runtime/WEIXIN.md)：扫码绑定、单入口授权、入站去重与独立投递队列。
- [插件机制](runtime/PLUGINS.md)：扩展包、插件市场入口、用户组授权、可选个人／管理员配置、主题与页面、服务连接、Shell／HTTP 工具、Skill、隔离和生命周期。
- [多用户与成员配额](runtime/MULTI_USER.md)：账号迁移、资源目录、私有数据与组级额度。
- [安全边界](runtime/SECURITY.md)、[沙盒实现](runtime/SANDBOX.md)、[文件与资源工具](runtime/FILE_TOOLS.md)、[资源授权额度](runtime/RESOURCE_QUOTAS.md)。
- [做梦与分层记忆](runtime/DREAM_MEMORY.md)：经历、主题与核心认知的自动沉淀，文件提交恢复和用户纠错。
- [上下文存储](runtime/CONTEXT_STORAGE.md)、[上下文更新](runtime/AGENT_CONTEXT.md)、[模型适配](runtime/MODEL_ADAPTERS.md)、[多模态文件](runtime/MULTIMODAL_FILES.md)、[开发工作台](runtime/WORKBENCH.md)。
- [品牌与 UI 基础](frontend/BRAND.md)、[主题与 UI 包](frontend/UI_PACKAGES.md)、[前端颜色规范与同屏辨识检查](frontend/UI_PACKAGES.md#前端颜色开发规范)、[组件 API](../packages/ui/COMPONENTS.md)。
- [许可证与分发](LICENSING.md)、[发布与私有文件隔离](RELEASE_SAFETY.md)。

## 文档冲突时的处理

当前用户操作以产品指南为入口，实现约束以对应代码及测试核实；发现不一致应修正文档，而不是默认为某份旧方案优先。内部需求和研究不构成已实现声明，也不是外部贡献者必须取得的依赖。不要复制接口列表、配置默认值和完整功能清单到多个总览中。
