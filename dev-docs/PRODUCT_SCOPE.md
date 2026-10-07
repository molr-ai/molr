# Molr 产品范围

本文描述当前仓库的组成和能力边界，供用户与贡献者理解实现；功能操作见 [产品文档](../docs/README.md)，模块设计见 [Runtime 架构](runtime/ARCHITECTURE.md)。

## 组成

- Go Runtime 提供身份认证、数据持久化、Agent 执行、计划、资源授权和费用管理。
- WebUI 连接真实 Runtime，桌面和手机浏览器使用同一服务。
- UI Design 使用模拟数据展示组件和交互；其 Desktop／Mobile／CLI 视图不代表独立原生客户端。
- 官网提供产品介绍和公开文档，与用户的 Runtime 实例分别部署。

## 能力与边界

| 主题 | 当前范围 | 实现与操作入口 |
| --- | --- | --- |
| 助理与聊天 | 每位助理独立执行上下文与持续会话，显式读取历史并发布回复 | [助理与计划](../docs/assistants-and-plans.md)、[上下文](runtime/AGENT_CONTEXT.md) |
| 计划与执行 | 管理安排、执行状态和结果；停止不回滚已经发生的外部副作用 | [助理与计划](../docs/assistants-and-plans.md) |
| 账号与授权 | 独立账号、管理员用户组、成员配额及助理权限；没有用户间共享空间或协同编辑 | [用户与用户组](../docs/users-and-groups.md)、[资源授权额度](runtime/RESOURCE_QUOTAS.md) |
| 模型与费用 | 配置支持的模型协议与凭据，按助理管理预算；远程推理受供应商能力和网络影响 | [模型适配](runtime/MODEL_ADAPTERS.md)、[资源与权限](../docs/resources-and-permissions.md) |
| 文件与命令 | 受授权范围约束的文件工具、Linux 隔离命令和远程 Linux Sandbox | [文件工具](runtime/FILE_TOOLS.md)、[沙盒设计](runtime/SANDBOX.md) |
| 插件与主题 | 管理员导入并授权插件，受控工具、Skills、主题及独立页面；不加载动态 Go 业务模块 | [插件机制](runtime/PLUGINS.md) |
| 微信 | 固定渠道适配器接入已有助理；平台约束和真实账号验证边界见专项说明 | [微信渠道](runtime/WEIXIN.md) |

## 部署限制

当前采用单实例 Go 服务、SQLite 和本地文件；没有多副本自动接管或分布式调度。命令沙盒要求具备相应 Linux 内核能力和 cgroup 委派；macOS／Windows 不能视为具有等价的本机 Shell 隔离。授权的远程 Linux 节点遵循独立部署说明。

本地部署不代表所有输入均在本机处理。使用远程模型、聊天渠道或网络工具时，相关数据可能发送到相应服务。普通沙盒的能力报告以实际探针为准；显式 Full Access 具有不同安全边界，见 [安全说明](runtime/SECURITY.md)。

新增能力应同步代码、测试和对应的公开说明。探索性设计不构成产品承诺，模拟界面或单次本地验收不能替代真实部署能力检查。
