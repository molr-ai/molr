import { APIError } from './api/client';

type Snapshot<T = unknown> = { data?: T; error?: unknown; loading: boolean; started: boolean };
type Loader = (signal: AbortSignal) => Promise<unknown>;
type Entry = {
  snapshot: Snapshot; listeners: Set<() => void>; loader?: Loader;
  revision?: number; requested?: number; pending: boolean; controller?: AbortController;
};
const empty: Snapshot = { loading: false, started: false };

/** One request and snapshot per query in this authenticated session. */
export class QueryStore {
  private entries = new Map<string, Entry>();
  private entry(key: string) {
    let entry = this.entries.get(key);
    if (!entry) {
      entry = { snapshot: empty, listeners: new Set(), pending: false };
      this.entries.set(key, entry);
    }
    return entry;
  }
  private trim() {
    for (const [key, entry] of this.entries) {
      if (this.entries.size <= 100) break;
      if (!entry.listeners.size && !entry.controller) this.entries.delete(key);
    }
  }
  snapshot<T>(key: string | null): Snapshot<T> { return (key ? this.entry(key).snapshot : empty) as Snapshot<T>; }
  subscribe(key: string, listener: () => void) {
    const entry = this.entry(key);
    entry.listeners.add(listener);
    return () => {
      entry.listeners.delete(listener);
      // Effect replay or another subscriber must not cancel a shared request.
      queueMicrotask(() => {
        if (entry.listeners.size) return;
        entry.controller?.abort(); entry.controller = undefined;
        entry.pending = false; entry.revision = undefined;
        entry.snapshot = { ...entry.snapshot, loading: false };
        this.trim();
      });
    };
  }
  private emit(entry: Entry, snapshot: Snapshot) {
    entry.snapshot = snapshot;
    entry.listeners.forEach(listener => listener());
  }
  refresh(key: string, revision: number, loader: Loader, force = false) {
    const entry = this.entry(key);
    entry.loader = loader;
    if (!force && (entry.controller ? entry.requested : entry.revision) === revision) return;
    entry.requested = revision;
    entry.pending = true;
    if (!entry.controller) void this.drain(entry);
  }
  private async drain(entry: Entry) {
    if (!entry.pending || !entry.listeners.size || !entry.loader) return;
    entry.pending = false;
    const revision = entry.requested, controller = new AbortController();
    entry.controller = controller;
    this.emit(entry, { ...entry.snapshot, loading: true, started: true });
    try {
      const data = await entry.loader(controller.signal);
      if (!controller.signal.aborted) this.emit(entry, { data, loading: false, started: true });
    } catch (error) {
      if (!controller.signal.aborted) {
        const inaccessible = error instanceof APIError && [401, 403, 404].includes(error.status);
        this.emit(entry, { data: inaccessible ? undefined : entry.snapshot.data, error, loading: false, started: true });
      }
    } finally {
      if (entry.controller === controller) {
        entry.controller = undefined; entry.revision = revision;
        // Invalidation during a request queues one follow-up instead of
        // repeatedly aborting a response that could already be useful.
        if (entry.pending) void this.drain(entry);
      }
    }
  }
  clear() {
    this.entries.forEach(entry => { entry.controller?.abort(); entry.controller = undefined; entry.pending = false; });
    this.entries.clear();
  }
}
