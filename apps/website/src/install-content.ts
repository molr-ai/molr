import type { Locale } from './site-content';

const zh = {
  loading: '正在读取发布信息…', empty: '即将推出', loadError: '暂时无法读取发布信息。', retry: '重试', notes: '更新说明', copied: '已复制', copyError: '复制失败，请选择并复制命令。',
  browser: '用桌面应用连接你的助理，或在自己的设备上部署 Molr 服务。',
  desktop: '桌面应用', server: '部署后端', sandbox: '部署沙盒',
  serverBody: 'Runtime 提供网页界面、账号、助理、任务和本机沙盒。安装后可通过桌面应用或浏览器连接。',
  sandboxBody: '已有 Molr 服务时，在其他设备上安装沙盒节点，提供文件与命令执行资源。节点不提供助理、账号或网页界面。',
  auxiliary: '安装方式',
  method: '安装入口', command: '命令安装',
  commandBody: '在 Linux 或 macOS 目标设备的终端运行。使用 curl 下载，自动检测系统与架构，安装正式渠道最新版。',
  copyCommand: '复制安装命令',
  agent: '让 Agent 为我安装',
  agentTitle: '把安装交给你的 Agent。',
  agentBody: '复制下面的指令，发送给能够操作目标设备终端的 Agent。它会检查环境、安装 Molr，并告诉你如何开始使用。',
  agentPromptLabel: '安装指令', agentCopy: '复制安装指令',
  agentCopyError: '复制失败，请选中文字手动复制。',
  agentGuide: '查看 Agent 安装说明',
  agentHint: '系统、架构和下载工具由 Agent 检测。它需要能够操作实际安装设备。',
  agentUnavailable: '可先复制指令，由 Agent 在目标设备重新检查发布信息。',
  agentEmpty: '安装包即将推出。可让 Agent 按安装指南检查可用方案。',
  guide: '详细安装指南',
} as const;

type InstallCopy = { [K in keyof typeof zh]: string };
export const installMessages: Record<Locale, InstallCopy> = {
  zh,
  en: {
    loading: 'Loading releases…', empty: 'Coming soon', loadError: 'Release information is temporarily unavailable.', retry: 'Retry', notes: 'Release notes', copied: 'Copied', copyError: 'Copy failed. Select and copy the command.',
    browser: 'Connect to your assistant with the desktop app, or deploy Molr on your own device.',
    desktop: 'Desktop app', server: 'Deploy backend', sandbox: 'Deploy sandbox',
    serverBody: 'The Runtime provides the web interface, accounts, assistants, tasks, and a local sandbox. Connect with the desktop app or a browser after installation.',
    sandboxBody: 'Add resources from another device to an existing Molr server. A sandbox node provides file access and command execution, without assistants, accounts, or a web interface.',
    auxiliary: 'Installation method',
    method: 'Installation entry', command: 'Command line',
    commandBody: 'Run in a Linux or macOS terminal on the target device. Use curl to download the installer, detect the OS and architecture automatically, and install the latest stable release.',
    copyCommand: 'Copy command',
    agent: 'Let an Agent install for me',
    agentTitle: 'Let your Agent handle installation.',
    agentBody: 'Copy these instructions to an Agent that can use the target device’s terminal. It will check the environment, install Molr, and tell you how to get started.',
    agentPromptLabel: 'Installation instructions', agentCopy: 'Copy installation instructions',
    agentCopyError: 'Copy failed. Select the text and copy it manually.',
    agentGuide: 'Read the Agent installation guide',
    agentHint: 'Your Agent detects the OS, architecture, and download tool. It needs access to the actual installation device.',
    agentUnavailable: 'You can copy the instructions now and let your Agent check releases on the target device.',
    agentEmpty: 'Installers are coming soon. Your Agent can check the installation guide for available options.',
    guide: 'Installation guide',
  },
};
