import { useEffect, useRef, useState } from 'react';
import { poll } from './polling';
import { api, APIError } from '@molr/sdk';
import type { Message, Page } from '@molr/sdk';
import { useData } from './state';

type Snapshot = { messages: Message[]; olderCursor: string; metadataVersions: Record<string, number> };
type History = { messages: Message[]; metadataVersions: Record<string, number>; more: boolean; loaded: boolean; loading: boolean; loadingOlder: boolean; error: unknown };

export function useChatHistory(conversationID: string, targetMessageID = '') {
  const { cache, messageChanges, topics } = useData();
  const key = `chat:${conversationID}`;
  const initial = cache.get<Snapshot>(key);
  const [history, setHistory] = useState<History>(() => ({ messages: initial?.messages ?? [], metadataVersions: initial?.metadataVersions ?? {}, more: !!initial?.olderCursor, loaded: !!initial, loading: true, loadingOlder: false, error: null }));
  const requests = useRef({ older: () => {} });
  const [location, setLocation] = useState<{ id: string; status: 'loading' | 'found' | 'missing' }>({ id: targetMessageID, status: 'loading' });
  useEffect(() => {
    const controller = new AbortController();
    let snapshot = cache.get<Snapshot>(key);
    let running: Promise<void> | undefined;
    let refreshPending = false, olderPending = false;
    let locatePending = !!targetMessageID;
    setLocation({ id: targetMessageID, status: 'loading' });
    const path = `/api/conversations/${conversationID}/messages`;
    const get = (query: string) => api.get<Page<Message>>(`${path}?limit=50&${query}`, controller.signal);
    function updateHistory(patch: Partial<History>) {
      setHistory(previous => Object.entries(patch).every(([key, value]) => previous[key as keyof History] === value) ? previous : { ...previous, ...patch });
    }
    function commit(items: Message[], olderCursor = snapshot?.olderCursor ?? '', versions: Record<string, number> = {}) {
      if (controller.signal.aborted) return;
      let messages = snapshot?.messages ?? [];
      if (items.length) {
        const merged = new Map(messages.map(message => [message.ID, message]));
        let changed = false;
        for (const message of items) {
          const previous = merged.get(message.ID);
          if (!previous || JSON.stringify(previous) !== JSON.stringify(message)) {
            merged.set(message.ID, message); changed = true;
          }
        }
        if (changed) messages = [...merged.values()].sort((a, b) => a.Sequence - b.Sequence);
      }
      let metadataVersions = snapshot?.metadataVersions ?? {};
      if (Object.entries(versions).some(([id, version]) => (metadataVersions[id] ?? 0) !== version)) {
        metadataVersions = { ...metadataVersions, ...versions };
      }
      if (!snapshot || messages !== snapshot.messages || olderCursor !== snapshot.olderCursor || metadataVersions !== snapshot.metadataVersions) {
        snapshot = { messages, olderCursor, metadataVersions };
        cache.set(key, snapshot);
      }
      updateHistory({ messages, metadataVersions, more: !!olderCursor, loaded: true, error: null });
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
    async function work() {
      if (controller.signal.aborted) return;
      if (!snapshot) updateHistory({ loading: true, error: null });
      try {
        while ((refreshPending || olderPending || locatePending) && !controller.signal.aborted) {
          if (locatePending && snapshot && !refreshPending) {
            await locate();
          } else if (olderPending && snapshot) {
            olderPending = false;
            if (snapshot.olderCursor) await fetchPage(`order=desc&cursor=${encodeURIComponent(snapshot.olderCursor)}`, true);
            if (!controller.signal.aborted) updateHistory({ loadingOlder: false });
          } else {
            refreshPending = false;
            await refresh();
          }
        }
      } catch (error) {
        if (!controller.signal.aborted) {
          const inaccessible = error instanceof APIError && [401, 403, 404].includes(error.status);
          if (inaccessible) { cache.delete(key); snapshot = undefined; refreshPending = olderPending = false; }
          updateHistory({ ...(inaccessible ? { messages: [], metadataVersions: {}, more: false, loaded: false } : {}), error });
          throw error;
        }
      } finally {
        if (!controller.signal.aborted) updateHistory({ loading: false, loadingOlder: false });
      }
    }
    function drain() {
      running ??= work().finally(() => { running = undefined; });
      return running;
    }
    const loop = poll(async () => { refreshPending = true; await drain(); return 30_000; }, false);
    requests.current = {
      older() { if (!snapshot?.olderCursor || olderPending) return; olderPending = true; updateHistory({ loadingOlder: true }); void drain().catch(() => {}); },
    };
    // Topic invalidation requests data directly; it does not publish React state.
    const unsubscribe = topics.subscribe(`messages:${conversationID}`, () => loop.request(true));
    loop.request(true);
    // Updates queue behind in-flight requests instead of aborting them. A
    // continuous event stream cannot starve an otherwise successful response.
    return () => { unsubscribe(); loop.stop(); controller.abort(); requests.current = { older: () => {} }; };
  }, [conversationID, key, cache, messageChanges, topics, targetMessageID]);
  return { ...history, targetStatus: location.id === targetMessageID ? location.status : 'loading', loadOlder: () => requests.current.older() };
}
