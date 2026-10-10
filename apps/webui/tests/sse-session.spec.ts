import { test, expect } from '@playwright/test';

// Pause browser timers after connection so the 30 s session poll cannot make
// this pass: the real Runtime closes SSE and its immediate probe must sign out.
test('revoking a connected session returns to login without periodic polling', async ({ page }) => {
  await page.addInitScript(() => {
    const Native = window.EventSource;
    (window as any).runtimeConnected = false;
    window.EventSource = class extends Native {
      constructor(url: string | URL, options?: EventSourceInit) {
        super(url, options);
        if (String(url).startsWith('/api/events/stream')) this.addEventListener('open', () => { (window as any).runtimeConnected = true; });
      }
    };
  });
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as any).runtimeConnected)).toBe(true);
  await page.clock.install();
  await page.clock.pauseAt(new Date());
  const principal = await (await page.request.get('/api/auth/me')).json();
  const origin = new URL(page.url()).origin;
  const port = new URL(origin).port;
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const probe = page.waitForResponse(r => new URL(r.url()).pathname === '/api/auth/me' && r.status() === 401);
  const revoked = await page.request.delete(`/api/auth/sessions/${principal.SessionID}`, { headers: { Origin: origin, 'X-CSRF-Token': csrf } });
  expect(revoked.status()).toBe(200);
  await probe;
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible({ timeout: 3000 });
});
