import type { Page } from '@playwright/test';

// Keep the real authenticated stream; simulated external state changes must
// deliver the same notification as Runtime instead of relying on idle polling.
export async function installRuntimeEvents(page: Page) {
  await page.addInitScript(() => {
    const Native = window.EventSource;
    const streams = new Set<EventSource>();
    window.EventSource = class extends Native {
      constructor(url: string | URL, options?: EventSourceInit) {
        super(url, options);
        if (String(url).startsWith('/api/events/stream')) streams.add(this);
      }
      close() { streams.delete(this); super.close(); }
    };
    window.addEventListener('test:runtime-event', event => {
      const data = JSON.stringify((event as CustomEvent).detail);
      for (const stream of streams) stream.dispatchEvent(new MessageEvent('update', { data }));
    });
  });
}

export async function emitRuntimeEvent(page: Page, event: { Topic: string; Object?: { ID: string } }) {
  await page.evaluate(detail => window.dispatchEvent(new CustomEvent('test:runtime-event', { detail })), event);
}
