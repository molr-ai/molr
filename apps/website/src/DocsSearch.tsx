import { useMemo, useRef, type KeyboardEvent } from 'react';
import { Dialog, EmptyState, UIIcon } from '@molr/ui';
import { searchDocuments, type MatchKind } from './docs-search';
import type { Doc } from './docs-content';
import type { Locale } from './site-content';

function Highlight({ text, query }: { text: string; query: string }) {
  const term = query.trim().toLowerCase();
  if (!term) return text;
  const parts = [];
  let start = 0;
  let index = text.toLowerCase().indexOf(term);
  while (index >= 0) {
    parts.push(text.slice(start, index), <mark key={index}>{text.slice(index, index + term.length)}</mark>);
    start = index + term.length;
    index = text.toLowerCase().indexOf(term, start);
  }
  return <>{parts}{text.slice(start)}</>;
}

export function DocsSearch({ open, onOpenChange, query, onQueryChange, items, locale }: {
  open: boolean; onOpenChange: (open: boolean) => void; query: string; onQueryChange: (query: string) => void;
  items: Doc[]; locale: Locale;
}) {
  const input = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLElement>(null);
  const results = useMemo(() => open ? searchDocuments(items, query) : [], [open, items, query]);
  const t = (zh: string, en: string) => locale === 'zh' ? zh : en;
  const labels: Record<MatchKind, string> = { title: t('标题匹配', 'Title match'), heading: t('章节匹配', 'Section match'), body: t('正文匹配', 'Body match') };
  function keyboard(event: KeyboardEvent) {
    if (event.nativeEvent.isComposing) return;
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onOpenChange(false); return; }
    const links = Array.from(list.current?.querySelectorAll<HTMLAnchorElement>('a') ?? []);
    const index = links.indexOf(document.activeElement as HTMLAnchorElement);
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const next = index < 0 ? (event.key === 'ArrowDown' ? 0 : links.length - 1) : (index + (event.key === 'ArrowDown' ? 1 : -1) + links.length) % links.length;
      links[next]?.focus();
    } else if (event.key === 'Enter' && event.target === input.current) {
      event.preventDefault(); links[0]?.click();
    }
  }
  return <Dialog open={open} onOpenChange={onOpenChange} title={t('搜索文档', 'Search documentation')}
    closeLabel={t('关闭搜索', 'Close search')} initialFocusRef={input} size="lg" className="docs-search-dialog"
    footer={<span className="docs-search-hint">{t('↑ ↓ 选择结果 · Enter 打开 · Esc 关闭', '↑ ↓ Select · Enter Open · Esc Close')}</span>}>
    <div onKeyDown={keyboard}>
      <label className="docs-search-field"><span className="ui-sr-only">{t('搜索文档', 'Search documentation')}</span><UIIcon name="search" />
        <input ref={input} type="search" autoComplete="off" maxLength={120} value={query} onChange={event => onQueryChange(event.target.value)} placeholder={t('输入关键词…', 'Enter keywords…')} />
      </label>
      <nav ref={list} className="docs-search-results" aria-label={t('搜索结果', 'Search results')}>
        {results.length ? <p role="status">{t(`找到 ${results.length} 篇文档`, `${results.length} documents found`)}</p> : <div role="status">
          <EmptyState className="docs-search-empty" illustration={<UIIcon name="search" />}
            title={query.trim() ? t('没有找到相关文档', 'No documents found') : t('输入关键词查找文档', 'Enter keywords to find documents')}
            description={query.trim() ? t('试试其他关键词。', 'Try another keyword.') : undefined} />
        </div>}
        {results.map(result => <a className="docs-search-result" key={result.doc.path} href={result.href} aria-label={result.doc.label}
          onClick={event => { if (!event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) onOpenChange(false); }}>
          <span className="docs-search-result-meta">{result.doc.group}{result.doc.label !== result.doc.title && <> / <Highlight text={result.doc.label} query={query} /></>}{result.section && <> / <Highlight text={result.section} query={query} /></>}</span>
          <strong><Highlight text={result.doc.title} query={query} /></strong>
          <span className="docs-search-match-types">{result.kinds.map(kind => <span key={kind}>{labels[kind]}</span>)}</span>
          {result.snippet && <span className="docs-search-snippet"><Highlight text={result.snippet} query={query} /></span>}
        </a>)}
      </nav>
    </div>
  </Dialog>;
}
