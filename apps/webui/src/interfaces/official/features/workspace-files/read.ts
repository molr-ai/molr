/** One active read per view. Background refresh joins it; navigation replaces it. */
export function workspaceReader() {
  let active: { controller: AbortController; done: Promise<void> } | undefined;
  return {
    run(load: (signal: AbortSignal) => Promise<void>, replace = false, signal?: AbortSignal): Promise<void> {
      if (active && !replace) return active.done;
      active?.controller.abort();
      const controller = new AbortController();
      const abort = () => controller.abort();
      signal?.addEventListener('abort', abort, { once: true });
      if (signal?.aborted) abort();
      const request = { controller, done: Promise.resolve() };
      request.done = Promise.resolve().then(() => {
        if (!controller.signal.aborted) return load(controller.signal);
      }).catch(error => {
        if (!controller.signal.aborted) throw error;
      }).finally(() => {
        signal?.removeEventListener('abort', abort);
        if (active === request) active = undefined;
      });
      active = request;
      return request.done;
    },
    cancel() { active?.controller.abort(); active = undefined; },
  };
}
