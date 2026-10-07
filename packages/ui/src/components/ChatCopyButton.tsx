import { useEffect, useRef, useState } from 'react';
import { useUILocale } from '../locale';
import { UIIcon } from './icons';

// LAN HTTP pages do not expose the Clipboard API. Keep a synchronous fallback
// inside the click gesture and restore the user's focus and selection afterward.
function copyLegacy(text: string) {
  const active = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const selection = window.getSelection();
  const ranges = selection ? Array.from({ length: selection.rangeCount }, (_, index) => selection.getRangeAt(index).cloneRange()) : [];
  const input = document.createElement('textarea');
  input.value = text;
  input.readOnly = true;
  input.style.cssText = 'position:fixed;left:0;top:0;opacity:0;pointer-events:none;font-size:16px';
  document.body.append(input);
  try {
    input.select();
    if (!document.execCommand('copy')) throw new Error('Copy unavailable');
  } finally {
    input.remove();
    active?.focus({ preventScroll: true });
    if (selection) { selection.removeAllRanges(); ranges.forEach(range => selection.addRange(range)); }
  }
}

export function ChatCopyButton({ text }: { text: string }) {
  const { messages } = useUILocale();
  const [status, setStatus] = useState<'idle' | 'copied' | 'error'>('idle');
  const [pending, setPending] = useState(false);
  const generation = useRef(0);
  useEffect(() => { generation.current++; setStatus('idle'); setPending(false); return () => { generation.current++; }; }, [text]);
  useEffect(() => {
    if (status === 'idle') return;
    const timer = setTimeout(() => setStatus('idle'), 2500);
    return () => clearTimeout(timer);
  }, [status]);
  async function copy() {
    if (pending) return;
    const current = generation.current;
    setPending(true);
    try {
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(text);
      else copyLegacy(text);
      if (generation.current === current) setStatus('copied');
    } catch {
      if (generation.current === current) setStatus('error');
    } finally {
      if (generation.current === current) setPending(false);
    }
  }
  const label = status === 'copied' ? messages.chat.copied : messages.chat.copyOriginal;
  return <>
    <button type="button" className="message-copy-button" aria-label={label} title={label} aria-disabled={pending} onClick={copy}><UIIcon name={status === 'copied' ? 'check' : 'copy'} /></button>
    <span role="status" className={status === 'error' ? 'message-copy-error' : 'ui-sr-only'}>{status === 'copied' ? messages.chat.copied : status === 'error' ? messages.chat.copyFailed : ''}</span>
  </>;
}
