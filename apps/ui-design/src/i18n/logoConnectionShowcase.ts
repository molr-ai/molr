const zh = {
  title: '让 Logo 表达连接状态',
  description: '日常保持灵动，重连时巡线等待，离线时安静淡出，恢复时轻轻回应。',
  controls: 'Logo 连接状态',
  states: { connected: '已连接', reconnecting: '正在重连', disconnected: '已断开', failed: '重连失败', recovered: '连接恢复' },
  details: {
    connected: '沿用昼夜轮换，保持正常品牌颜色。',
    reconnecting: '复用 loading 动画：轮廓持续巡线，直到连接恢复。',
    disconnected: '停止动画，保留原色并降低至 24% 不透明度，与 loading 底轨一致。',
    failed: '轻轻左右摇动一次后停止，保持 24% 不透明度。连接未恢复，可点击重试。',
    recovered: '复用招呼动画，轻轻点头、眨眼一次，2.4 秒后回到昼夜轮换。',
  },
  play: '播放连接过程', playFailure: '演示重连失败', retry: '重试连接', stop: '停止演示', replay: '重播当前状态', reduceMotion: '减少动态效果',
  light: '浅色背景', dark: '深色背景', navigation: '导航尺寸 · 44 × 44',
  wordmark: '完整 Logo', mark: '独立 M',
  hint: '悬停或聚焦 Logo 可查看状态。减少动态效果时仍保留 loading 状态动画，其余动画使用静态图案。',
  preview: '设计预览，可手动切换状态；不代表当前服务连接情况。',
  logoLabel: (state: string) => `Molr，${state}`,
};
const en: typeof zh = {
  title: 'Connection states, expressed by the logo',
  description: 'Lively when connected, tracing while reconnecting, quiet when offline, and a small greeting on return.',
  controls: 'Logo connection state',
  states: { connected: 'Connected', reconnecting: 'Reconnecting', disconnected: 'Disconnected', failed: 'Reconnection failed', recovered: 'Connection restored' },
  details: {
    connected: 'Keep the daily animation and normal brand colors.',
    reconnecting: 'Reuse the loading animation: trace the outline until the connection returns.',
    disconnected: 'Stop the animation and keep the original colors at 24% opacity, matching the loading track.',
    failed: 'A single gentle shake, then remain still at 24% opacity. The connection is still unavailable; retry when ready.',
    recovered: 'Reuse the greeting: one gentle nod and blink, then return to the daily animation after 2.4 seconds.',
  },
  play: 'Play connection sequence', playFailure: 'Preview reconnection failure', retry: 'Retry connection', stop: 'Stop preview', replay: 'Replay current state', reduceMotion: 'Reduce motion',
  light: 'Light background', dark: 'Dark background', navigation: 'Navigation size · 44 × 44',
  wordmark: 'Full logo', mark: 'M symbol',
  hint: 'Hover or focus the logo to read its status. Reduced motion keeps the loading indicator running and makes other animations static.',
  preview: 'Design preview with manual state controls; this does not reflect the live service connection.',
  logoLabel: state => `Molr, ${state}`,
};
export const logoConnectionShowcaseMessages = { zh, en };
