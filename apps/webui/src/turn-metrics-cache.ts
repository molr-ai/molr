import { api, APIError } from './api/client';
import type { TurnMetrics } from './api/types';

type Snapshot = { data?: TurnMetrics; error?: unknown; loading: boolean; receivedAt?: number };
type Entry = { snapshot: Snapshot; listeners: Set<() => void>; controller?: AbortController; timer?: ReturnType<typeof setTimeout>; path: string };

/** Only this login session owns the cache. A finalized snapshot never refetches. */
export class TurnMetricsCache {
  private entries = new Map<string, Entry>();
  private closed = false;
  private entry(key: string, path: string) {
    let entry = this.entries.get(key);
    if (!entry) { entry = { snapshot: { loading: false }, listeners: new Set(), path }; this.entries.set(key, entry); }
    return entry;
  }
  snapshot(key: string, path: string) { return this.entry(key, path).snapshot; }
  subscribe(key: string, path: string, listener: () => void) {
    const entry = this.entry(key, path);
    entry.listeners.add(listener);
    void this.refresh(key, path);
    return () => {
      entry.listeners.delete(listener);
      if (!entry.listeners.size) { clearTimeout(entry.timer); entry.controller?.abort(); entry.controller = undefined; entry.snapshot = { ...entry.snapshot, loading: false }; }
    };
  }
  private emit(entry: Entry, snapshot: Snapshot) { entry.snapshot = snapshot; entry.listeners.forEach(listener => listener()); }
  async refresh(key: string, path: string) {
    const entry = this.entry(key, path);
    if (this.closed || entry.controller || entry.snapshot.data?.Finalized) return;
    clearTimeout(entry.timer);
    const controller = new AbortController(); entry.controller = controller;
    this.emit(entry, { ...entry.snapshot, loading: true, error: undefined });
    try {
      const data = await api.get<TurnMetrics>(path, controller.signal);
      if (!controller.signal.aborted && !this.closed) this.emit(entry, { data, loading: false, receivedAt: performance.now() });
    } catch (error) {
      if (!controller.signal.aborted && !this.closed) {
        const inaccessible = error instanceof APIError && [401, 403, 404].includes(error.status);
        this.emit(entry, { ...(!inaccessible ? entry.snapshot : {}), loading: false, error });
      }
    } finally {
      if (entry.controller === controller) {
        entry.controller = undefined;
        if (!this.closed && entry.listeners.size && !entry.snapshot.data?.Finalized && entry.snapshot.data?.State !== 'unavailable' && !entry.snapshot.error) entry.timer = setTimeout(() => void this.refresh(key, path), 2000);
      }
    }
  }
  clear() {
    this.closed = true;
    this.entries.forEach(entry => { clearTimeout(entry.timer); entry.controller?.abort(); });
    this.entries.clear();
  }
  // React StrictMode can replay the provider's effect setup after cleanup.
  activate() { this.closed = false; this.entries.forEach((entry, key) => { if (entry.listeners.size) void this.refresh(key, entry.path); }); }
}
