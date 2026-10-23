import { createContext, useCallback, useContext, useState, useSyncExternalStore } from 'react';
import type { ReactNode } from 'react';
import type { ChatDraft } from '@molr/sdk/ui';
import { DraftStore } from './draft-store';

const Context = createContext<DraftStore | null>(null);
export function DraftProvider({ owner, children }: { owner: string; children: ReactNode }) {
  const [store] = useState(() => new DraftStore(owner, {
    getItem: key => sessionStorage.getItem(key),
    setItem: (key, value) => sessionStorage.setItem(key, value),
  }));
  return <Context.Provider value={store}>{children}</Context.Provider>;
}
export function useDraft(id: string) {
  const store = useContext(Context);
  if (!store) throw new Error('Missing drafts');
  const subscribe = useCallback((listener: () => void) => store.subscribe(id, listener), [store, id]);
  const snapshot = useCallback(() => store.snapshot(id), [store, id]);
  const update = useCallback((value: ChatDraft | ((previous: ChatDraft) => ChatDraft)) => store.update(id, value), [store, id]);
  return [useSyncExternalStore(subscribe, snapshot), update] as const;
}
