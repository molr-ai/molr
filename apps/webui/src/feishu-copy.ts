export const feishuCopy = {
 zh: {
  name: '飞书聊天', connect: '连接飞书', scan: '使用飞书扫码授权', manual: '使用已有应用', save: '连接应用', cancel: '取消连接', refresh: '刷新二维码', disconnect: '解绑飞书', manage: '管理飞书连接', readonly: '请通过飞书发送消息', readonlyHelp: '或解绑飞书以启用此输入框',
  help: '在飞书私聊中与这位助理交流。当前支持文字，图片、文件、群聊和交互卡片暂不支持。', privacy: '此入口的聊天内容会传输至飞书。授权后保留现有记录与上下文，聊天输入切换至飞书。',
  appID: 'App ID', secret: 'App Secret', openID: '你的 Open ID', manualHelp: '使用已启用机器人能力的飞书自建应用。开通单聊消息读取和机器人发送权限，订阅接收消息事件并选择长连接。Open ID 必须属于此应用。连接建立后，在飞书后台保存订阅方式并发布应用。',
  disconnectTitle: '解绑并恢复 WebUI 聊天？', disconnectHelp: '保留历史记录和上下文，取消尚未发送的回复。已被飞书接受的消息无法撤回；飞书应用仍保留。', open: '在飞书授权页面打开', firstMessage: '连接建立后，请在飞书向机器人发送文字消息。',
  delivery: '飞书消息投递', pending: '发送暂未确认，正在使用相同消息编号重试。', failed: '飞书消息发送失败，请检查应用权限和可用范围。', unknown: '发送结果未知，已停止重试。请在飞书核对是否收到。',
  states: { disconnected: '未连接', connecting: '正在建立连接', connected: '已连接', reconnecting: '正在自动重连', credentials_required: '需要重新连接应用', paused: '已暂停', waiting: '等待飞书授权', confirmed: '应用已连接', expired: '二维码已过期', denied: '已取消授权', unsupported_tenant: '当前仅支持飞书租户' },
 },
 en: {
  name: 'Feishu chat', connect: 'Connect Feishu', scan: 'Scan with Feishu to authorize', manual: 'Use an existing app', save: 'Connect app', cancel: 'Cancel connection', refresh: 'Refresh QR code', disconnect: 'Disconnect Feishu', manage: 'Manage Feishu connection', readonly: 'Send messages in Feishu', readonlyHelp: 'Or disconnect Feishu to enable this message box',
  help: 'Chat privately with this assistant in Feishu. Text is supported; images, files, group chats and interactive cards are not supported yet.', privacy: 'Chat content on this channel is transmitted to Feishu. Authorization preserves history and context and switches chat input to Feishu.',
  appID: 'App ID', secret: 'App Secret', openID: 'Your Open ID', manualHelp: 'Use a Feishu custom app with a bot. Enable private message reading and bot sending permissions, subscribe to receive-message events and select a long connection. Your Open ID must belong to this app. Once connected, save the subscription method in Feishu and publish the app.',
  disconnectTitle: 'Disconnect and restore WebUI chat?', disconnectHelp: 'History and context are preserved, and unsent replies are canceled. Messages accepted by Feishu cannot be recalled. The Feishu app is retained.', open: 'Open Feishu authorization', firstMessage: 'Once connected, send the bot a text message in Feishu.',
  delivery: 'Feishu delivery', pending: 'Delivery is unconfirmed; retrying with the same message ID.', failed: 'Feishu rejected the message. Check app permissions and availability.', unknown: 'Delivery is unconfirmed and retries have stopped. Check whether it arrived in Feishu.',
  states: { disconnected: 'Not connected', connecting: 'Connecting', connected: 'Connected', reconnecting: 'Reconnecting automatically', credentials_required: 'Reconnect the app', paused: 'Paused', waiting: 'Waiting for Feishu authorization', confirmed: 'App connected', expired: 'QR code expired', denied: 'Authorization canceled', unsupported_tenant: 'Only Feishu tenants are supported' },
 },
};
