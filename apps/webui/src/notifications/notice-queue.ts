/** Serialize each notice; merge duplicate versions and retain a newer change. */
export class NoticeQueue {
  private pending = new Map<string, { version?: number; again: boolean }>();
  private stopped = false;
  constructor(private deliver: (id: string) => Promise<void>, private invalidate: (id: string) => void) {}
  async request(id: string, version?: number) {
    if (this.stopped) return;
    const existing = this.pending.get(id);
    if (existing) {
      if (version !== undefined && existing.version !== undefined && version <= existing.version) return;
      existing.version = version; existing.again = true; this.invalidate(id);
      return;
    }
    const entry = { version, again: false };
    this.pending.set(id, entry);
    try {
      do {
        entry.again = false;
        await this.deliver(id);
      } while (!this.stopped && entry.again);
    } finally { this.pending.delete(id); }
  }
  stop() { this.stopped = true; }
}
