import { StrictMode, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { DraftProvider, useDraft } from '../../src/drafts';
import { DataProvider, useData } from '../../src/state';
import { useChatHistory } from '../../src/use-chat-history';

declare global { interface Window { renders: Record<string, number>; refreshHistory: () => void; invalidateHistoryMessage: (id: string, conversationID?: string) => void; selectHistory: (id: string) => void; cachedHistoryFeedback: () => string | undefined } }
window.renders = {};
function count(name: string) { window.renders[name] = (window.renders[name] ?? 0) + 1; }
function Draft({ id }: { id: string }) {
  count(id);
  const [draft, update] = useDraft(id);
  return <input aria-label={id} value={draft.text} onChange={event => update({ ...draft, text: event.target.value })} />;
}
function History({ conversationID }: { conversationID: string }) {
  count('history');
  const history = useChatHistory(conversationID);
  const { invalidateTopics, invalidateMessage, cache } = useData();
  window.cachedHistoryFeedback = () => cache.get(conversationID)?.messages[0]?.Feedback;
  window.invalidateHistoryMessage = invalidateMessage;
  window.refreshHistory = () => invalidateTopics([`messages:${conversationID}`]);
  return <output aria-label="history">{JSON.stringify({ conversationID, loaded: history.loaded, loading: history.loading, error: !!history.error, messages: history.messages })}</output>;
}
function Host() {
  count('host');
  const [conversationID, selectHistory] = useState('fixture');
  window.selectHistory = selectHistory;
  return <><Draft id="first" /><Draft id="second" /><History key={conversationID} conversationID={conversationID} /></>;
}
const fixture = <DataProvider><DraftProvider owner="fixture"><Host /></DraftProvider></DataProvider>;
createRoot(document.getElementById('root')!).render(new URLSearchParams(location.search).has('strict') ? <StrictMode>{fixture}</StrictMode> : fixture);
