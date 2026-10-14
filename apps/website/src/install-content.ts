import type { Locale } from './site-content';

const zh = {
  channel: '发布渠道', stable: '正式版', preview: '预发布版', nightly: 'Nightly',
  stableDescription: '推荐日常使用。', previewDescription: 'Beta / RC 版本，适合提前体验和验证。', nightlyDescription: '来自主分支的每日构建，适合参与测试。',
  loading: '正在读取发布信息…', empty: '该渠道暂无已发布版本，请选择其他渠道。', loadError: '暂时无法读取发布信息。', retry: '重试', notes: '更新说明', copied: '已复制', copyError: '复制失败，请选择并复制命令。', checksum: '下载校验文件', offline: '将安装包和同名 .sha256 校验文件放在同一目录，校验并解压后运行包内安装器：', targetHint: '请选择目标设备的系统与架构，在目标设备上运行命令。',
  system: '操作系统', architecture: '处理器架构', downloadTool: '下载工具', downloadAddress: '下载地址',
  browser: '在设备或服务器上安装，在电脑和手机的浏览器中使用。',
  method: '安装方式', command: '命令安装', download: '下载安装包', recommended: '推荐',
  commandTitle: '准备好，开始安装。',
  commandBody: '在目标设备的终端运行，安装所选渠道的最新版本。',
  copyCommand: '复制安装命令',
  downloadTitle: '下载并安装 Molr。',
  downloadBody: '程序与网页界面已包含在同一个安装包中。',
  downloadAction: '下载安装包',
  guide: '详细安装指南',
} as const;

type InstallCopy = { [K in keyof typeof zh]: string };
export const installMessages: Record<Locale, InstallCopy> = {
  zh,
  en: {
    channel: 'Release channel', stable: 'Stable', preview: 'Preview', nightly: 'Nightly',
    stableDescription: 'Recommended for everyday use.', previewDescription: 'Beta / RC releases for early testing.', nightlyDescription: 'Daily builds from main for development testing.',
    loading: 'Loading releases…', empty: 'No published release in this channel. Choose another channel.', loadError: 'Release information is temporarily unavailable.', retry: 'Retry', notes: 'Release notes', copied: 'Copied', copyError: 'Copy failed. Select and copy the command.', checksum: 'Download checksum', offline: 'Place the package and matching .sha256 file in the same directory. Verify and extract it, then run the included installer:', targetHint: 'Choose the OS and architecture of your target device. Run the command on that device.',
    system: 'Operating system', architecture: 'Architecture', downloadTool: 'Download tool', downloadAddress: 'Download URL',
    browser: 'Install on your device or server. Use it in a browser on your computer or phone.',
    method: 'Installation method', command: 'Command line', download: 'Download package', recommended: 'Recommended',
    commandTitle: 'Ready to install.',
    commandBody: 'Run in your target device’s terminal to install the latest release in this channel.',
    copyCommand: 'Copy command',
    downloadTitle: 'Download and install Molr.',
    downloadBody: 'The application and web interface, together in one package.',
    downloadAction: 'Download package',
    guide: 'Installation guide',
  },
};
