import { useEffect, useState } from 'react';
import type { Locale } from './site-content';
import { installMessages } from './install-content';

export function InstallCommand({ command, locale, terminal = false }: { command: string; locale: Locale; terminal?: boolean }) {
  const t = installMessages[locale];
  const [copyFeedback, setCopyFeedback] = useState(0);
  const copied = copyFeedback > 0;
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    if (!copyFeedback) return;
    const timer = window.setTimeout(() => setCopyFeedback(0), 2000);
    return () => window.clearTimeout(timer);
  }, [copyFeedback]);
  const copy = async () => {
    try { await navigator.clipboard.writeText(command); setCopyFeedback(value => value + 1); setFailed(false); }
    catch { setCopyFeedback(0); setFailed(true); }
  };
  const action = copied ? t.copied : t.copyCommand;
  return <>
    {terminal ? <div className="home-terminal-window">
      <div className="home-terminal-titlebar">
        <span className="home-terminal-controls" aria-hidden="true"><i /><i /><i /></span>
        <span>Terminal</span><span className="home-terminal-shell" aria-hidden="true">sh</span>
      </div>
      <div className="home-terminal-command">
      <span className="home-terminal-prompt" aria-hidden="true">$</span>
      <pre><code>{command}</code></pre>
      <button type="button" onClick={copy} aria-label={action} title={action}>
        <svg className={copied ? 'home-terminal-copy-success' : undefined} width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          {copied ? <path d="m5 12 4 4L19 6" /> : <><rect x="8" y="8" width="12" height="13" rx="2" /><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3" /></>}
        </svg>
      </button>
      <span className="ui-sr-only" role="status">{copied ? t.copied : ''}</span>
      </div>
    </div> : <div className="home-install-terminal">
      <div><span>Terminal</span><button type="button" onClick={copy} aria-live="polite">{action}</button></div>
      <pre><code>{command}</code></pre>
    </div>}
    {failed && <p role="status">{t.copyError}</p>}
  </>;
}
