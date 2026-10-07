import type { Locale } from './site-content';

const zh = {
  platform: '安装到', soon: '即将推出', selected: '已选择',
  browser: '在设备或服务器上安装，在电脑和手机的浏览器中使用。',
  method: '安装方式', command: '命令安装', download: '下载安装包', recommended: '推荐',
  commandTitle: '在终端里，开始使用 Molr。',
  commandBody: '在目标 Linux 设备上运行安装脚本，自动识别架构、安装程序并配置服务。',
  commandPending: '安装命令即将推出', copyCommand: '复制安装命令',
  steps: ['下载安装脚本', '运行脚本，完成配置', '打开访问链接，连接模型'],
  downloadTitle: '选择适合设备的安装包。',
  downloadBody: '下载安装包，在目标 Linux 设备上完成安装。程序与网页界面包含在同一个包中。',
  x64: '适用于 Intel / AMD 64 位设备', arm64: '适用于 ARM 64 位设备',
  downloadAction: '下载安装包', archive: 'Linux · tar.gz',
  requirements: 'Linux · systemd 254+ · x64 / ARM64', guide: '详细安装指南',
} as const;

type InstallCopy = { [K in keyof typeof zh]: K extends 'steps' ? readonly string[] : string };
export const installMessages: Record<Locale, InstallCopy> = {
  zh,
  en: {
    platform: 'Install on', soon: 'Coming soon', selected: 'Selected',
    browser: 'Install on your device or server. Use it in a browser on your computer or phone.',
    method: 'Installation method', command: 'Command line', download: 'Download package', recommended: 'Recommended',
    commandTitle: 'Start with Molr in your terminal.',
    commandBody: 'Run the installer on your Linux device to detect its architecture, install Molr and configure the service.',
    commandPending: 'Installation command coming soon', copyCommand: 'Copy command',
    steps: ['Download the installer', 'Run it and complete setup', 'Open the link and connect a model'],
    downloadTitle: 'Choose a package for your device.',
    downloadBody: 'Download a package and install it on your Linux device. The application and web interface come together.',
    x64: 'For Intel / AMD 64-bit devices', arm64: 'For ARM 64-bit devices',
    downloadAction: 'Download package', archive: 'Linux · tar.gz',
    requirements: 'Linux · systemd 254+ · x64 / ARM64', guide: 'Installation guide',
  },
};
