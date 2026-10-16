# 操作微博

通过微博开放平台的官方 [Weibo CLI](https://open.weibo.com/cli/quickstart)，助理可以查询微博内容、读取评论，以及发布微博和回复评论。助理通过 Shell 调用 `weibo-cli`，可用操作取决于账号授权和已开通的服务。

## 安装与授权

先完成[执行环境准备](https://molr.ai/zh/docs/user/best-practices/#准备执行环境)，再把下面的委托发给助理：

> [!PROMPT]
> 按 https://open.weibo.com/cli/quickstart 的官方说明，将 @weibo-ai/weibo-cli 和依赖安装到你的工作区。检查版本和可用命令，引导我完成开发者认证、服务开通和设备码授权，把需要我打开的链接发给我。先读取当前账号信息验证接入。

官方 npm 包是 `@weibo-ai/weibo-cli`，需要 Node.js 18 或以上版本及 npm。将安装前缀指定到工作区，例如：

```bash
npm install --global --prefix /work/.local @weibo-ai/weibo-cli
/work/.local/bin/weibo-cli version
```

下面用 `weibo-cli` 表示安装后的程序；未加入 PATH 时使用 `/work/.local/bin/weibo-cli`。每次执行都需要能找到 Node.js，不能只依赖交互终端中的 PATH 设置。配置和登录凭据也应保留在助理工作区内，按当前 CLI 的帮助核对实际保存位置。

在[微博开放平台](https://open.weibo.com/cli)通过微博客户端完成开发者扫脸认证，再领取体验服务或购买正式积分包。服务范围、接口价格和调用额度以控制台当前显示为准；体验服务只开放部分能力，实际查询也可能消耗额度。

Molr 沙盒通常与用户浏览器不在同一台电脑，使用设备码登录：

```bash
weibo-cli auth login --device
weibo-cli doctor
weibo-cli me --output table
```

在自己的浏览器中打开 CLI 返回的官方授权链接，按提示输入设备码并确认账号。完成后让助理检查认证和服务状态；不要将访问令牌或刷新令牌粘贴到聊天中。

## 可以这样委托

> [!PROMPT]
> 在微博搜索本周与我指定产品有关的讨论，整理少量有代表性的反馈，保留作者、时间和原帖链接。

<!-- -->

> [!PROMPT]
> 读取我指定微博的最近十条评论，归纳问题并起草回复，先给我确认目标评论和完整回复内容。

<!-- -->

> [!PROMPT]
> 根据这份产品更新起草一条微博，先给我确认账号、正文和配图，再发布并返回微博 ID 和链接。

委托时说明目标账号、微博链接、时间范围和数量。发布 AI 生成内容时，按当前发布命令的参数声明 AI 生成属性。需要定期汇总或定时发布时，先验证一次对应操作，再通过[计划与跟进](https://molr.ai/zh/docs/user/guides/plans/)安排执行。

## 验证与排查

先查看当前账号可用的命令和参数，再执行一次只读查询：

```bash
weibo-cli commands list --available
weibo-cli commands show users show/biz
weibo-cli users show/biz --output json
```

核对返回的用户信息属于预期账号。`doctor` 或登录成功不能替代实际业务查询；搜索、评论和发布等能力应分别检查命令清单及所需参数。

| 现象 | 检查方式 |
| --- | --- |
| 找不到命令或 Node.js | 检查工作区安装路径及本次 Shell 的 PATH，使用 CLI 绝对路径 |
| 登录后仍无法调用 | 用 `doctor` 检查开发者认证、服务开通和额度，再核对目标命令是否可用 |
| 授权过期或被撤销 | 重新执行设备码登录，并确认使用的是预期账号 |
| 接口限频或积分不足 | 按平台提示等待或在控制台处理额度，减少查询范围和频次 |
| 发布或回复后超时 | 保留已返回的微博／评论 ID，先核实是否成功；结果不明时不要直接重发 |

升级后使用 `weibo-cli version`、`weibo-cli --help` 和 `commands show` 核对用法。停止使用时，在开放平台的授权列表撤销对应会话，并清理工作区中的相关凭据。完整安装与服务说明见[官方使用手册](https://open.weibo.com/cli/quickstart)。
