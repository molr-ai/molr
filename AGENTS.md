# 项目协作约定

这是一个开发中的项目，且没有发布过稳定版。不维护旧版 API 兼容层；持久化结构变更通过数据库版本迁移保留受支持版本的数据，业务代码只处理当前结构。请始终按照 KISS 原则设计和实现。

开始任务前必须阅读并遵守以下开发者文档；这些引用是本文件的组成部分。

- [公开内容与内部资料](docs/dev/releasing/public-content.md#公开内容与内部资料)
- [开发环境](docs/dev/contributing/environment.md)：开发目录、共享下载缓存与开发服务。
- [协作流程](docs/dev/contributing/workflow.md)：Worktree、分支、提交、合并与清理。
- [验收与服务生效](docs/dev/contributing/acceptance.md)：验收交付、真实模型测试、Runtime 验证与合并生效。
- [设计系统](docs/dev/frontend/design-system.md)：视觉规范、语义颜色与同屏辨识检查。
- [交互约定](docs/dev/frontend/interaction.md)：UI/UX 标准、文案、操作与反馈。
- [文档维护约定](docs/dev/contributing/documentation.md)

后台状态等待默认使用提交后事件通知与最近到期定时器。新增或调整周期轮询必须遵守[后台等待与轮询门禁](docs/dev/contributing/workflow.md#后台等待与轮询门禁)，登记必要性和调用预算，并通过空闲行为测试；不得用调小轮询间隔解决状态同步问题。

阅读顺序见[开发协作指南](docs/dev/contributing/README.md)，产品实现与专项文档见[开发文档索引](docs/dev/README.md)。
