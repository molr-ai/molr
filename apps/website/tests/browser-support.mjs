import { mkdir, rm, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

// A first document visit loads a separate React bundle (and transforms it in
// development). Keep this separate from ordinary 5-second DOM assertions.
export const documentReadyTimeout = 30_000;

// These contexts contain only artificial website fixtures, never login sessions.
export async function withBrowserDiagnostics(context, name, run, signal) {
  const directory = resolve(import.meta.dirname, '../test-results', name);
  const events = [];
  const record = event => {
    events.push({ at: new Date().toISOString(), ...event });
    if (events.length > 100) events.shift();
  };
  context.on('page', page => {
    page.on('pageerror', error => record({ type: 'pageerror', url: page.url(), message: error.message }));
    page.on('crash', () => record({ type: 'crash', url: page.url() }));
  });
  context.on('requestfailed', request => record({ type: 'requestfailed', url: request.url(), error: request.failure() }));
  context.on('response', response => {
    if (response.status() >= 400) record({ type: 'response', url: response.url(), status: response.status() });
  });
  let tracing = false;
  let onAbort;
  try {
    await rm(directory, { recursive: true, force: true });
    await context.tracing.start({ screenshots: true, snapshots: true });
    tracing = true;
    signal?.throwIfAborted();
    if (!signal) return await run();
    const aborted = new Promise((_, reject) => {
      onAbort = () => reject(signal.reason);
      signal.addEventListener('abort', onAbort, { once: true });
    });
    return await Promise.race([run(), aborted]);
  } catch (error) {
    // Artifact failures must not replace the assertion that failed.
    try {
      await mkdir(directory, { recursive: true });
      const pages = context.pages().filter(page => !page.isClosed());
      await writeFile(resolve(directory, 'failure.json'), JSON.stringify({
        error: String(error.stack ?? error), pages: pages.map(page => page.url()), events,
      }, null, 2));
      const captures = await Promise.allSettled(pages.map(async (page, index) => {
        await page.screenshot({ path: resolve(directory, `page-${index}.png`), timeout: 5_000 });
      }));
      for (const capture of captures) if (capture.status === 'rejected') console.error('Screenshot unavailable:', capture.reason);
      if (tracing) {
        await context.tracing.stop({ path: resolve(directory, 'trace.zip') });
        tracing = false;
      }
      console.error(`Browser failure artifacts: ${directory}`);
    } catch (captureError) {
      console.error('Browser failure diagnostics unavailable:', captureError);
    }
    throw error;
  } finally {
    if (onAbort) signal.removeEventListener('abort', onAbort);
    if (tracing) await context.tracing.stop().catch(error => console.error('Trace cleanup failed:', error));
    await context.close();
  }
}
