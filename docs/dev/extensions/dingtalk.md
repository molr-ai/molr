# 钉钉渠道

固定 Go 适配器位于 `runtime/internal/im/dingtalk`，通过钉钉 Stream 接收机器人单聊，使用应用 access token 调用单聊发送接口。用户步骤见[钉钉聊天入口](../../user/chat-channels/dingtalk.md)。协议依据[官方 Stream 描述](https://open-dingtalk.github.io/developerpedia/docs/learn/stream/protocol/)、[消息结构](https://open-dingtalk.github.io/developerpedia/docs/learn/bot/message/)和[官方渠道 SDK](https://github.com/DingTalk-Real-AI/dingtalk-channel-sdk-go)，核查日期为 2026-10-16。

## 绑定与信任边界

每个应用在实例内只能绑定一位助理。配对记录保存随机绑定代际、应用 ID、十分钟有效的配对码摘要和 AES-GCM 加密凭据；AAD 包含账号和绑定 ID。状态 DTO 不包含凭据、令牌和配对码。配对码只在创建响应返回，不进入聊天记录或模型上下文，重复推送配对消息也会去重。过期未配对记录由 worker 清理。

仅 `conversationType=1`、机器人 ID 匹配、发送企业与机器人企业相同且含员工 ID 的消息可参与配对。绑定后同时核对企业、员工 ID、发送者 ID 和单聊会话 ID。配对在消息事务内重新检查用户与助理状态，以及微信、Telegram 和 Slack 的入口互斥条件；各渠道也检查钉钉已绑定或未过期配对的占用。撤销、重新连接及迟到回调通过绑定代际隔离。

入站复用可信 `conversation.ReceiveChannel` 和附件 `files.ChannelStore`，不签发用户会话或 API Token。普通 WebUI/SDK/插件入站经过两个渠道的 `CheckWebInput`。消息与去重记录在同一事务提交，文件下载在事务外执行，文件提交和聊天关联分别复查绑定。

## 传输与投递

使用已有 `golang.org/x/net/websocket` 实现限定的 Stream 消息和心跳协议。连接地址限制为钉钉 WSS、443 端口；HTTP 和 WebSocket 拨号复用公网 IP 校验，禁止凭据随重定向转发。帧上限 1 MiB，回调队列最多 32 条，单独处理心跳；工作线程取消后等待回调退出再关闭数据库。队列溢出断开重连，不承诺机器人消息具备服务端补发保证。

`Published` 在消息事务内写入独立 `dingtalk.deliveries`，不竞争调度器 IM outbox。文字按 UTF-8 边界每段最多 3500 字节，每个附件单独投递。图片也通过文件消息发送，避免暴露 Molr 文件 URL。卡片仅转文字和登录链接，不注册卡片回调。

出站先持久化 `sending`。响应必须提供 `processQueryKey` 且接收者不在无效或限流名单中才标记 `accepted`。429/明确限流最多尝试五次，等待至少一分钟及平台 Retry-After；超时、5xx、不完整响应标记 `unknown`，不自动重发。重启将遗留 `sending` 转为 `unknown`。凭据错误暂停连接；解绑取消尚未完成的投递。发送和解绑按账号、助理串行化，已进入网络的请求不能撤回。

应用 access token 按凭据缓存并在到期前刷新。附件使用机器人文件下载接口返回的 HTTPS 地址，仅接受钉钉或阿里云域名、公网解析地址，禁止重定向，不携带应用 token。渠道文件上限取 Runtime 配置与 20 MiB 的较小值。上传或本地读取失败与最终发送未知结果分别记录。必要的连接和投递异常接入统一通知模块。

## API

| 方法和路径 | 请求 | 响应 |
| --- | --- | --- |
| `GET /api/assistants/{id}/dingtalk` | 助理 ID | `Channel`、`State`、`ClientID`、安全错误码和时间字段 |
| `POST /api/dingtalk/connect` | `AssistantID`、`ClientID`、`ClientSecret` | `Code`、`ExpiresAt` |
| `POST /api/dingtalk/disconnect` | `AssistantID` | 空结果；取消配对或解绑 |

这些接口沿用已认证账号、助理授权、CSRF 和 HTTP 限流。已配对连接必须先解绑才能更换凭据。未配对连接不锁定 WebUI。实时 `dingtalk.changed` 事件刷新入口状态，模型请求通过 `chat_channel` 上下文了解文字、附件和审批边界。

## 验证边界

Go 测试覆盖身份、群消息拒绝、配对过期与取消、消息去重、文件隔离、未知投递、限流、心跳与关闭；WebUI 测试模拟平台回调，验证连接、入口锁定和解绑，以及六套配色的明暗模式和桌面/窄屏。真实组织的权限、机器人搜索、客户端文件展示与长期主动消息必须使用测试账号验收；可控协议测试不能代替这些平台行为。
