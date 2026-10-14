# 管理员故障排查

实例管理员先检查资源连接与用户组分配；涉及服务进程、配置文件和系统依赖时，由具备服务器访问权限的维护者处理。

## 按故障范围定位

| 问题 | 检查与操作 |
| --- | --- |
| 页面无法访问或版本不一致 | 核对服务状态、访问地址、代理和前后端版本，见[配置与访问](deployment/configuration.md) |
| 账号无法登录 | 核对账号状态、组分配和二次验证；需本机恢复时按[维护指南](deployment/maintenance.md#备份恢复与升级)操作 |
| 模型连接失败 | 检查提供商地址、凭证、模型能力与 Runtime 网络许可，见[模型连接](resources/models.md) |
| 成员没有资源或额度不足 | 核对用户组模型、工具、沙盒引用及配额，见[用户与用户组](administration/users-and-groups.md) |
| 命令环境不可用 | 按[沙盒检查](sandbox/linux.md#检查与验证)检查系统依赖与实际隔离能力 |
| 远程沙盒离线 | 核对远端服务、网络、证书指纹与令牌，见[独立沙盒服务](sandbox/remote.md) |
| 浏览器组件失败 | 查看组件任务状态与错误，按[组件管理](administration/components.md)更新或修复 |
| 微信连接或链接异常 | 核对外部网络和公开访问地址，见[微信连接配置](deployment/configuration.md#微信连接配置) |

## 诊断信息

安装、更新或重启后，按[启动后的检查](deployment/installation.md#启动后的检查)验证页面、认证和沙盒能力。端口监听或服务 active 状态不能代替能力检查。

需要执行详情时，由部署者启用 `debug.enabled`，再按[开发工作台](../dev/runtime/workbench.md)查看已采集记录。个人设置中的显示开关不能补回未采集的记录。模型响应问题可临时启用[诊断日志](deployment/maintenance.md#模型响应诊断日志)，收集后关闭，并在分享前审查敏感内容。
