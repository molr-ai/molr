import { createRoot } from 'react-dom/client';
import { DraftProvider, useDraft } from '../../src/drafts';
import { DataProvider, useData } from '../../src/state';
import { useChatHistory } from '../../src/use-chat-history';

declare global { interface Window { renders: Record<string, number>; refreshHistory: () => void; invalidateHistoryMessage: (id: string, conversationID?: string) => void } }
window.renders = {};
function count(name: string) { window.renders[name] = (window.renders[name] ?? 0) + 1; }
function Draft({ id }: { id: string }) {
  count(id);
  const [draft, update] = useDraft(id);
  return <input aria-label={id} value={draft.text} onChange={event => update({ ...draft, text: event.target.value })} />;
}
function History() {
  count('history');
  const history = useChatHistory('fixture');
  const { invalidateTopics, invalidateMessage } = useData();
  window.invalidateHistoryMessage = invalidateMessage;
  window.refreshHistory = () => invalidateTopics(['messages:fixture']);
  return <output aria-label="history">{JSON.stringify({ loaded: history.loaded, loading: history.loading, error: !!history.error, messages: history.messages })}</output>;
}
function Host() {
  count('host');
  return <><Draft id="first" /><Draft id="second" /><History /></>;
}
createRoot(document.getElementById('root')!).render(<DataProvider><DraftProvider owner="fixture"><Host /></DraftProvider></DataProvider>);
