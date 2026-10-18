# Molr 管理员指南

本指南面向实例管理员和服务器维护者。WebUI 中的资源、成员和组件管理需要管理员账号；安装、配置文件、服务与备份操作需要服务器访问权限。

## 搭建实例

从[搭建 Molr 实例](https://molr.ai/zh/docs/admin/getting-started/)完成安装、初始化、资源接入与成员配置。

- [安装与服务管理](https://molr.ai/zh/docs/admin/deployment/installation/)：环境要求、发布渠道、指定版本、离线安装和服务生命周期。
- [让 Agent 为我安装](https://molr.ai/zh/docs/admin/deployment/agent-install/)：将安装交给能够操作目标设备的 Agent。
- [Docker 部署](https://molr.ai/zh/docs/admin/deployment/docker/)：在一个容器中运行 Runtime 与 WebUI，配置持久数据及可选命令执行。
- [源码构建与部署](https://molr.ai/zh/docs/admin/deployment/source-build/)：自行构建程序与发行包。
- [配置与访问](https://molr.ai/zh/docs/admin/deployment/configuration/)：局域网、HTTPS、模型网络与微信连接配置。

## 接入资源

- [模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)：支持方式对照、各提供商接入指南、订阅政策与替代方案。
- [MCP 工具](https://molr.ai/zh/docs/admin/resources/mcp/)：连接远程工具服务，发现工具并授权给助理。
- [管理沙盒资源](https://molr.ai/zh/docs/admin/resources/sandbox/)：连接执行机器，登记存储、计算与网络范围。
- [Linux 沙盒部署](https://molr.ai/zh/docs/admin/sandbox/linux/)、[独立沙盒服务](https://molr.ai/zh/docs/admin/sandbox/remote/)：系统依赖、隔离、GPU 与远程执行节点。

## 管理成员与扩展

- [用户与用户组](https://molr.ai/zh/docs/admin/administration/users-and-groups/)：账号、资源分配、配额与汇总统计。
- [插件管理](https://molr.ai/zh/docs/admin/administration/plugins/)：插件安装、共享连接与版本管理。
- [组件管理](https://molr.ai/zh/docs/admin/administration/components/)：浏览器组件的安装、代理、更新与修复。

## 维护与排查

[更新与备份恢复](https://molr.ai/zh/docs/admin/deployment/maintenance/)维护版本、持久数据和恢复流程；[管理员故障排查](https://molr.ai/zh/docs/admin/troubleshooting/)定位服务、认证、模型与沙盒问题。

准备好实例后，进入[使用指南](https://molr.ai/zh/docs/user/)管理自己的助理。实现机制与接口见[开发文档](https://molr.ai/zh/docs/dev/)。
