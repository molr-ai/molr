# 开发协作指南

本目录维护贡献者与编码代理共同遵守的开发流程。[AGENTS.md](https://github.com/molr-ai/molr/blob/main/AGENTS.md) 是必读索引；所引用规范具有相同约束力。产品实现文档按主题从[开发文档索引](https://molr.ai/zh/docs/dev/)进入，正式部署见[管理员指南](https://molr.ai/zh/docs/admin/)。

## 阅读顺序

| 阶段 | 文档 | 内容 |
| --- | --- | --- |
| 准备内容 | [公开内容与内部资料](https://molr.ai/zh/docs/dev/releasing/public-content/#公开内容与内部资料) | 可提交内容、私有资料与发布审查 |
| 准备环境 | [开发环境](https://molr.ai/zh/docs/dev/contributing/environment/) | 机器目录、共享下载缓存、开发服务 |
| 开始任务 | [协作流程](https://molr.ai/zh/docs/dev/contributing/workflow/) | Worktree、分支、验证、提交、合并与清理 |
| 验证与交付 | [验收与服务生效](https://molr.ai/zh/docs/dev/contributing/acceptance/) | WebUI 链接、桌面开发包、模型测试、Runtime 和主服务验证 |
| 维护文档 | [文档维护约定](https://molr.ai/zh/docs/dev/contributing/documentation/) | 内容归属、中英文同步、链接与官网发布 |
| 维护 CI | [Gitea CI](https://molr.ai/zh/docs/dev/contributing/gitea-ci/) | Action 镜像、runner 与独立安装验收 |

## 规范归属

前端的视觉、文案和交互要求分别由[设计系统](https://molr.ai/zh/docs/dev/frontend/design-system/)与[交互约定](https://molr.ai/zh/docs/dev/frontend/interaction/)维护。构建发布、公开内容和许可证由[发行维护](https://molr.ai/zh/docs/dev/releasing/releases/)、[公开内容规范](https://molr.ai/zh/docs/dev/releasing/public-content/)与[许可证规范](https://molr.ai/zh/docs/dev/releasing/licensing/)维护。本目录通过链接引用这些规范，具体规则在负责该主题的文档中维护。
