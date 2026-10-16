# 飞书渠道

固定适配器位于 `runtime/internal/im/feishu`，使用官方 Go SDK v3.12.2 的 WebSocket 客户端。应用注册协议以该版本 `scene/registration` 为依据，由有超时、响应大小限制和域名限制的 HTTP 客户端执行。用户操作见[飞书聊天入口](https://molr.ai/zh/docs/user/chat-channels/feishu/)。

## 身份和生命周期

一个应用只绑定一个账号下的一位助理。绑定保存应用 ID、应用内 Open ID、首次有效消息的租户标识及代际 ID，App Secret 用主密钥 AES-GCM 加密，AAD 包含账号、绑定和应用。每次入站要求应用、发送者和租户匹配，只接受 `user` 与 `p2p`。组消息及机器人回流被忽略。

连接、解绑和发送按账号与助理串行；绑定写入事务内复查权限与另一渠道的排他约束。入站通过 `ReceiveChannel` 再次授权，不生成用户 Session 或 Token。WebUI、SDK 的普通消息写入在绑定期间被拒绝。失效代际不能恢复连接或写入；取消的扫码结果不能完成绑定。未完成扫码仅保存在内存，重启后需重试。

Runtime 管理长连接，连接恢复与中断同步 `feishu.changed` 及统一通知。停用账号或助理后停止 worker，每次收发仍检查实时权限。关闭应用时取消并等待所有渠道 worker，然后关闭存储。

## 持久化与消息

私有集合为 `feishu.bindings`、`feishu.incoming`、`feishu.deliveries`。入站按应用、租户与平台 `message_id` 去重，记录不依赖绑定代际，防止重新连接后的延迟事件重放。聊天消息、去重记录和租户绑定在同一事务提交；事务失败允许平台重推。接收处理限制为两秒，不等待模型执行。

助理显式 Publish 时同事务创建投递。文本按 UTF-8 边界拆为最多 12000 字节；只投递绑定后的新消息。每段采用固定 UUID，成功要求合法业务成功码及服务端消息 ID。未知结果与限流最多尝试五次，等待一至四分钟；首发后 55 分钟停止重试，预留平台一小时幂等窗口余量。崩溃遗留 `sending` 在窗口内恢复到 `pending`，窗口外记为 `unknown`。同一绑定按会话序号和分段顺序投递；解绑取消待发记录。

附件与卡片发布提供文字和登录后的 Molr 链接。入站富文本提取文字，其他类型标注未读取，不自动下载附件。`chat_channel` 自动上下文说明能力边界，不把模型私有流转发至飞书。

## 验证范围

协议测试使用人工 HTTP 响应验证权限请求、消息 JSON、固定 UUID 和错误分类；SQLite 测试覆盖入站过滤、重复消息、事务回滚、撤权、扫码取消及重启投递恢复。WebUI 测试使用独立 Runtime 和模拟飞书响应。真实租户扫码、管理员审批、发布可用范围及飞书客户端展示仍需真实账号验收。

参考：[官方 SDK](https://github.com/larksuite/oapi-sdk-go/tree/v3.12.2)、[接收消息](https://open.feishu.cn/document/server-docs/im-v1/message/events/receive)、[发送消息](https://open.feishu.cn/document/server-docs/im-v1/message/create)。
