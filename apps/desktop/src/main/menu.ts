import type { MenuItemConstructorOptions } from 'electron';

export function applicationMenu(locale: 'zh' | 'en', actions: { manager: () => void; reconnect: () => void }): MenuItemConstructorOptions[] {
  const t = (zh: string, en: string) => locale === 'zh' ? zh : en;
  return [
    { label: 'Molr', submenu: [
      { role: 'about', label: t('关于 Molr', 'About Molr') },
      { type: 'separator' },
      { role: 'services', label: t('服务', 'Services') },
      { type: 'separator' },
      { role: 'hide', label: t('隐藏 Molr', 'Hide Molr') },
      { role: 'hideOthers', label: t('隐藏其他应用', 'Hide Others') },
      { role: 'unhide', label: t('显示全部', 'Show All') },
      { type: 'separator' },
      { role: 'quit', label: t('退出 Molr', 'Quit Molr') },
    ] },
    { label: t('连接', 'Connections'), submenu: [
      { label: t('管理连接', 'Manage connections'), accelerator: 'CmdOrCtrl+Shift+M', click: actions.manager },
      { label: t('重新连接', 'Reconnect'), accelerator: 'CmdOrCtrl+R', click: actions.reconnect },
      { type: 'separator' },
      { role: 'quit', label: t('退出桌面应用（本机服务继续运行）', 'Quit desktop (local service keeps running)') },
    ] },
    { label: t('编辑', 'Edit'), submenu: [
      { role: 'undo', label: t('撤销', 'Undo') },
      { role: 'redo', label: t('重做', 'Redo') },
      { type: 'separator' },
      { role: 'cut', label: t('剪切', 'Cut') },
      { role: 'copy', label: t('复制', 'Copy') },
      { role: 'paste', label: t('粘贴', 'Paste') },
      { role: 'pasteAndMatchStyle', label: t('粘贴并匹配样式', 'Paste and Match Style') },
      { role: 'delete', label: t('删除', 'Delete') },
      { role: 'selectAll', label: t('全选', 'Select All') },
      { type: 'separator' },
      { label: t('替换', 'Substitutions'), submenu: [
        { role: 'showSubstitutions', label: t('显示替换', 'Show Substitutions') },
        { type: 'separator' },
        { role: 'toggleSmartQuotes', label: t('智能引号', 'Smart Quotes') },
        { role: 'toggleSmartDashes', label: t('智能破折号', 'Smart Dashes') },
        { role: 'toggleTextReplacement', label: t('文本替换', 'Text Replacement') },
      ] },
      { label: t('语音', 'Speech'), submenu: [
        { role: 'startSpeaking', label: t('开始朗读', 'Start Speaking') },
        { role: 'stopSpeaking', label: t('停止朗读', 'Stop Speaking') },
      ] },
    ] },
    { label: t('显示', 'View'), submenu: [
      { role: 'resetZoom', label: t('实际大小', 'Actual Size') },
      { role: 'zoomIn', label: t('放大', 'Zoom In') },
      { role: 'zoomOut', label: t('缩小', 'Zoom Out') },
      { role: 'togglefullscreen', label: t('切换全屏', 'Toggle Full Screen') },
    ] },
  ];
}
