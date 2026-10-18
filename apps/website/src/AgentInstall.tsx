import { useEffect, useState } from 'react';
import { Button, copyTextToClipboard } from '@molr/ui';
import type { Locale } from './site-content';
import type { RuntimeMode } from './release-data';
import { installMessages } from './install-content';
import { Icon } from './HomeIcon';

export const agentInstallPath = '/docs/admin/deployment/agent-install/';
export const agentInstallMarkdown = { zh: '/agent-install.md', en: '/en/agent-install.md' } as const;

export function agentInstallPrompt(locale: Locale, mode: RuntimeMode = 'server') {
  const guide = `https://molr.ai${agentInstallMarkdown[locale]}`;
  return locale === 'zh'
    ? `请帮我在目标设备上安装正式渠道最新版 Molr，以 --mode ${mode} 安装并启动 Runtime。
先读取官方安装说明：${guide}
确认你能操作实际要安装的设备，检查环境和已有 Molr 实例，然后按说明完成安装与验证。目标设备不明确时先问我。
没有适用安装包时告诉我，并按官方文档说明可选方案。涉及需要额外授权的系统变更时，说明具体操作。
${mode === 'sandbox' ? '此设备用于给已有 Molr 添加沙盒节点，只启动资源服务。完成后给我节点地址、连接方法、安装版本和能力验证结果；安全保存节点令牌。' : '此设备运行完整 Molr 服务。完成后给我访问地址、一次性登录链接、安装版本和验证结果。'}尚未完成的步骤请明确说明。`
    : `Please install the latest stable Molr release on the target device. Install and start the Runtime with --mode ${mode}.
First read the official installation guide: ${guide}
Confirm that you can operate the actual target device, inspect the environment and any existing Molr instance, then follow the guide to install and verify it. Ask me if the target device is unclear.
If there is no compatible installer, tell me and explain the alternatives in the official guide. Explain any system changes that need additional authorization.
${mode === 'sandbox' ? 'This device adds a sandbox node to an existing Molr server and runs only the resource service. When finished, provide the node address, connection instructions, installed version, and capability verification results. Store the node token securely.' : 'This device runs the complete Molr service. When finished, give me the access address, a one-time login link, installed version, and verification results.'} Clearly identify any unfinished steps.`;
}

export function AgentInstall({ locale, mode }: { locale: Locale; mode: RuntimeMode }) {
  const t = installMessages[locale];
  const prompt = agentInstallPrompt(locale, mode);
  const [status, setStatus] = useState<'idle' | 'copied' | 'error'>('idle');
  useEffect(() => {
    if (status !== 'copied') return;
    const timer = window.setTimeout(() => setStatus('idle'), 2000);
    return () => window.clearTimeout(timer);
  }, [status]);
  async function copy() {
    try { await copyTextToClipboard(prompt); setStatus('copied'); }
    catch { setStatus('error'); }
  }
  return <div className="home-install-details">
    <div className="home-install-heading"><h3>{t.agentTitle}</h3></div>
    <p>{t.agentBody}</p>
    <div className="home-install-prompt" role="group" aria-label={t.agentPromptLabel}>
      <div className="home-install-snippet-toolbar">
        <span><Icon name="bot" />{t.agentPromptLabel}</span>
        <Button variant="ghost" size="sm" onClick={copy}><Icon name={status === 'copied' ? 'check' : 'copy'} />{status === 'copied' ? t.copied : t.agentCopy}</Button>
      </div>
      <p className="home-install-prompt-text">{prompt}</p>
      <span className={status === 'error' ? 'home-install-copy-error' : 'ui-sr-only'} role="status">{status === 'copied' ? t.copied : status === 'error' ? t.agentCopyError : ''}</span>
    </div>
  </div>;
}
