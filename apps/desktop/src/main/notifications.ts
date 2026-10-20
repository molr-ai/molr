import type { AppNotification } from '@molr/ui-host/app';

// Only text and internal routes cross the bridge; no icons, files or URLs.
const routePattern = /^#\/(?:assistants(?:\/[A-Za-z0-9_-]{1,128}(?:\?panel=(?:info|permissions|channel|tasks)(?:&task=[A-Za-z0-9_-]{1,128})?)?)|plans\/[A-Za-z0-9_-]{1,128}|workbench\/[A-Za-z0-9_-]{1,128}\?event=[A-Za-z0-9_-]{1,128})$/;
function token(value: unknown): asserts value is string {
  if (typeof value !== 'string' || !value || value.length > 256 || /[\x00-\x1f]/.test(value)) throw new Error('Invalid notification identifier.');
}
function message(value: unknown): asserts value is AppNotification {
  if (!value || typeof value !== 'object') throw new Error('Invalid notification.');
  const n = value as AppNotification;
  token(n.tag);
  if (typeof n.title !== 'string' || n.title.length > 256 || typeof n.body !== 'string' || n.body.length > 4096 || typeof n.route !== 'string' || n.route !== '' && !routePattern.test(n.route)) throw new Error('Invalid notification content.');
}
export interface NativeNotice {
  on(event: 'show' | 'failed' | 'click' | 'close', callback: (...args: any[]) => void): unknown;
  show(): void;
  close(): void;
}
type Entry = { notice: NativeNotice; cancel: () => void };

/** One Runtime view, one current login scope. Native handles never leave main. */
export class DesktopNotifications {
  private scope?: string;
  private entries = new Map<string, Entry>();
  constructor(private create: (value: AppNotification) => NativeNotice, private open: (scope: string, route: string) => void, private timeout = 10_000) {}
  begin(scope: unknown) {
    token(scope);
    this.dispose(); this.scope = scope;
  }
  async show(scope: unknown, value: unknown) {
    token(scope); message(value);
    if (scope !== this.scope) throw new Error('Notification session expired.');
    this.close(scope, value.tag);
    // Bound retained native objects even if a platform never emits close.
    if (this.entries.size >= 100) this.close(scope, this.entries.keys().next().value);
    const notice = this.create(value), tag = value.tag, route = value.route;
    await new Promise<void>((resolve, reject) => {
      let settled = false;
      const finish = (error?: Error) => { if (settled) return; settled = true; clearTimeout(timer); error ? reject(error) : resolve(); };
      const current = () => this.scope === scope && this.entries.get(tag) === entry;
      const entry: Entry = { notice, cancel: () => finish(new Error('Notification canceled.')) };
      const timer = setTimeout(() => { finish(new Error('Notification delivery timed out.')); if (current()) this.close(scope, tag); }, this.timeout);
      this.entries.set(tag, entry);
      notice.on('show', () => { if (current()) finish(); else notice.close(); });
      notice.on('failed', () => { finish(new Error('System notification could not be delivered.')); if (current()) this.close(scope, tag); });
      notice.on('click', () => { if (!current()) return; this.open(scope, route); this.close(scope, tag); });
      // Keep the native handle after auto-dismissal so close() can also remove
      // it from the notification center when read, disabled or signed out.
      notice.on('close', () => finish(new Error('Notification closed.')));
      try { notice.show(); } catch (error) { finish(error instanceof Error ? error : new Error('Notification failed.')); this.close(scope, tag); }
    });
  }
  close(scope: unknown, tag?: unknown) {
    token(scope); if (tag !== undefined) token(tag);
    if (scope !== this.scope) return;
    for (const [key, entry] of this.entries) if (tag === undefined || key === tag) {
      this.entries.delete(key); entry.cancel(); entry.notice.close();
    }
  }
  dispose() {
    if (this.scope) this.close(this.scope);
    this.scope = undefined;
  }
}
