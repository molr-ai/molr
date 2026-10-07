import { useMemo } from 'react';
import type { CSSProperties } from 'react';
import { highlightJSON } from '../code-highlight';
import { useUILocale } from '../locale';

export type CodeBlockProps = {
  code: string;
  language?: 'json' | 'text';
  label?: string;
  wrap?: boolean;
  maxHeight?: CSSProperties['maxHeight'];
  className?: string;
};

export function CodeBlock({ code, language = 'text', label, wrap = true, maxHeight = '28rem', className = '' }: CodeBlockProps) {
  const { messages } = useUILocale();
  const tokens = useMemo(() => language === 'json' ? highlightJSON(code) : null, [code, language]);
  return <div className={`ui-code-block ${className}`} data-language={language} data-highlighted={!!tokens} data-wrap={wrap}>
    <pre role="region" aria-label={label ?? messages.codeBlock.label} tabIndex={0} style={{ maxHeight }}><code>{tokens ? tokens.map(token => token.kind ? <span className={`ui-code-token--${token.kind}`} key={token.offset}>{token.text}</span> : token.text) : code}</code></pre>
  </div>;
}
