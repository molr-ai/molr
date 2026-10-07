import { api } from './client';
export type RuntimeConnection = 'reconnecting' | 'connected' | 'recovered' | 'disconnected' | 'failed';
const connectionTimeout = 20_000;
const recoveryDuration = 2400;
/** Browser SSE transport with resumable reconnect and session-expiry checks. */
export function subscribeRuntime(options: { onUpdate: (event: MessageEvent) => void; onRestore: () => void; onConnection: (state: RuntimeConnection) => void }) {
  let active = true, interrupted = false, connectedBefore = false, cursor = '';
  let stream: EventSource | null = null, authCheck: AbortController | null = null;
  let deadline: ReturnType<typeof setTimeout> | undefined, recovery: ReturnType<typeof setTimeout> | undefined;
  function clearTimers() { clearTimeout(deadline); deadline = undefined; clearTimeout(recovery); recovery = undefined; }
  function closeStream() { if (stream) { stream.onopen = null; stream.onerror = null; stream.close(); stream = null; } }
  function disconnected() {
    interrupted = true; clearTimers(); closeStream();
    if (active) options.onConnection('disconnected');
  }
  function failed() {
    interrupted = true; clearTimers(); closeStream();
    if (active) options.onConnection(navigator.onLine ? 'failed' : 'disconnected');
  }
  function armDeadline() { if (deadline === undefined) deadline = setTimeout(failed, connectionTimeout); }
  function verifySession() {
    if (authCheck) return;
    const controller = new AbortController(); authCheck = controller;
    // The client handles a real 401 through the existing session-expired flow.
    void api.get('/api/auth/me', controller.signal).catch(() => {}).finally(() => { if (authCheck === controller) authCheck = null; });
  }
  function connect() {
    if (!active) return;
    clearTimers(); closeStream();
    if (!navigator.onLine) { disconnected(); return; }
    options.onConnection('reconnecting'); armDeadline();
    const current = new EventSource(`/api/events/stream${cursor ? `?after=${encodeURIComponent(cursor)}` : ''}`);
    stream = current;
    current.addEventListener('update', event => {
      if (!active || stream !== current) return;
      const message = event as MessageEvent;
      if (message.lastEventId) cursor = message.lastEventId;
      options.onUpdate(message);
    });
    current.onopen = () => {
      if (!active || stream !== current) return;
      clearTimers();
      options.onConnection(interrupted ? 'recovered' : 'connected');
      if (interrupted) recovery = setTimeout(() => { if (active && stream === current) options.onConnection('connected'); }, recoveryDuration);
      if (connectedBefore || interrupted) options.onRestore();
      connectedBefore = true; interrupted = false;
    };
    current.onerror = () => {
      if (!active || stream !== current) return;
      interrupted = true; clearTimeout(recovery); recovery = undefined;
      if (!navigator.onLine) { disconnected(); return; }
      options.onConnection('reconnecting'); armDeadline();
      verifySession();
      // Native EventSource retries transient errors with Last-Event-ID. A
      // terminal HTTP error or an attempt exceeding 20 s needs manual retry.
      if (current.readyState === EventSource.CLOSED) failed();
    };
  }
  const online = () => { if (stream?.readyState !== EventSource.OPEN) connect(); };
  const retry = () => { interrupted = true; connect(); };
  window.addEventListener('offline', disconnected); window.addEventListener('online', online);
  connect();
  const dispose = () => {
    active = false; clearTimers(); closeStream(); authCheck?.abort();
    window.removeEventListener('offline', disconnected); window.removeEventListener('online', online);
  };
  return { retry, dispose };
}
