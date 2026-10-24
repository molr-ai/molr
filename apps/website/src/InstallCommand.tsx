import { Button, copyTextToClipboard } from '@molr/ui';
import { useEffect, useState } from 'react';
import type { Locale } from './site-content';
import { Icon } from './HomeIcon';
import { installMessages } from './install-content';

export function InstallCommand({ command, locale, label = "Terminal", copyLabel }: { command: string; locale: Locale; label?: string; copyLabel?: string }) {
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
    try { await copyTextToClipboard(command); setCopyFeedback(value => value + 1); setFailed(false); }
    catch { setCopyFeedback(0); setFailed(true); }
  };
  const action = copied ? t.copied : (copyLabel ?? t.copyCommand);
  return <>
    <div className="home-install-terminal">
      <div><span><Icon name="terminal" />{label}</span><Button variant="ghost" size="sm" onClick={copy} aria-live="polite"><Icon name={copied ? 'check' : 'copy'} />{action}</Button></div>
      <pre><code>{command}</code></pre>
    </div>
    {failed && <p role="status">{t.copyError}</p>}
  </>;
}
