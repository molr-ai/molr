import { useEffect, useState } from 'react';
import { copyTextToClipboard } from '@molr/ui';
import type { Locale } from './site-content';
import { channels } from './release-data';
import type { Channel } from './release-data';
import { installMessages } from './install-content';
import { Icon } from './HomeIcon';

export const agentInstallPath = '/docs/admin/deployment/agent-install/';
export const agentInstallMarkdown = { zh: '/agent-install.md', en: '/en/agent-install.md' } as const;

export function agentInstallPrompt(locale: Locale, channel: Channel) {
  const guide = `https://molr.ai${agentInstallMarkdown[locale]}`;
  const release = `${installMessages[locale][channel]} (${channel})`;
  return locale === 'zh'
    ? `请帮我在目标设备上安装 Molr，使用「${release}」渠道。
先读取官方安装说明：${guide}
确认你能操作实际要安装的设备，检查环境和已有 Molr 实例，然后按说明完成安装与验证。目标设备不明确时先问我。
所选渠道没有适用版本时告诉我，由我选择其他渠道。涉及需要额外授权的系统变更时，说明具体操作。
完成后给我访问地址、一次性登录链接、安装版本和验证结果；尚未完成的步骤请明确说明。`
    : `Please install Molr on the target device using the ${release} channel.
First read the official installation guide: ${guide}
Confirm that you can operate the actual target device, inspect the environment and any existing Molr instance, then follow the guide to install and verify it. Ask me if the target device is unclear.
If this channel has no compatible release, tell me so I can choose another channel. Explain any system changes that need additional authorization.
When finished, give me the access address, a one-time login link, the installed version, and verification results. Clearly identify any unfinished steps.`;
}

export function AgentInstall({ locale, channel, onChannelChange }: { locale: Locale; channel: Channel; onChannelChange: (channel: Channel) => void }) {
  const t = installMessages[locale];
  const prompt = agentInstallPrompt(locale, channel);
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
    <div className="home-install-channels">
      <div className="home-install-options" role="group" aria-label={t.channel}>{channels.map(c => <button type="button" key={c} aria-pressed={channel === c} onClick={() => { setStatus('idle'); onChannelChange(c); }}>{t[c]}</button>)}</div>
      <p>{t[`${channel}Description`]}</p>
    </div>
    <div className="home-install-prompt" role="group" aria-label={t.agentPromptLabel}>
      <div className="home-install-snippet-toolbar">
        <span><Icon name="bot" />{t.agentPromptLabel}</span>
        <button type="button" onClick={copy}><Icon name={status === 'copied' ? 'check' : 'copy'} />{status === 'copied' ? t.copied : t.agentCopy}</button>
      </div>
      <p className="home-install-prompt-text">{prompt}</p>
      <span className={status === 'error' ? 'home-install-copy-error' : 'ui-sr-only'} role="status">{status === 'copied' ? t.copied : status === 'error' ? t.agentCopyError : ''}</span>
    </div>
  </div>;
}
