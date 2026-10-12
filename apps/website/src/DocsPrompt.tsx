import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Button, UIIcon } from '@molr/ui';
import type { Locale } from './site-content';

function copyOnHTTP(text: string) {
  const active = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const selection = window.getSelection();
  const ranges = selection ? Array.from({ length: selection.rangeCount }, (_, i) => selection.getRangeAt(i).cloneRange()) : [];
  const textarea = document.createElement('textarea');
  textarea.className = 'docs-copy-buffer';
  textarea.value = text;
  textarea.readOnly = true;
  document.body.append(textarea);
  try {
    textarea.select();
    if (!document.execCommand('copy')) throw new Error('Copy unavailable');
  } finally {
    textarea.remove();
    active?.focus({ preventScroll: true });
    if (selection) { selection.removeAllRanges(); ranges.forEach(range => selection.addRange(range)); }
  }
}

export function DocsPrompt({ children, locale }: { children: ReactNode; locale: Locale }) {
  const content = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState<'idle' | 'copied' | 'error'>('idle');
  const t = (zh: string, en: string) => locale === 'zh' ? zh : en;
  useEffect(() => {
    if (status === 'idle') return;
    const timer = window.setTimeout(() => setStatus('idle'), 2500);
    return () => window.clearTimeout(timer);
  }, [status]);
  async function copy() {
    const text = content.current?.innerText.trim();
    if (!text) return;
    try {
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(text);
      else copyOnHTTP(text);
      setStatus('copied');
    } catch { setStatus('error'); }
  }
  return <div className="docs-prompt">
    <div className="docs-prompt-header"><strong><UIIcon name="sparkles" />{t('示例 Prompt', 'Example prompt')}</strong>
      <Button variant="ghost" size="sm" onClick={copy}><UIIcon name={status === 'copied' ? 'check' : 'copy'} />{status === 'copied' ? t('已复制', 'Copied') : t('复制', 'Copy')}</Button>
    </div>
    <div ref={content} className="docs-prompt-content">{children}</div>
    <span className={status === 'error' ? 'docs-copy-error' : 'ui-sr-only'} role="status">{status === 'copied' ? t('已复制提示词', 'Prompt copied') : status === 'error' ? t('复制失败，请选中文字手动复制。', 'Copy failed. Select the text and copy it manually.') : ''}</span>
  </div>;
}
