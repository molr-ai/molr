import { useLayoutEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { Button, IconButton } from './ui';
import { Dialog } from './Dialog';
import { SearchField } from './forms';
import { Select } from './Select';
import { UIIcon } from './icons';

export type SearchPanelSource = { value: string; label: string; icon: ReactNode };
export type SearchPanelItem = { id: string; source: string; title: string; description: string; onSelect: () => void };
export type SearchPanelShortcut = { id: string; label: string; description: string; icon: ReactNode; onSelect: () => void; disabled?: boolean };
export type SearchPanelLabels = {
  title: string; input: string; placeholder: string; clear: string; filter: string; all: string;
  quickNavigation: string; trySearching: string; resultsFound: (count: number) => string;
  emptyTitle: string; emptyHelp: string; reset: string; keyboardHint: string;
};
export type SearchPanelProps = {
  open: boolean; onOpenChange: (open: boolean) => void; mobile?: boolean;
  query: string; onQueryChange: (value: string) => void; scopes: string[]; onScopesChange: (values: string[]) => void;
  sources: SearchPanelSource[]; shortcuts: SearchPanelShortcut[]; items: SearchPanelItem[];
  labels: SearchPanelLabels; suggestions?: string[]; total?: number;
  busy?: boolean; statusContent?: ReactNode; pagination?: ReactNode;
};

/** Shared presentation and keyboard behavior; callers own retrieval and navigation. */
export function SearchPanel({ open, onOpenChange, mobile = false, query, onQueryChange, scopes, onScopesChange, sources, shortcuts, items, labels, suggestions = [], total = items.length, busy = false, statusContent, pagination }: SearchPanelProps) {
  const content = useRef<HTMLDivElement>(null), results = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => { if (open) focusInput(); }, [open]);
  const term = query.trim().toLowerCase();
  function focusInput() { content.current?.querySelector<HTMLInputElement>('input')?.focus({ preventScroll: true }); }
  function changeQuery(value: string) { onQueryChange(value); if (!value.trim()) onScopesChange([]); }
  function resultButtons() { return Array.from(results.current?.querySelectorAll<HTMLButtonElement>('[data-search-item]:not(:disabled)') ?? []); }
  function select(action: () => void) { onOpenChange(false); action(); }
  function highlight(text: string) {
    const index = term ? text.toLowerCase().indexOf(term) : -1;
    return index < 0 ? text : <>{text.slice(0, index)}<mark>{text.slice(index, index + term.length)}</mark>{text.slice(index + term.length)}</>;
  }
  return <Dialog open={open} onOpenChange={onOpenChange} title={labels.title} className="workspace-search" size="lg" showCloseButton={false}
    headerContent={<div ref={content} className="workspace-search__controls">
      <SearchField label={labels.input} hideLabel placeholder={labels.placeholder} value={query} onValueChange={changeQuery} maxLength={120} clearLabel={labels.clear} onKeyDown={event => {
        if (event.nativeEvent.isComposing) return;
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
          event.preventDefault(); const buttons = resultButtons(); (event.key === 'ArrowDown' ? buttons[0] : buttons.at(-1))?.focus();
        } else if (event.key === 'Enter' && term) { event.preventDefault(); resultButtons()[0]?.click(); }
      }} trailingAction={<Select multiple hideLabel label={labels.filter} align="end" value={scopes.length ? scopes : ['all']} onValueChange={values => {
        if (scopes.length && values.includes('all')) { onScopesChange([]); return; }
        const next = values.filter(value => value !== 'all');
        onScopesChange(next.length === sources.length ? [] : next);
      }} options={[{ value: 'all', label: labels.all }, ...sources]} trigger={<IconButton className="workspace-search__filter" label={labels.filter} data-filter-active={!!scopes.length}><UIIcon name="filter" strokeWidth={2} /></IconButton>} />} />
    </div>}
    footer={!mobile && <span className="workspace-search__hint">{labels.keyboardHint}</span>}>
    <div className="workspace-search__content">
      <div ref={results} className="workspace-search__results" aria-busy={busy || undefined} onKeyDown={event => {
        if (event.nativeEvent.isComposing || !['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key) || !(event.target instanceof Element) || !event.target.closest('[data-search-item]')) return;
        const buttons = resultButtons(), index = buttons.indexOf(document.activeElement as HTMLButtonElement);
        if (!buttons.length) return;
        event.preventDefault();
        const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length;
        buttons[next]?.focus();
      }}>
        {!term ? <>
          <section aria-label={labels.quickNavigation}>
            <h3>{labels.quickNavigation}</h3>
            <div className="workspace-search__shortcuts">{shortcuts.map(item => <button key={item.id} type="button" data-search-item disabled={item.disabled} onClick={() => select(item.onSelect)}>
              {item.icon}<span><strong>{item.label}</strong><small>{item.description}</small></span><UIIcon name="arrow-up-right" width={14} height={14} strokeWidth={2} />
            </button>)}</div>
          </section>
          {!!suggestions.length && <div className="workspace-search__suggestions"><span>{labels.trySearching}</span>{suggestions.map(word => <Button key={word} variant="ghost" size="sm" onClick={() => { changeQuery(word); focusInput(); }}>{word}</Button>)}</div>}
        </> : statusContent || <>
          <p className="workspace-search__summary" role="status">{labels.resultsFound(total)}{!!scopes.length && ` · ${sources.filter(source => scopes.includes(source.value)).map(source => source.label).join(' / ')}`}</p>
          {items.length ? sources.filter(source => !scopes.length || scopes.includes(source.value)).map(source => {
            const rows = items.filter(item => item.source === source.value);
            return !!rows.length && <section className="workspace-search__section" key={source.value} aria-label={source.label}>
              <h3>{source.label}<span>{rows.length}</span></h3>
              <ul>{rows.map(item => <li key={item.id}><button type="button" data-search-item className="workspace-search__result" onClick={() => select(item.onSelect)}>
                <span className="workspace-search__result-icon">{source.icon}</span><span className="workspace-search__result-copy"><strong>{highlight(item.title)}</strong><small>{highlight(item.description)}</small></span><UIIcon name="chevron-right" width={15} height={15} strokeWidth={2} />
              </button></li>)}</ul>
            </section>;
          }) : <div className="workspace-search__empty"><UIIcon name="search" width={28} height={28} strokeWidth={2} /><h3>{labels.emptyTitle}</h3><p>{labels.emptyHelp}</p><Button size="sm" variant="secondary" onClick={() => { changeQuery(''); focusInput(); }}>{labels.reset}</Button></div>}
          {pagination}
        </>}
      </div>
    </div>
  </Dialog>;
}
