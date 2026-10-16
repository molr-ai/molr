# 最佳实践

把具体任务交给助理，让它使用合适的工具完成查询、整理和协作。本章按使用场景介绍接入方式和委托示例。

## 选择场景

| 场景 | 接入方式 | 从这里开始 |
| --- | --- | --- |
| 浏览器 | 接入中 | [操作浏览器](browser.md) |
| 钉钉 | 官方 DWS CLI | [操作钉钉](dingtalk.md) |
| 飞书 | 官方 Lark CLI | [操作飞书](feishu.md) |
| ClawEmail 邮箱 | 官方 Mail-CLI（`mail-cli`，推荐） | [操作 ClawEmail](clawemail.md) |
| 知乎 | 官方 Zhihu CLI | [操作知乎](zhihu.md) |
| GitHub | 官方 GitHub CLI（`gh`） | [操作 GitHub](github.md) |
| Codex 编程任务 | 官方 Codex CLI | [操作 Codex](codex.md) |
| ZCode 编程任务 | Z.ai 官方 ZCode CLI | [操作 ZCode](zcode.md) |
| Google 服务 | Google Workspace CLI（`gws`） | [操作 Google 服务](google.md) |

## 准备执行环境

CLI 是命令行工具，助理通过 Shell 调用它访问对应平台。可以直接把各篇中的安装委托发给助理，由它按照官方文档安装程序、检查依赖并引导授权。

在助理权限页授予所需的计算和网络资源。CLI、依赖和需要保留的配置放在助理的 `/work` 内；官方安装器的默认位置可能需要调整。需要 Skill 时，将完整 Skill 目录放到 `/work/.agents/skills/`。操作入口见[插件与 Skills](../guides/plugins-and-skills.md)，工作区和终端用法见[文件与命令](../guides/files-and-commands.md)。

平台登录由你在官方授权页面完成。凭据按 CLI 支持的方式配置；不要把密钥写入 Skill、任务产物或可分享的脚本。沙盒内无法使用系统密钥链时，按该 CLI 的官方文档选择适用的凭据方式，并验证后续命令能读取。普通非交互命令不会加载 `/work/.profile`，不能只在某次终端中设置环境变量就假定后续调用也会继承。

## 从一次小任务开始

首次接入后，先让助理查询一条日程、一份指定文档或少量搜索结果。结果正确后，再组合成更完整的工作流程。

委托时说明目标账号或组织、资料范围、时间范围和交付格式。需要发送消息、修改记录或创建日程时，写清目标对象和执行范围；希望先审阅时，直接要求“先给我草稿，确认后再执行”。Molr 的资源授权和平台的账号权限都需要满足，排查入口见[权限与预算](../guides/permissions-and-budget.md)。
