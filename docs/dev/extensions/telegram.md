# Telegram 渠道

固定适配器位于 `runtime/internal/im/telegram`，使用官方 [Bot API](https://core.telegram.org/bots/api)。用户操作见[Telegram 聊天入口](../../user/chat-channels/telegram.md)，接口见[HTTP API](../reference/http-api.md#telegram-聊天入口)。

后台连接和队列使用[统一的提交通知与截止时间调度](../architecture/overview.md#聊天渠道后台调度)，空闲时不轮询本地绑定或消息队列。

## 绑定与授权

每位助理绑定一个专属 Bot 和一个 Telegram 私聊用户。`getMe` 验证 Token，`getWebhookInfo` 检查已有接收入口，不自动删除其他服务的 Webhook。五分钟内的一次性 `/start` 随机码只识别候选用户，必须由持有助理写权限的已认证 WebUI 请求确认才建立绑定。待确认状态仅在内存中保存，取消或重启后失效。

绑定在事务中再次检查权限、助理状态、机器人唯一性和其他渠道。`app.chatChannels` 按当前绑定分派会话和附件授权、普通聊天写入限制及 Publish；发现多个绑定时拒绝操作。微信提交绑定时也检查 Telegram 冲突。

Bot Token 使用 AES-GCM 和包含渠道、账号与绑定 ID 的 AAD 加密。状态、通知、模型上下文和日志不包含 Token。Telegram URL 路径含 Token，HTTP 客户端只暴露固定错误码，不传播原始 URL 或服务端错误描述。

## 接收与投递

每个 Bot 一个 worker，使用 20 秒长轮询，匹配绑定用户 ID、Chat ID、私聊类型且无话题 ID。其他消息不会触发助理。附件在事务外下载，复用 `files.ChannelStore` 保存；最终事务再次验证绑定，原子写入消息、去重记录和下一轮 offset。轮询不等待模型执行。相册按消息处理，不做合并；编辑事件不订阅。

只有助理 Publish 创建投递记录，和聊天消息处于同一事务，不消费调度器的 IM Outbox。纯文本按不超过 4000 个 UTF-16 单元分段；每个附件单独投递，记录发送成功后的 Telegram message ID。发送前持久化 `sending`；成功为 `accepted`，超时或无完整响应为 `unknown`。进程启动将遗留 `sending` 转为 `unknown`，不重发结果未知的消息。

同一绑定按消息和分段顺序每轮发送一条，每轮至少间隔一秒；跨绑定发送至少间隔 40 毫秒。429 按 `retry_after` 等待，缺失时等待一分钟，总计最多五次。401 停止并要求换 Token；409 停止以避免与其他服务争抢；403 标记受阻，收到用户新消息后恢复。网络轮询失败复用指数退避，最长 640 秒。解绑与发送串行，解绑取消未完成投递；生命周期等待全部渠道 worker 退出后关闭数据库。

## 文件与网络

下载不超过 20 MB，发送不超过 50 MB，均取部署上传限制的较小值。符合格式与大小条件的 JPEG/PNG 使用 `sendPhoto`，其他文件使用 `sendDocument`。下载地址固定为 Telegram HTTPS origin，拒绝不安全文件路径及重定向。所有请求均有时间和响应字节上限。文件下载失败保留可用文字并标记未读取。

默认使用现有的公网地址校验客户端，忽略环境代理。管理员可通过 `telegram.proxy_url` 显式指定 HTTP(S) 代理，禁止 URL 凭据、路径、查询及片段；代理是部署者信任的网络出口，目标 TLS 仍验证 Telegram。机器人管理接口不允许修改代理或 API 地址。

## 验证

在 Runtime 目录运行 `go test ./internal/im/telegram ./internal/im/weixin ./internal/app ./internal/api ./internal/config`。测试覆盖浏览器确认、取消和过期、渠道互斥、越权、入站去重与游标回滚、附件、撤销绑定、限流、未知发送、重启恢复及敏感信息脱敏。WebUI 测试通过模拟渠道接口检查绑定、锁定和解绑；真实 Telegram 展示和网络可达性需要专用机器人验收。
