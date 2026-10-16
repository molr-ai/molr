# Slack 渠道

固定 Go 适配器位于 `runtime/internal/im/slack`，采用 Socket Mode 和 Web API；不加载常驻插件。用户配置见 [Slack 聊天入口](../../user/chat-channels/slack.md)。

## 身份与入口

`auth.test` 与 `bots.info` 核对 workspace、机器人和 App ID；Socket Mode 的 hello 必须匹配同一个 App。绑定固定 Molr 用户、助理、现有会话、Slack workspace、App、机器人、成员和私聊。创建绑定与配对确认均重新检查助理权限和其他入口占用。

待配对绑定五分钟有效，凭据经 AES-GCM 加密，AAD 绑定账号和绑定代际，配对码只存摘要。配对前 WebUI 可继续输入；只有指定成员发来的正确私聊配对码才切换入口。一个 App 或同一 workspace 的成员只能绑定一个助理。一个 App 不得同时供其他进程消费 Socket Mode。

每条入站校验 workspace、App、成员、私聊及消息类型，忽略机器人、群聊、编辑和删除事件。`ReceiveChannel` 和附件入口在事务内复查绑定及当前账号、助理状态；不创建用户 Session 或全权限 Token。微信、Telegram 与 Slack 的占用检查在绑定事务中执行。

## 持久接收与投递

私有 keyspace 为 `slack.bindings`、`slack.incoming`、`slack.deliveries` 及引用缓存。Socket Mode 事件先按绑定代际和 event ID 去重落库再 ACK，附件与助理执行不阻塞确认。后台将消息、完成记录、引用和可见事件同事务提交；完成后清除队列中的原始内容。解绑取消待处理消息和投递，旧绑定无法继续写入。

显式 Publish 在原消息事务创建独立投递记录，不消费调度器 IM Outbox。文字按 UTF-8 边界分段，每段最多 3500 字节；每个附件独立投递。同一绑定顺序发送，每轮最多一项，轮间至少一秒。发送前落库 sending，成功需完整 Slack `ok: true` 响应；文字还要求服务端 ts。超时、5xx、异常或不完整回包标记 unknown，不自动重发。重启将遗留 sending 标为 unknown。429 遵守 Retry-After 且至少一分钟，最多五次。通知与投递状态同事务保存。

Socket Mode 主动更新连接，网络失败复用共享退避策略；失效凭据或权限不足要求重新连接。账号禁用或助理解雇后停止收发，Runtime 退出时取消并等待 worker 后关闭数据库。

## 附件与引用

私有下载仅允许 HTTPS files.slack.com，鉴权头不跟随重定向；实际 DNS/IP 由现有公网网络守卫检查。上传采用 files.getUploadURLExternal、上传字节、files.completeUploadExternal。上传地址限定 Slack 域名且不携带 Bot Token。最终完成分享结果未知时不重试。

文件使用现有 ChannelStore，沿用大小、配额、归属和绑定复查。图片、文件、音频和视频均通过附件体系接收；音频直接供可用模型读取，不增加转写服务。未读取内容明确标记。

私聊 thread_ts 仅用于引用本地保存的父消息，不建立新会话或拉取历史。缓存按绑定代际隔离，最多 4096 条、30 天有效、每条文字最多 16 KiB；引用附件重新检查当前绑定权限。解绑清理缓存。卡片降级成文字和 Molr 链接，实时 chat_channel 上下文解释能力和投递边界。

## HTTP 入口与验证

- `GET /api/assistants/{id}/slack`：连接状态与最近 30 条投递，无凭据或配对码。
- `POST /api/slack/connect`：`AssistantID`、`AppToken`、`BotToken`、`UserID`；返回 `ID`、`Code`、`ExpiresAt`。
- `POST /api/slack/disconnect`：`AssistantID`；取消配对或解除已完成绑定。

运行 `go test ./internal/im/slack` 和 `go test ./internal/app -run 'Test(Slack|Telegram|Weixin)'` 检查协议、身份隔离、附件、去重和入口互斥。模拟测试不替代真实 Slack 安装、配对、客户端媒体显示和长连接验收。
