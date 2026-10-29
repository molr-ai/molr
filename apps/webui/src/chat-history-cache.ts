import type { Message } from '@molr/sdk';

export type ChatSnapshot = { messages: Message[]; olderCursor: string; metadataVersions: Record<string, number> };
type Entry = { snapshot?: ChatSnapshot; ids: Set<string>; changes: Map<string, number>; readers: number; pending: number };

/**
 * Chat snapshots and pending metadata changes share one session-local lifetime.
 * Each conversation has one serial writer (useChatHistory). Retention also pins
 * canceled reads until they settle during unmount/remount; it is not a merge
 * protocol for concurrent history writers.
 */
export class ChatHistoryCache {
  private entries = new Map<string, Entry>();
  private sequence = 0;
  private entry(conversationID: string) {
    let entry = this.entries.get(conversationID);
    if (!entry) {
      entry = { ids: new Set(), changes: new Map(), readers: 0, pending: 0 };
      this.entries.set(conversationID, entry);
    }
    return entry;
  }
  private trim() {
    for (const [id, entry] of this.entries) {
      if (entry.readers || entry.pending) continue;
      if (!entry.snapshot || this.entries.size > 100) this.entries.delete(id);
    }
  }
  private pruneChanges(entry: Entry) {
    for (const [id, revision] of entry.changes) {
      if ((!entry.pending && !entry.ids.has(id)) || revision <= (entry.snapshot?.metadataVersions[id] ?? 0)) entry.changes.delete(id);
    }
  }
  get(conversationID: string) { return this.entries.get(conversationID)?.snapshot; }
  retain(conversationID: string) {
    const entry = this.entry(conversationID);
    entry.readers++;
    this.entries.delete(conversationID); this.entries.set(conversationID, entry);
    this.trim();
    return () => { entry.readers--; this.trim(); };
  }
  set(conversationID: string, snapshot: ChatSnapshot) {
    const entry = this.entry(conversationID);
    if (entry.snapshot?.messages !== snapshot.messages) entry.ids = new Set(snapshot.messages.map(message => message.ID));
    entry.snapshot = snapshot;
    this.pruneChanges(entry);
    this.entries.delete(conversationID); this.entries.set(conversationID, entry);
    this.trim();
  }
  delete(conversationID: string) {
    const entry = this.entries.get(conversationID);
    if (!entry) return;
    entry.snapshot = undefined; entry.ids.clear(); entry.changes.clear();
    this.trim();
  }
  beginRead(conversationID: string) {
    const entry = this.entry(conversationID);
    entry.pending++;
    // A scalar captures the request boundary without copying unrelated changes.
    return { revision: this.sequence, finish: () => {
      entry.pending--; this.pruneChanges(entry); this.trim();
    } };
  }
  changed(conversationID: string, messageID: string) { return this.entries.get(conversationID)?.changes.get(messageID) ?? 0; }
  invalidate(messageID: string, conversationID?: string) {
    const revision = ++this.sequence;
    const mark = (entry: Entry | undefined) => {
      // Unloaded messages will be read fresh. During a page read its IDs are not
      // known yet, so retain events until the response has been reconciled.
      if (entry && (entry.pending || entry.ids.has(messageID))) entry.changes.set(messageID, revision);
    };
    if (conversationID) mark(this.entries.get(conversationID));
    else this.entries.forEach(mark);
    return revision;
  }
  clear() { this.entries.clear(); }
}
