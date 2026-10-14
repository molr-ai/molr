# 操作知乎

通过[知乎数据开放平台](https://developer.zhihu.com/)的官方 `zhihu-cli`，助理可以搜索知乎与全网内容、获取热榜、调用知乎直答，以及读取当前账号的创作、关注和收藏。本指引围绕这些查询能力展开。

## 安装与授权

先完成[执行环境准备](https://molr.ai/zh/docs/user/best-practices/#准备执行环境)，再打开知乎开放平台的「Zhihu CLI」说明，取得官方安装指引。可以这样委托：

> [!PROMPT]
> 按 https://developer.zhihu.com/ 的官方 Zhihu CLI 和 Skill 说明安装到你的工作区。先检查可用命令，再引导我配置 Access Secret，最后用少量搜索结果验证接入。

登录[开放平台个人中心](https://developer.zhihu.com/profile)生成 Access Secret。桌面密钥链可用时，CLI 支持 `auth set --secret-stdin`；无系统密钥链的沙盒可使用进程环境变量 `ZHIHU_ACCESS_SECRET`。由助理按官方说明配置，避免将 Secret 放进命令参数或输出。实际 API 调用可能消耗开放平台额度，可在个人中心查看用量。

官方 Skill 安装器返回 CLI 的绝对路径。下面用 `zhihu-cli` 表示该程序；没有加入 PATH 时，由助理替换成安装结果中的绝对路径：

```bash
zhihu-cli capabilities
zhihu-cli auth status
zhihu-cli search zhihu --query '知识管理' --count 3
```

`auth status` 查看本地配置状态，搜索成功才验证了实际访问能力。

## 可以这样委托

> [!PROMPT]
> 在知乎搜索个人知识管理的实践经验，挑选三条不同思路，比较适用场景，保留标题、作者和原文链接。

<!-- -->

> [!PROMPT]
> 查看当前知乎热榜，选出与软件开发相关的话题，说明为什么值得进一步阅读，并给出来源。

<!-- -->

> [!PROMPT]
> 整理我最近的十条知乎收藏，按主题归类，给出一份阅读顺序建议。

需要对比观点和核实资料时，让助理使用搜索结果并保留来源；知乎直答适合快速取得综合答案。读取本人数据时，明确所需类型和数量。

## 验证与排查

本人内容查询使用 Access Secret 所属账号。创作列表中的摘要不是全文，近期收藏也不代表完整收藏历史。查询更多内容前先让助理确认当前命令的分页能力。

鉴权失败时检查 Secret 的来源和有效性；密钥链不可用时检查进程环境变量；额度或频率受限时按平台提示处理。使用 `zhihu-cli <命令> --help` 核对参数，能力范围以 `capabilities` 和[官方说明](https://developer.zhihu.com/)为准。
