import { isChatDraft } from '@molr/sdk/ui';
import type { ChatDraft } from '@molr/sdk/ui';

/** Account-scoped drafts with stable snapshots and assistant-scoped notifications. */
export class DraftStore {
  private drafts = new Map<string, ChatDraft>();
  private listeners = new Map<string, Set<() => void>>();
  constructor(private owner: string, private storage: Pick<Storage, 'getItem' | 'setItem'>) {}
  private key(id: string) { return `molr-chat-draft:${this.owner}:${id}`; }
  snapshot(id: string): ChatDraft {
    let draft = this.drafts.get(id);
    if (!draft) {
      draft = { text: '', files: [] };
      try {
        const saved: unknown = JSON.parse(this.storage.getItem(this.key(id)) ?? 'null');
        if (isChatDraft(saved)) draft = saved;
      } catch { /* Keep a usable in-memory draft when storage is unavailable. */ }
      this.drafts.set(id, draft);
    }
    return draft;
  }
  subscribe(id: string, listener: () => void) {
    let listeners = this.listeners.get(id);
    if (!listeners) { listeners = new Set(); this.listeners.set(id, listeners); }
    listeners.add(listener);
    return () => { listeners.delete(listener); if (!listeners.size) this.listeners.delete(id); };
  }
  update(id: string, value: ChatDraft | ((previous: ChatDraft) => ChatDraft)) {
    const previous = this.snapshot(id), next = typeof value === 'function' ? value(previous) : value;
    if (next === previous || next.text === previous.text && next.files === previous.files) return;
    this.drafts.set(id, next);
    try { this.storage.setItem(this.key(id), JSON.stringify(next)); } catch { /* Navigation still retains the draft in memory. */ }
    this.listeners.get(id)?.forEach(listener => listener());
  }
}
