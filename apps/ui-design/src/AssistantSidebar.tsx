import { productMessages } from './i18n';
import { Ellipsis, Pin, Plus, Search } from 'lucide-react';
import { AssistantList, Button, DropdownMenu, IconButton, Logo, useTheme } from '@molr/ui';
import type { InterfaceDemo } from './interface-demo';
import type { Locale } from './messages';
import { assistantListPreview } from './assistant-list-preview';

export function AssistantSidebar({ demo, locale, onSearch, onSelect, mobile, hidden, id }: {
  demo: InterfaceDemo; locale: Locale; onSearch: () => void;
  onSelect: (id: string) => void; mobile: boolean; hidden: boolean; id: string;
}) {
  const t = productMessages[locale];
  const { theme } = useTheme();
  const assistants = demo.assistants;
  return <aside id={id} className="conversation-sidebar" aria-label={t.assistantSidebar.assistantSidebar} hidden={hidden}>
    {mobile && <div className="mobile-assistant-toolbar">
      <Logo className="assistant-toolbar-logo" variant={theme === 'dark' ? 'light' : 'dark'} animation="daily" interactive interactionLabel={t.common.molrSkipTheRestAndPlayTheNextAnimation} />
      <IconButton label={t.common.search} aria-haspopup="dialog" onClick={onSearch}><Search size={20} /></IconButton>
      <DropdownMenu align="end" label={t.assistantSidebar.assistantActions} trigger={<IconButton label={t.common.more}><Ellipsis size={20} /></IconButton>} items={[{ id: 'create', label: t.assistantSidebar.hireNewAssistant, icon: <Plus size={16} />, onSelect: () => {} }]} />
    </div>}
    <AssistantList label={t.common.assistantList} selectedID={demo.activeAssistantId} onSelect={onSelect} mobile={mobile}
      unreadLabel={t.common.unreadMessages} pinnedLabel={t.assistantSidebar.pinned2} pinIcon={<Pin size={12} aria-hidden="true" />} emptyLabel={t.assistantSidebar.noMatchingConversations}
      entries={assistants.map(assistant => {
        const last = assistant.messages.at(-1);
        return { id: assistant.id, name: assistant.name, preview: last ? locale === 'en' && last.textEn ? last.textEn : last.text : t.assistantSidebar.startAConversation,
          pinned: assistantListPreview(assistant.id).pinned, unread: demo.unreadCounts[assistant.id] };
      })} />
    {!mobile && <div className="assistant-sidebar-footer">
      <Button className="conversation-sidebar__create" size="sm"><Plus size={18} />{t.assistantSidebar.hireNewAssistant}</Button>
    </div>}
  </aside>;
}
