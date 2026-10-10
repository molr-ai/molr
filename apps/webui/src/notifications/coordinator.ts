/** IndexedDB's read/write transaction elects exactly one tab per conversation sequence.
 * No message text is persisted. Storage failures fail closed for automatic alerts. */
export class NotificationClaims {
  private database?: Promise<IDBDatabase>;
  private closed = false;
  private lastPruned?: number;
  private open() {
    return this.database ??= new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('molr-notification-claims', 1);
      request.onupgradeneeded = () => request.result.createObjectStore('claims');
      request.onerror = () => reject(request.error);
      request.onsuccess = () => { if (this.closed) { request.result.close(); reject(new Error('Closed')); } else resolve(request.result); };
    });
  }
  async claim(account: string, conversation: string, sequence: number): Promise<boolean> {
    try {
      const db = await this.open();
      if (this.closed) return false;
      return await new Promise<boolean>((resolve, reject) => {
        const tx = db.transaction('claims', 'readwrite'), store = tx.objectStore('claims');
        let claimed = false, prunedAt: number | undefined;
        const key = `${account}:${conversation}`, request = store.get(key);
        request.onsuccess = () => {
          const now = Date.now();
          if (!request.result || request.result.sequence < sequence) { store.put({ sequence, at: now }, key); claimed = true; }
          // Decide after queued transactions finish, so a burst shares one scan.
          if (this.lastPruned === undefined || now < this.lastPruned || now - this.lastPruned >= 60_000) {
            prunedAt = now;
            const cursor = store.openCursor();
            cursor.onsuccess = () => { const item = cursor.result; if (item) { if (item.value.at < now - 7 * 86400_000) item.delete(); item.continue(); } };
          }
        };
        // An aborted cleanup must be retried by the next claim.
        tx.oncomplete = () => { if (prunedAt !== undefined) this.lastPruned = prunedAt; resolve(claimed); };
        tx.onerror = () => reject(tx.error);
        tx.onabort = () => reject(tx.error);
      });
    } catch { return false; }
  }
  /** One sound per account across all conversations/tabs, at most every 5 s. */
  async claimSound(account: string): Promise<boolean> {
    try {
      const db = await this.open();
      if (this.closed) return false;
      return await new Promise<boolean>((resolve, reject) => {
        const tx = db.transaction('claims', 'readwrite'), store = tx.objectStore('claims');
        const key = `sound:${account}`, request = store.get(key);
        let claimed = false;
        request.onsuccess = () => {
          const now = Date.now(), previous = request.result?.at;
          if (previous === undefined || now < previous || now - previous >= 5000) {
            store.put({ at: now }, key); claimed = true;
          }
        };
        tx.oncomplete = () => resolve(claimed);
        tx.onerror = () => reject(tx.error); tx.onabort = () => reject(tx.error);
      });
    } catch { return false; }
  }
  close() { this.closed = true; void this.database?.then(db => db.close(), () => {}); }
}
