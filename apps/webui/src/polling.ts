import { APIError } from '@molr/sdk';

export type PollDelay = number | false;
export function retryDelay(error: unknown, failures: number): PollDelay {
  if (error instanceof APIError && [400, 401, 403, 404].includes(error.status)) return false;
  return Math.max(Math.min(60_000, 2000 * 2 ** Math.min(failures - 1, 5)), error instanceof APIError ? error.retryAt - Date.now() : 0);
}

/** A serial request loop (foreground-only unless explicitly enabled). Events cannot bypass error cooldowns. */
export function poll(task: (signal: AbortSignal) => Promise<PollDelay>, immediate = true, background = false) {
  let timer: ReturnType<typeof setTimeout> | undefined, controller: AbortController | undefined;
  let stopped = false, pending = false, failures = 0, blockedUntil = 0;
  const available = () => typeof document === 'undefined' || (background || document.visibilityState === 'visible') && navigator.onLine !== false;
  function schedule(delay: PollDelay) {
    clearTimeout(timer); timer = undefined;
    if (!stopped && delay !== false && available()) timer = setTimeout(request, Math.max(0, delay));
  }
  async function run() {
    if (stopped || controller || !pending || !available()) return;
    if (blockedUntil === Infinity) return;
    if (Date.now() < blockedUntil) { schedule(blockedUntil - Date.now()); return; }
    pending = false;
    const current = new AbortController(); controller = current;
    let delay: PollDelay = false;
    try { delay = await task(current.signal); failures = 0; blockedUntil = 0; }
    catch (error) {
      if (!current.signal.aborted) {
        delay = retryDelay(error, ++failures);
        blockedUntil = delay === false ? Infinity : Date.now() + delay;
        pending = false;
      }
    } finally {
      if (controller === current) controller = undefined;
      if (!stopped) { if (pending) void run(); else schedule(delay); }
    }
  }
  function request(reset = false) { if (reset === true && blockedUntil === Infinity) blockedUntil = 0; clearTimeout(timer); timer = undefined; pending = true; void run(); }
  const resume = () => { if (available()) request(); else { clearTimeout(timer); timer = undefined; } };
  if (typeof window !== 'undefined') {
    window.addEventListener('online', resume); window.addEventListener('offline', resume);
    document.addEventListener('visibilitychange', resume);
  }
  if (immediate) request();
  return {
    request,
    // Recompute a subscription's cadence without issuing a duplicate request.
    reschedule(delay: PollDelay) { if (!controller && blockedUntil !== Infinity) schedule(blockedUntil > Date.now() ? blockedUntil - Date.now() : delay); },
    stop() {
      stopped = true; clearTimeout(timer); controller?.abort();
      if (typeof window !== 'undefined') {
        window.removeEventListener('online', resume); window.removeEventListener('offline', resume);
        document.removeEventListener('visibilitychange', resume);
      }
    },
  };
}
