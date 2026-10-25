import { onRequestSuccess, onSessionExpired, verifySession } from './client';
export type RuntimeSession = {
  verifySession: (signal?: AbortSignal) => Promise<unknown>;
  onRequestSuccess: (callback: () => void) => () => void;
  onSessionExpired: (callback: () => void) => () => void;
};
const defaultSession: RuntimeSession = { verifySession, onRequestSuccess, onSessionExpired };
export type RuntimeConnection = 'reconnecting' | 'connected' | 'recovered' | 'disconnected' | 'failed';
const connectionTimeout = 20_000;
const recoveryDuration = 2400;
const wakeInterval = 5000;
/** Browser SSE transport with resumable reconnect and session-expiry checks. */
export function subscribeRuntime(options: { session?: RuntimeSession; onUpdate: (event: MessageEvent) => void; onRestore: () => void; onConnection: (state: RuntimeConnection) => void }) {
  const session = options.session ?? defaultSession;
  let active = true, interrupted = false, cursor = '';
  let stream: EventSource | null = null, authCheck: AbortController | null = null;
  let deadline: ReturnType<typeof setTimeout> | undefined, recovery: ReturnType<typeof setTimeout> | undefined, reconnect: ReturnType<typeof setTimeout> | undefined;
  let retryDelay = 5000, retryAt = 0, lastAttempt = -Infinity;
  function clearTimers() { clearTimeout(deadline); deadline = undefined; clearTimeout(recovery); recovery = undefined; clearTimeout(reconnect); reconnect = undefined; retryAt = 0; }
  function closeStream() { if (stream) { stream.onopen = null; stream.onerror = null; stream.close(); stream = null; } }
  function cancelAuthCheck() { authCheck?.abort(); authCheck = null; }
  function disconnected() {
    cancelAuthCheck();
    interrupted = true; clearTimers(); closeStream();
    if (active) options.onConnection('disconnected');
  }
  function failed() {
    if (!active) return;
    if (!navigator.onLine) { disconnected(); return; }
    interrupted = true; clearTimers(); closeStream();
    options.onConnection('failed');
    schedule(Math.min(30_000, retryDelay * (0.9 + Math.random() * 0.2)));
    retryDelay = Math.min(30_000, retryDelay * 2);
  }
  function schedule(delay: number) {
    const at = Date.now() + delay;
    if (reconnect !== undefined && retryAt <= at) return;
    clearTimeout(reconnect); retryAt = at;
    reconnect = setTimeout(connect, delay);
  }
  function armDeadline() { if (deadline === undefined) deadline = setTimeout(failed, connectionTimeout); }
  function verifySession() {
    if (authCheck) return;
    const controller = new AbortController(); authCheck = controller;
    // The client handles a real 401 through the existing session-expired flow.
    void session.verifySession(controller.signal).catch(() => {}).finally(() => { if (authCheck === controller) authCheck = null; });
  }
  function connect() {
    if (!active) return;
    clearTimers(); closeStream(); cancelAuthCheck();
    if (!navigator.onLine) { disconnected(); return; }
    lastAttempt = Date.now();
    options.onConnection('reconnecting'); armDeadline();
    const current = new EventSource(`/api/events/stream${cursor ? `?after=${encodeURIComponent(cursor)}` : ''}`);
    stream = current;
    current.addEventListener('ready', event => {
      if (!active || stream !== current) return;
      const message = event as MessageEvent;
      if (message.lastEventId) cursor = message.lastEventId;
      clearTimers(); cancelAuthCheck(); retryDelay = 5000;
      options.onConnection(interrupted ? 'recovered' : 'connected');
      if (interrupted) recovery = setTimeout(() => { if (active && stream === current) options.onConnection('connected'); }, recoveryDuration);
      interrupted = false;
      // Snapshots may have loaded before the server fixed this subscription's
      // position. Refresh once after the handshake to cover that race.
      options.onRestore();
    });
    current.addEventListener('update', event => {
      if (!active || stream !== current) return;
      const message = event as MessageEvent;
      if (message.lastEventId) cursor = message.lastEventId;
      options.onUpdate(message);
    });
    current.onerror = () => {
      if (!active || stream !== current) return;
      interrupted = true; clearTimeout(recovery); recovery = undefined;
      if (!navigator.onLine) { disconnected(); return; }
      options.onConnection('reconnecting'); armDeadline();
      verifySession();
      // Native EventSource retries transient errors with Last-Event-ID. A
      // terminal HTTP error or an attempt exceeding 20 s starts backoff.
      if (current.readyState === EventSource.CLOSED) failed();
    };
  }
  function wake() {
    if (!active || stream || !navigator.onLine) return;
    const delay = Math.max(0, lastAttempt + wakeInterval - Date.now());
    if (delay) schedule(delay);
    else connect();
  }
  const visible = () => { if (document.visibilityState === 'visible') wake(); };
  const retry = () => { interrupted = true; connect(); };
  function dispose() {
    active = false; clearTimers(); closeStream(); cancelAuthCheck();
    window.removeEventListener('offline', disconnected); window.removeEventListener('online', wake); window.removeEventListener('focus', wake);
    document.removeEventListener('visibilitychange', visible);
    unsubscribeSuccess(); unsubscribeExpired();
  }
  const unsubscribeSuccess = session.onRequestSuccess(wake), unsubscribeExpired = session.onSessionExpired(dispose);
  window.addEventListener('offline', disconnected); window.addEventListener('online', wake); window.addEventListener('focus', wake);
  document.addEventListener('visibilitychange', visible);
  connect();
  return { retry, dispose };
}
