import { AssistantAvatar } from './AssistantAvatar';
import { useUILocale } from '../locale';
import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';

export type AssistantListEntry = { id: string; name: string; avatar?: string; preview?: ReactNode; unread?: number; pinned?: boolean };
export type AssistantListProps = {
  label: string; entries: readonly AssistantListEntry[]; selectedID?: string; onSelect: (id: string) => void;
  unreadLabel: (count: number) => string; pinnedLabel: string; pinIcon?: ReactNode; emptyLabel: string; emptyState?: ReactNode; mobile?: boolean; className?: string;
};
/** Presentation only: authentication, fetching and preview data belong to callers. */
export function AssistantList({ label, entries, selectedID, onSelect, unreadLabel, pinnedLabel, pinIcon, emptyLabel, emptyState, mobile = false, className = '' }: AssistantListProps) {
  const listRef = useRef<HTMLUListElement>(null);
  const { reducedMotion } = useUILocale();
  const [hoveredID, setHoveredID] = useState<string>();
  const highlightedID = mobile ? undefined : entries.some(entry => entry.id === hoveredID) ? hoveredID : selectedID;
  const visible = [...entries].sort((a, b) => Number(!!b.pinned) - Number(!!a.pinned));
  useLayoutEffect(() => {
    const list = listRef.current;
    if (!list) return;
    const buttons = [...list.querySelectorAll<HTMLButtonElement>('button[data-assistant-id]')];
    const target = buttons.find(button => button.dataset.assistantId === highlightedID);
    const measure = () => {
      if (!target) {
        delete list.dataset.highlight;
        return;
      }
      const bounds = list.getBoundingClientRect();
      const rect = target.getBoundingClientRect();
      list.style.setProperty('--assistant-highlight-x', `${rect.left - bounds.left + list.scrollLeft - list.clientLeft}px`);
      list.style.setProperty('--assistant-highlight-y', `${rect.top - bounds.top + list.scrollTop - list.clientTop}px`);
      list.style.setProperty('--assistant-highlight-width', `${rect.width}px`);
      list.style.setProperty('--assistant-highlight-height', `${rect.height}px`);
      list.dataset.highlight = '';
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(list);
    buttons.forEach(button => observer.observe(button));
    return () => observer.disconnect();
  }, [entries, highlightedID, mobile]);

  return <ul ref={listRef} className={`assistant-list${mobile ? ' assistant-list--mobile' : ''}${!visible.length && emptyState ? ' assistant-list--empty' : ''} ${className}`} aria-label={label} data-reduced-motion={reducedMotion || undefined} onPointerLeave={() => setHoveredID(undefined)}>
    {visible.map(entry => <li key={entry.id}><button type="button" data-assistant-id={entry.id} aria-label={entry.name} aria-current={!mobile && entry.id === selectedID ? 'page' : undefined} onClick={() => onSelect(entry.id)}
      onPointerEnter={event => { if (event.pointerType !== 'touch') setHoveredID(entry.id); }} onPointerLeave={() => setHoveredID(undefined)} onPointerCancel={() => setHoveredID(undefined)}>
      <span className="ui-assistant-avatar ip-avatar" aria-hidden="true"><AssistantAvatar name={entry.name} avatar={entry.avatar} /></span><span className="assistant-list__copy"><strong>{entry.name}</strong><small>{entry.preview}</small></span>
      {entry.pinned && <span className="assistant-list__pin" role="img" aria-label={pinnedLabel} title={pinnedLabel}>{pinIcon}</span>}
      {!!entry.unread && <span className="assistant-unread" aria-label={unreadLabel(entry.unread)}>{entry.unread}</span>}
    </button></li>)}
    {!visible.length && <li className="assistant-list__empty" role="status">{emptyState ?? emptyLabel}</li>}
  </ul>;
}
