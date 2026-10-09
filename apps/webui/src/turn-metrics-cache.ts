import { api, APIError } from './api/client';
import type { TurnMetrics } from './api/types';

type Snapshot = { data?: TurnMetrics; error?: unknown; loading: boolean; receivedAt?: number };
type Entry = { snapshot: Snapshot; listeners: Set<() => void>; controller?: AbortController; timer?: ReturnType<typeof setTimeout>; path: string };

const emptySnapshot: Snapshot = Object.freeze({ loading: false });
const retainedLimit = 100;

/** Session-local, demand-loaded snapshots. Retained final results need no refetch. */
export class TurnMetricsCache {
  private entries = new Map<string, Entry>();
  private closed = false;
  private entry(key: string, path: string) {
    let entry = this.entries.get(key);
    if (!entry) entry = { snapshot: emptySnapshot, listeners: new Set(), path };
    this.entries.delete(key); this.entries.set(key, entry);
    return entry;
  }
  // Rendering a closed message footer must not allocate a persistent entry.
  snapshot(key: string, _path: string) { return this.entries.get(key)?.snapshot ?? emptySnapshot; }
  private prune() {
    if (this.entries.size <= retainedLimit) return;
    for (const [key, entry] of this.entries) {
      // Visible snapshots and pending requests remain pinned until released.
      if (entry.listeners.size || entry.controller) continue;
      clearTimeout(entry.timer); this.entries.delete(key);
      if (this.entries.size <= retainedLimit) break;
    }
  }
  subscribe(key: string, path: string, listener: () => void) {
    const entry = this.entry(key, path);
    entry.listeners.add(listener);
    void this.refresh(key, path);
    this.prune();
    return () => {
      entry.listeners.delete(listener);
      if (!entry.listeners.size) {
        clearTimeout(entry.timer); entry.controller?.abort(); entry.controller = undefined;
        entry.snapshot = { ...entry.snapshot, loading: false };
        if (this.entries.get(key) === entry) {
          this.entries.delete(key);
          if (entry.snapshot.data) this.entries.set(key, entry);
        }
      }
      this.prune();
    };
  }
  private emit(entry: Entry, snapshot: Snapshot) { entry.snapshot = snapshot; entry.listeners.forEach(listener => listener()); }
  async refresh(key: string, path: string) {
    if (this.closed) return;
    const entry = this.entry(key, path);
    if (entry.controller || entry.snapshot.data?.Finalized) return;
    clearTimeout(entry.timer);
    const controller = new AbortController(); entry.controller = controller;
    this.prune();
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
        this.prune();
      }
    }
  }
  clear() {
    this.closed = true;
    this.entries.forEach(entry => { clearTimeout(entry.timer); entry.controller?.abort(); });
    this.entries.clear();
  }
  // React StrictMode can replay the provider's effect setup after cleanup.
  activate() {
    this.closed = false;
    // Refresh updates recency, so iterate a fixed list rather than the live Map.
    for (const [key, entry] of [...this.entries]) if (entry.listeners.size) void this.refresh(key, entry.path);
  }
}
