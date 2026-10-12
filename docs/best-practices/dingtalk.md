# 操作钉钉

通过钉钉官方 [DWS CLI](https://github.com/DingTalk-Real-AI/dingtalk-workspace-cli/blob/main/README_zh.md)，助理可以查询日程、搜索文档、查看钉盘文件，并在授权范围内执行协作操作。命令名为 `dws`。

## 安装与授权

先完成[执行环境准备](README.md#准备执行环境)，再把下面的委托发给助理：

> 按 https://github.com/DingTalk-Real-AI/dingtalk-workspace-cli 的官方说明，把 DWS CLI 和所需 Skills 安装到你的工作区，检查运行依赖。使用无浏览器环境的登录方式，把授权链接发给我；完成后查询今天的日程验证可用。

官方提供预编译文件及 npm 安装方式。由助理选择适合沙盒的方式，先验证程序能够运行，再登录：

```bash
dws --help
dws auth login --device
```

打开命令返回的链接，选择目标组织并授权。组织未开启 CLI 访问时，按页面提示申请，由企业管理员开启后重新登录。授权完成后执行一次只读查询：

```bash
dws calendar event list
```

## 可以这样委托

> 查看我今天的钉钉日程，按时间排序，列出会议时间和会前需要准备的材料。

> 搜索标题包含“季度复盘”的钉钉文档，整理最近一次复盘的结论和待办，并附上来源链接。

> 根据这份会议记录整理待办草稿，列出负责人和截止时间，等我确认后再创建到钉钉。

涉及多个组织时，说明本次使用的组织和账号。发送或写入任务按[委托范围](README.md#从一次小任务开始)交代目标。

## 验证与排查

日程查询成功且结果属于目标组织，即可继续具体任务；空日程也可能是正常结果。权限错误时检查组织 CLI 访问、账号资源权限和 Molr 网络授权。程序无法启动时，让助理检查二进制与沙盒的运行依赖是否匹配。

其他操作先查 `dws <命令> --help`，完整步骤以[官方使用说明](https://github.com/DingTalk-Real-AI/dingtalk-workspace-cli/blob/main/README_zh.md)为准。
