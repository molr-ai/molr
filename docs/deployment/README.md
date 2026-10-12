# 部署与维护

本目录面向安装和维护 Molr 服务的部署者。首次体验可从[快速开始](../getting-started.md)完成安装和第一轮委托。

## 选择安装方式

| 场景 | 阅读入口 |
| --- | --- |
| 使用发行包安装 Linux 服务 | [安装与服务管理](installation.md) |
| 从仓库构建自己的部署 | [源码构建与部署](source-build.md) |
| 设置局域网访问、HTTPS 或反向代理 | [配置与访问](configuration.md) |
| 准备隔离命令环境、GPU 或宿主工具 | [Linux 沙盒部署](../sandbox/linux.md) |
| 在另一台 Linux 机器执行命令 | [独立 Sandbox 服务](../sandbox/remote.md) |
| 更新版本、备份、恢复或重置账号 | [更新与备份恢复](maintenance.md) |

## 完成部署

安装后按[启动后的检查](installation.md#启动后的检查)验证页面、认证、模型与沙盒能力。管理员随后[连接模型](../models/README.md)、[配置沙盒资源](../sandbox/resources.md)，并为[用户组](../administration/users-and-groups.md)分配可用范围。

部署配置限定可用能力，用户授权在此范围内生效。遇到问题先查[故障排查](../troubleshooting.md)，命令环境故障查[沙盒检查](../sandbox/linux.md#检查与验证)。
