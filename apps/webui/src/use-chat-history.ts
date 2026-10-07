import { useEffect, useRef, useState } from 'react';
import { api, APIError } from './api/client';
import type { Message, Page } from './api/types';
import { useData, useTopicVersion } from './state';

type Snapshot = { messages: Message[]; olderCursor: string; metadataVersions: Record<string, number> };
type History = { messages: Message[]; metadataVersions: Record<string, number>; more: boolean; loaded: boolean; loading: boolean; loadingOlder: boolean; error: unknown };

export function useChatHistory(conversationID: string, targetMessageID = '') {
  const { cache, messageChanges } = useData(), revision = useTopicVersion('messages');
  const key = `chat:${conversationID}`;
  const initial = cache.get<Snapshot>(key);
  const [history, setHistory] = useState<History>(() => ({ messages: initial?.messages ?? [], metadataVersions: initial?.metadataVersions ?? {}, more: !!initial?.olderCursor, loaded: !!initial, loading: true, loadingOlder: false, error: null }));
  const requests = useRef({ refresh: () => {}, older: () => {} });
  const [location, setLocation] = useState<{ id: string; status: 'loading' | 'found' | 'missing' }>({ id: targetMessageID, status: 'loading' });
  useEffect(() => {
    const controller = new AbortController();
    let snapshot = cache.get<Snapshot>(key);
    let running = false, refreshPending = false, olderPending = false;
    let locatePending = !!targetMessageID;
    setLocation({ id: targetMessageID, status: 'loading' });
    const path = `/api/conversations/${conversationID}/messages`;
    const get = (query: string) => api.get<Page<Message>>(`${path}?limit=50&${query}`, controller.signal);
    function commit(items: Message[], olderCursor = snapshot?.olderCursor ?? '', versions: Record<string, number> = {}) {
      if (controller.signal.aborted) return;
      const merged = new Map(snapshot?.messages.map(message => [message.ID, message]));
      items.forEach(message => merged.set(message.ID, message));
      const next = { messages: [...merged.values()].sort((a, b) => a.Sequence - b.Sequence), olderCursor, metadataVersions: { ...snapshot?.metadataVersions, ...versions } };
      snapshot = next;
      cache.set(key, next);
      setHistory(previous => ({ ...previous, messages: next.messages, metadataVersions: next.metadataVersions, more: !!olderCursor, loaded: true, error: null }));
    }
    async function fetchPage(query: string, older = false) {
      // Capture versions before the request; events arriving in flight must
      // still cause another metadata read, even if this response is stale.
      const versions = new Map(messageChanges);
      const page = await get(query);
      const items = page.Items ?? [];
      commit(items, older ? page.NextCursor : undefined, Object.fromEntries(items.map(message => [message.ID, versions.get(message.ID) ?? 0])));
      return page;
    }
    async function refresh() {
      if (!snapshot) {
        await fetchPage('order=desc', true);
      } else {
        const after = snapshot.messages.at(-1)?.Sequence ?? 0;
        let cursor = '';
        const seen = new Set<string>();
        do {
          const page = await fetchPage(`order=asc&after_sequence=${after}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`);
          cursor = page.NextCursor;
          if (cursor && seen.has(cursor)) throw new APIError('invalid_response');
          seen.add(cursor);
        } while (cursor && !controller.signal.aborted);
      }
      // Existing rows change only through acceptance/feedback events. Refresh
      // those exact rows without downloading all previously loaded history.
      for (const message of snapshot?.messages ?? []) {
        const version = messageChanges.get(message.ID) ?? 0;
        if (version === (snapshot?.metadataVersions[message.ID] ?? 0)) continue;
        const page = await get(`message_id=${encodeURIComponent(message.ID)}`);
        commit(page.Items ?? [], undefined, { [message.ID]: version });
      }
    }
    async function locate() {
      // Verify the target in this conversation before traversing history. Do
      // not insert an isolated row: that would leave gaps in the cached log.
      const target = await get(`message_id=${encodeURIComponent(targetMessageID)}`);
      if (controller.signal.aborted) return;
      const exists = target.Items?.some(message => message.ID === targetMessageID);
      const seen = new Set<string>();
      while (exists && !snapshot?.messages.some(message => message.ID === targetMessageID) && snapshot?.olderCursor && !controller.signal.aborted) {
        const cursor = snapshot.olderCursor;
        if (seen.has(cursor)) throw new APIError('invalid_response');
        seen.add(cursor);
        await fetchPage(`order=desc&cursor=${encodeURIComponent(cursor)}`, true);
      }
      if (controller.signal.aborted) return;
      locatePending = false;
      setLocation({ id: targetMessageID, status: exists && snapshot?.messages.some(message => message.ID === targetMessageID) ? 'found' : 'missing' });
    }
    async function drain() {
      if (running || controller.signal.aborted) return;
      running = true;
      setHistory(previous => ({ ...previous, loading: true, error: null }));
      try {
        while ((refreshPending || olderPending || locatePending) && !controller.signal.aborted) {
          if (locatePending && snapshot && !refreshPending) {
            await locate();
          } else if (olderPending && snapshot) {
            olderPending = false;
            if (snapshot.olderCursor) await fetchPage(`order=desc&cursor=${encodeURIComponent(snapshot.olderCursor)}`, true);
            if (!controller.signal.aborted) setHistory(previous => ({ ...previous, loadingOlder: false }));
          } else {
            refreshPending = false;
            await refresh();
          }
        }
      } catch (error) {
        if (!controller.signal.aborted) {
          const inaccessible = error instanceof APIError && [401, 403, 404].includes(error.status);
          if (inaccessible) { cache.delete(key); snapshot = undefined; refreshPending = olderPending = false; }
          setHistory(previous => ({ ...previous, ...(inaccessible ? { messages: [], metadataVersions: {}, more: false, loaded: false } : {}), error }));
        }
      } finally {
        running = false;
        if (!controller.signal.aborted) setHistory(previous => ({ ...previous, loading: false, loadingOlder: false }));
      }
    }
    requests.current = {
      refresh() { refreshPending = true; void drain(); },
      older() { if (!snapshot?.olderCursor || olderPending) return; olderPending = true; setHistory(previous => ({ ...previous, loadingOlder: true })); void drain(); },
    };
    // Updates queue behind in-flight requests instead of aborting them. A
    // continuous event stream cannot starve an otherwise successful response.
    return () => { controller.abort(); requests.current = { refresh: () => {}, older: () => {} }; };
  }, [conversationID, key, cache, messageChanges, targetMessageID]);
  useEffect(() => { requests.current.refresh(); }, [revision, conversationID, targetMessageID]);
  return { ...history, targetStatus: location.id === targetMessageID ? location.status : 'loading', loadOlder: () => requests.current.older() };
}
