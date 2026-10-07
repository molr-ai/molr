/** Bounded, session-local snapshots. Nothing is persisted to browser storage. */
export class QueryCache {
  private entries = new Map<string, { value: unknown; order: number }>();
  private sequence = 0;
  next() { return ++this.sequence; }
  get<T>(key: string) { return this.entries.get(key)?.value as T | undefined; }
  set<T>(key: string, value: T, order = this.next()) {
    if ((this.entries.get(key)?.order ?? -1) > order) return;
    this.entries.delete(key);
    this.entries.set(key, { value, order });
    if (this.entries.size > 100) this.entries.delete(this.entries.keys().next().value!);
  }
  delete(key: string) { this.entries.delete(key); }
  clear() { this.entries.clear(); }
}
