import { APIError } from './api/client';
import { poll } from './polling';
import type { PollDelay } from './polling';

type Snapshot<T = unknown> = { data?: T; error?: unknown; loading: boolean; started: boolean };
type Loader = (signal: AbortSignal) => Promise<unknown>;
export type QueryInterval<T = unknown> = number | ((data: T | undefined) => PollDelay);
type Entry = {
  snapshot: Snapshot; listeners: Map<() => void, QueryInterval>; loader?: Loader;
  revision?: number; requested?: number; fetching: boolean; loop?: ReturnType<typeof poll>;
};
const empty: Snapshot = { loading: false, started: false };

/** One snapshot, serial request loop and fastest requested cadence per query. */
export class QueryStore {
  private entries = new Map<string, Entry>();
  private entry(key: string) {
    let entry = this.entries.get(key);
    if (!entry) { entry = { snapshot: empty, listeners: new Map(), fetching: false }; this.entries.set(key, entry); }
    return entry;
  }
  private interval(entry: Entry): PollDelay {
    const delays = [...entry.listeners.values()].map(value => typeof value === 'function' ? value(entry.snapshot.data) : value).filter((value): value is number => typeof value === 'number' && value > 0);
    return delays.length ? Math.min(...delays) : false;
  }
  private trim() {
    for (const [key, entry] of this.entries) {
      if (this.entries.size <= 100) break;
      if (!entry.listeners.size) this.entries.delete(key);
    }
  }
  snapshot<T>(key: string | null): Snapshot<T> { return (key ? this.entry(key).snapshot : empty) as Snapshot<T>; }
  subscribe(key: string, listener: () => void, interval: QueryInterval = 0) {
    const entry = this.entry(key);
    entry.listeners.set(listener, interval);
    entry.loop?.reschedule(this.interval(entry));
    return () => {
      entry.listeners.delete(listener);
      queueMicrotask(() => {
        if (entry.listeners.size) { entry.loop?.reschedule(this.interval(entry)); return; }
        entry.loop?.stop(); entry.loop = undefined; entry.fetching = false; entry.revision = undefined;
        entry.snapshot = { ...entry.snapshot, loading: false };
        this.trim();
      });
    };
  }
  private emit(entry: Entry, snapshot: Snapshot) {
    entry.snapshot = snapshot;
    entry.listeners.forEach((_, listener) => listener());
  }
  refresh(key: string, revision: number, loader: Loader, force = false) {
    const entry = this.entry(key); entry.loader = loader;
    if (!force && (entry.fetching ? entry.requested : entry.revision) === revision) return;
    entry.requested = revision;
    if (!entry.listeners.size) return;
    entry.loop ??= poll(async signal => {
      const requested = entry.requested; entry.fetching = true;
      this.emit(entry, { ...entry.snapshot, loading: true, started: true });
      try {
        const data = await entry.loader!(signal);
        if (!signal.aborted) this.emit(entry, { data, loading: false, started: true });
      } catch (error) {
        if (!signal.aborted) {
          const inaccessible = error instanceof APIError && [401, 403, 404].includes(error.status);
          this.emit(entry, { data: inaccessible ? undefined : entry.snapshot.data, error, loading: false, started: true });
        }
        throw error;
      } finally {
        if (!signal.aborted) { entry.fetching = false; entry.revision = requested; }
      }
      return this.interval(entry);
    }, false);
    // A new domain revision may restore access; finite rate-limit cooldowns still apply.
    entry.loop.request(true);
  }
  clear() { this.entries.forEach(entry => entry.loop?.stop()); this.entries.clear(); }
}
