/** Session-local revisions; a domain invalidation also reaches its scoped topics. */
export class TopicStore {
  private versions = new Map<string, number>();
  private listeners = new Map<string, Set<() => void>>();

  version(topic: string) {
    return (this.versions.get('all') ?? 0) + (this.versions.get(topic) ?? 0)
      + (topic.includes(':') ? this.versions.get(topic.split(':')[0]) ?? 0 : 0);
  }

  subscribe(topic: string, listener: () => void) {
    const keys = new Set(['all', topic, ...(topic.includes(':') ? [topic.split(':')[0]] : [])]);
    for (const key of keys) {
      let listeners = this.listeners.get(key);
      if (!listeners) { listeners = new Set(); this.listeners.set(key, listeners); }
      listeners.add(listener);
    }
    return () => {
      for (const key of keys) {
        const listeners = this.listeners.get(key);
        listeners?.delete(listener);
        if (!listeners?.size) this.listeners.delete(key);
      }
    };
  }

  invalidate(topics: string[]) {
    const pending = new Set<() => void>();
    for (const topic of new Set(topics)) {
      this.versions.set(topic, (this.versions.get(topic) ?? 0) + 1);
      this.listeners.get(topic)?.forEach(listener => pending.add(listener));
    }
    pending.forEach(listener => listener());
  }
}
