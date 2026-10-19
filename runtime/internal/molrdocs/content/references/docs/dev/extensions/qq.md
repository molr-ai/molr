# QQ 渠道

固定 Go 适配器位于 `runtime/internal/im/qq`，通过 QQ 官方 WebSocket 网关接收 `C2C_MESSAGE_CREATE`，以 access token 调用单聊发送接口。用户操作见 [QQ 聊天入口](https://molr.ai/zh/docs/user/chat-channels/qq/)。

扫码协议核查于 2026-10-19，依据腾讯发布的 `@tencent-connect/qqbot-connector@1.2.0`；网关字段参考 `@tencent-connect/qqbot-nodejs@1.0.5` 和[官方 WebSocket 文档](https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/event-emit/websocket.html)。适配器直接实现协议，不安装或运行 OpenClaw 插件。

## 绑定与身份

Runtime 生成 32 字节随机密钥，通过 `https://q.qq.com/lite/create_bind_task` 获取任务 ID；浏览器仅获得本地登录 ID、二维码 URL 和有效期。`/lite/poll_bind_result` 返回机器人 App ID、加密 Secret 和可选 user OpenID。AES-256-GCM 验证并解密 Secret，密钥只保存在有界、短时的内存登录会话中。扫码会话以账号、助理和随机 ID 隔离，取消、过期、撤权及其他渠道占用在提交绑定前重新检查。

有 OpenID 时绑定扫码用户；缺失时及手动配置时，创建十分钟私聊配对码，仅保存摘要。配对成功前不锁定 WebUI；配对消息去重且不进入会话。机器人在实例内唯一，配对中的机器人占用渠道。其他发送者和非 C2C 事件被忽略。App Secret 使用包含渠道、账号、绑定 ID 和 App ID 的 AAD 加密保存，DTO、通知及错误不包含凭据。

## 收发与恢复

连接和队列通过 `channelworker` 的提交通知及截止时间驱动。网关实现鉴权、心跳、断线退避和 RESUME；仅在入站消息事务成功后推进序列。入站去重键包含 App ID 和远端消息 ID，解绑代际在事务中复核。

只有 Publish 创建投递。文字按最多 4000 UTF-8 字节分段，不拆分字符。当前附件及卡片转换为 Molr 链接；QQ 入站附件标记未读取，不下载。

官方单聊概述写明被动回复有效期 60 分钟、每条消息最多 4 次，但发送接口的 `msg_id` 字段仍写 5 分钟。适配器保守使用接收后四分钟内、最多四次被动回复，其余使用主动消息。每个绑定至少间隔三秒发送。主动消息仍受 QQ 的接收开关和平台限额控制，详见[消息规则](https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/overview.html)。

发送前持久化 `sending`；成功为 `accepted`。网络结果不明或重启遗留 `sending` 转为 `unknown`，不自动重复发送。明确限流按 Retry-After 等待，最多五次尝试。解绑与发送串行，取消未完成投递。文件及审批仍由 Molr 授权，QQ 的发布回执不代表用户已读。

## 接口与验证

`GET /api/assistants/{id}/qq` 返回当前状态。`POST /api/qq/login`、`poll`、`cancel`、`connect`、`disconnect` 分别管理扫码、轮询、取消、手动连接和解绑；请求使用 `AssistantID`，轮询和取消增加登录 `ID`，手动连接使用 `AppID` 和 `AppSecret`。接口沿用账号认证、助理授权、CSRF 和限流。

运行 `go test ./internal/im/qq ./internal/app ./internal/api`；WebUI 回归为 `npm run test:webui -- qq.spec.ts weixin.spec.ts telegram.spec.ts slack.spec.ts dingtalk.spec.ts feishu.spec.ts`。协议测试覆盖扫码解密及篡改、配对与去重、越权、撤销、渠道互斥、限流和未知结果；UI 测试使用模拟 QQ 响应，真实 QQ 扫码和消息展示需要用户在专用机器人上确认。
