# Slack 聊天入口

在助理的「切换聊天入口」中连接 Slack。连接后仅指定成员与机器人的私聊进入该助理；聊天历史与助理上下文保留，WebUI 继续用于查看、管理和审批。

## 准备 Slack 应用

为每个助理创建一个独立的 [Slack App](https://api.slack.com/apps)，安装到目标 workspace。同一 App 不要同时连接其他 Runtime 或程序，Socket Mode 会在连接间分配事件。

1. 在 Socket Mode 中启用连接。在 Basic Information 创建 App Token，授予 `connections:write`。
2. 在 OAuth & Permissions 添加 Bot Token Scopes：`im:history`、`chat:write`、`users:read`、`files:read`、`files:write`。
3. 在 Event Subscriptions 启用事件，订阅 Bot Event `message.im`。
4. 在 App Home 启用 Messages Tab，并允许用户发送消息。
5. 安装应用；修改权限后重新安装。保存 App Token 和 Bot User OAuth Token。
6. 在自己的 Slack 个人资料菜单选择「复制成员 ID」。

Runtime 需要出站访问 Slack HTTPS 和 WebSocket 服务，无需公网回调地址。协议说明见 [Socket Mode](https://docs.slack.dev/apis/events-api/using-socket-mode/)。

## 连接与使用

打开助理聊天页菜单，选择「切换聊天入口」。如已绑定微信或 Telegram，先解绑。在 Slack 区域填写两个 Token 和成员 ID，点击「连接 Slack」，然后使用指定账号将页面显示的完整配对码私聊发送给机器人。配对码五分钟有效，配对消息不会触发助理。

配对成功后，WebUI 输入框锁定；之前的聊天消息不补发到 Slack。可以关闭页面，Runtime 会继续收发。一个 Slack App 或同一 workspace 的成员不能重复绑定到其他助理。未配对时可以取消；配对码丢失后取消并重新连接。

- 文字、图片和普通文件双向支持；音频、视频按附件接收。语音是否可理解取决于助理的音频模型能力，不自动调用外部转写服务。
- 私聊线程回复可附带本机缓存的父消息和附件作为引用。所有消息仍属于同一个助理会话；引用缺失、过期或附件不可用时明确标记，不拉取 Slack 历史。回复发送到同一个私聊。
- 附件大小上限为管理员设置与 100 MiB 中较小值，默认 20 MiB。部分附件失败时保留文字及成功附件，并标记失败内容未读取。
- 任务、权限等卡片转为文字和需要登录的 Molr 链接。在 WebUI 中完成审批。

消息和附件会通过 Slack 传输和保存。绑定凭据加密保存，不进入助理上下文。

## 异常与解绑

网络中断时自动重连并显示通知，恢复后解除。凭据失效或缺少权限时，检查应用配置，解绑后重新连接。Slack 限流按响应要求等待，最多尝试五次。发送超时或没有完整确认时显示「投递结果未确认」，不会自动重发；先在 Slack 核对是否收到。

解绑后恢复 WebUI 输入，保留历史与上下文，取消未发送回复；已经发出的外部请求不能撤回。解雇助理暂停渠道，返聘后恢复。群聊、其他成员消息和机器人回流不会进入助理。
