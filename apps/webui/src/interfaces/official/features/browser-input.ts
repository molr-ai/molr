export type BrowserAction = { action: string; [key: string]: unknown };
type Entry = { action: BrowserAction; at: number; dragging: boolean };
const encoder = new TextEncoder();
const bytes = (action: BrowserAction) => encoder.encode(JSON.stringify(action)).length;
export const batchable = (a: BrowserAction) => ['pointer', 'wheel', 'insert_text', 'press'].includes(a.action);

/** Only adjacent events can merge. Clicks, keys, text and navigation are barriers. */
export class BrowserInputBuffer {
  private entries: Entry[] = [];
  get length() { return this.entries.length; }
  clear() { this.entries = []; }
  push(action: BrowserAction, dragging = false, at = performance.now()): boolean {
    if (action.action === 'insert_text' && encoder.encode(String(action.text)).length > 8192) return false;
    const last = this.entries.at(-1);
    if (last && action.action === 'pointer' && action.event === 'move' && last.action.action === 'pointer' && last.action.event === 'move' && last.dragging === dragging && last.action.modifiers === action.modifiers) {
      if (!dragging || at - last.at < 32) { last.action = action; return true; }
      // Retain a bounded path, including its start and most recent samples.
      let start = this.entries.length - 1;
      while (start > 0 && this.entries[start - 1].dragging && this.entries[start - 1].action.action === 'pointer' && this.entries[start - 1].action.event === 'move' && this.entries[start - 1].action.modifiers === action.modifiers) start--;
      if (this.entries.length - start >= 8) this.entries.splice(start + 1, 1);
    }
    if (last && action.action === 'wheel' && last.action.action === 'wheel' && ['x', 'y', 'modifiers'].every(key => action[key] === last.action[key]) && ['delta_x', 'delta_y'].every(key => Math.sign(Number(action[key])) === Math.sign(Number(last.action[key])) && Math.abs(Number(action[key]) + Number(last.action[key])) <= 100000)) {
      last.action = { ...action, delta_x: Number(last.action.delta_x) + Number(action.delta_x), delta_y: Number(last.action.delta_y) + Number(action.delta_y) }; return true;
    }
    // Reserve room for releases so congestion cannot leave a mouse button held.
    if (this.entries.length >= 512 && action.event !== 'up' || bytes(action) > 12000 || this.entries.reduce((total, entry) => total + bytes(entry.action), 0) + bytes(action) > 65536 && action.event !== 'up') return false;
    this.entries.push({ action, at, dragging }); return true;
  }
  take(): BrowserAction[] {
    const first = this.entries.shift();
    if (!first) return [];
    const result = [first.action]; let size = bytes(first.action);
    if (!batchable(first.action)) return result;
    while (this.entries.length && result.length < 32 && batchable(this.entries[0].action) && size + bytes(this.entries[0].action) < 12000) {
      const next = this.entries.shift()!; result.push(next.action); size += bytes(next.action);
    }
    return result;
  }
  // Only the server-confirmed unexecuted suffix may return to the queue.
  prepend(actions: BrowserAction[]) { this.entries.unshift(...actions.map(action => ({ action, at: 0, dragging: true }))); }
}
