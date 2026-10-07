import { createContext, useContext, useEffect, useId, useRef, useState, useSyncExternalStore } from 'react';
import type { ReactNode, RefObject } from 'react';

// Same error, one visible notice. Request IDs deliberately do not create new
// notices. Dismissal survives background polling until an explicit user action.
class ErrorNotices {
 private entries = new Map<string, { key: string; element: RefObject<HTMLDivElement | null> }>();
 private dismissed = new Set<string>();
 private listeners = new Set<() => void>();
 private revision = 0;
 subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
 snapshot = () => this.revision;
 private changed() { this.revision++; this.listeners.forEach(listener => listener()); }
 register(id: string, key: string, element: RefObject<HTMLDivElement | null>) {
  this.entries.set(id, { key, element }); this.changed();
  return () => { this.entries.delete(id); if (key.includes(':')) this.dismissed.delete(key); this.changed(); };
 }
 owner(key: string) {
  if (this.dismissed.has(key)) return '';
  let owner = '', score = -1;
  const dialogs = [...document.querySelectorAll('dialog[open]')];
  for (const [id, entry] of this.entries) {
   if (entry.key !== key) continue;
   const dialog = entry.element.current?.closest('dialog');
   if (dialog && !dialog.open) continue;
   const next = dialog ? dialogs.indexOf(dialog) + 2 : 0;
   if (next >= score) { owner = id; score = next; }
  }
  return owner;
 }
 dismiss(key: string) { this.dismissed.add(key); this.changed(); }
 rearm = () => { if (this.dismissed.size) { this.dismissed.delete('rate_limited'); this.dismissed.delete('unavailable'); this.changed(); } };
}
const Context = createContext<ErrorNotices | null>(null);
export function ErrorNoticeProvider({ children }: { children: ReactNode }) {
 const [notices] = useState(() => new ErrorNotices());
 return <Context.Provider value={notices}>{children}</Context.Provider>;
}
export function useErrorNotices() { return useContext(Context); }
export function useErrorNotice(key: string) {
 const shared = useErrorNotices(), [fallback] = useState(() => new ErrorNotices()), notices = shared ?? fallback;
 const id = useId(), element = useRef<HTMLDivElement>(null);
 useSyncExternalStore(notices.subscribe, notices.snapshot, notices.snapshot);
 useEffect(() => key ? notices.register(id, key, element) : undefined, [id, key, notices]);
 return { element, visible: !!key && notices.owner(key) === id, dismiss: () => notices.dismiss(key) };
}
