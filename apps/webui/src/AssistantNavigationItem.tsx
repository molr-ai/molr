import { Tooltip } from '@molr/ui';
import { MessagesSquare } from 'lucide-react';
import { useCopy, useQueryValue } from './state';
import { conversationUnread } from './conversation-unread';

/** Keep conversation updates local to the navigation item, including its accessible description. */
export function AssistantNavigationItem({ active, mobile, onNavigate }: { active: boolean; mobile: boolean; onNavigate: () => void }) {
  const { t } = useCopy();
  const unread = useQueryValue('/api/conversations', conversationUnread, true);
  return <Tooltip content={t.nav.assistants} side={mobile ? 'top' : 'right'} width="auto">
    <button className="nav-button" type="button" aria-label={t.nav.assistants} aria-current={active ? 'page' : undefined} aria-describedby={unread > 0 ? 'nav-unread-count' : undefined} onClick={onNavigate}>
      <span className="nav-icon"><MessagesSquare size={22} />{unread > 0 && <span className="nav-unread" id="nav-unread-count" role="status" aria-label={t.assistants.unread(unread)}>{unread > 99 ? '99+' : unread}</span>}</span>
      <span>{t.nav.assistants}</span>
    </button>
  </Tooltip>;
}
