import { productMessages } from './i18n';
import { useEffect, useState } from 'react';
import { Archive, UserRoundPlus } from 'lucide-react';
import { Badge, Button, ChatMessage, Dialog, SearchField } from '@molr/ui';
import type { Locale } from './messages';
import { AssistantActionDialog } from './AssistantActionDialog';

export function FormerAssistantsPanel({ locale, active }: { locale: Locale; active: boolean }) {
  const t = productMessages[locale];
  const [query, setQuery] = useState('');
  const [archiveOpen, setArchiveOpen] = useState(false);
  const [rehiring, setRehiring] = useState(false);
  useEffect(() => { if (!active) { setArchiveOpen(false); setRehiring(false); } }, [active]);
  const name = t.formerAssistantsPanel.readingAssistant;
  const messages = [
    { role: 'user' as const, text: t.formerAssistantsPanel.summarizeTheKeyPointsOfThisArticle },
    { role: 'assistant' as const, text: t.formerAssistantsPanel.theKeyPointsAreReadyWithReferencesToThe },
  ];
  const matches = [name, ...messages.map(message => message.text)].some(text => text.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  return <div className="former-assistants">
    <p className="ip-lead">{t.formerAssistantsPanel.findFormerAssistantsReviewArchivedConversationsOrRehireThem}</p>
    <SearchField label={t.formerAssistantsPanel.searchFormerAssistantsAndConversations} hideLabel placeholder={t.formerAssistantsPanel.searchNamesOrMessages} value={query} onValueChange={setQuery} />
    {matches ? <ul className="former-assistants__list" aria-label={t.formerAssistantsPanel.formerAssistants}>
      <li className="ip-row">
        <span className="ip-avatar" aria-hidden="true">{name.slice(0, 1)}</span>
        <span className="ip-row__body"><strong>{name}</strong><small>{t.formerAssistantsPanel.sampleArchive2Messages}</small></span>
        <Badge>{t.formerAssistantsPanel.former}</Badge>
        <div className="former-assistants__actions">
          <Button size="sm" variant="secondary" onClick={() => setArchiveOpen(true)}><Archive size={15} />{t.formerAssistantsPanel.viewArchive}</Button>
          <Button size="sm" onClick={() => setRehiring(true)}><UserRoundPlus size={15} />{t.formerAssistantsPanel.rehire}</Button>
        </div>
      </li>
    </ul> : <div className="ip-empty" role="status"><Archive size={24} /><h4>{t.formerAssistantsPanel.noMatchingFormerAssistants}</h4><p>{t.formerAssistantsPanel.tryADifferentNameOrMessageKeyword}</p><Button size="sm" variant="secondary" onClick={() => setQuery('')}>{t.common.clearSearch}</Button></div>}
    <Dialog open={active && archiveOpen} onOpenChange={setArchiveOpen} title={t.formerAssistantsPanel.archivedConversation} description={`${name} · ${t.formerAssistantsPanel.sampleArchive}`}>
      <p className="ip-notice">{t.formerAssistantsPanel.thisAssistantHasLeftTheseMessagesAreReadOnly}</p>
      <div aria-label={t.formerAssistantsPanel.archivedMessages} role="region">
        {messages.map(message => <ChatMessage key={message.role} role={message.role} sender={message.role === 'user' ? t.common.you : name} text={message.text} readStatus={message.role === 'user' ? 'read' : undefined} />)}
      </div>
    </Dialog>
    <AssistantActionDialog action={active && rehiring ? { kind: 'rehire', name } : null} locale={locale} onClose={() => setRehiring(false)} />
  </div>;
}
