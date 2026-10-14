import { AssistantAvatar } from './AssistantAvatar';
import { SlidingHighlight } from './SlidingHighlight';
import type { ReactNode } from 'react';

export type AssistantListEntry = { id: string; name: string; avatar?: string; preview?: ReactNode; unread?: number; pinned?: boolean };
export type AssistantListProps = {
  label: string; entries: readonly AssistantListEntry[]; selectedID?: string; onSelect: (id: string) => void;
  unreadLabel: (count: number) => string; pinnedLabel: string; pinIcon?: ReactNode; emptyLabel: string; emptyState?: ReactNode; mobile?: boolean; className?: string;
};
/** Presentation only: authentication, fetching and preview data belong to callers. */
export function AssistantList({ label, entries, selectedID, onSelect, unreadLabel, pinnedLabel, pinIcon, emptyLabel, emptyState, mobile = false, className = '' }: AssistantListProps) {
  const visible = [...entries].sort((a, b) => Number(!!b.pinned) - Number(!!a.pinned));
  return <SlidingHighlight as="ul" value={selectedID} disabled={mobile} className={`assistant-list${mobile ? ' assistant-list--mobile' : ''}${!visible.length && emptyState ? ' assistant-list--empty' : ''} ${className}`} aria-label={label}>
    {visible.map(entry => <li key={entry.id}><button type="button" data-assistant-id={entry.id} data-sliding-item={entry.id} aria-label={entry.name} aria-current={!mobile && entry.id === selectedID ? 'page' : undefined} onClick={() => onSelect(entry.id)}>
      <span className="ui-assistant-avatar ip-avatar" aria-hidden="true"><AssistantAvatar name={entry.name} avatar={entry.avatar} /></span><span className="assistant-list__copy"><strong>{entry.name}</strong><small>{entry.preview}</small></span>
      {entry.pinned && <span className="assistant-list__pin" role="img" aria-label={pinnedLabel} title={pinnedLabel}>{pinIcon}</span>}
      {!!entry.unread && <span className="assistant-unread" aria-label={unreadLabel(entry.unread)}>{entry.unread}</span>}
    </button></li>)}
    {!visible.length && <li className="assistant-list__empty" role="status">{emptyState ?? emptyLabel}</li>}
  </SlidingHighlight>;
}
