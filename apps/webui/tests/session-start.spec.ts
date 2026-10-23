import { test, expect } from '@playwright/test';

for (const panel of ['browser', 'terminal']) test(`${panel} start state keeps one action, explains unavailable resources and prevents duplicate starts`, async ({ page }) => {
  let granted = false, requests = 0;
  let finish: () => void = () => {};
  const pending = new Promise<void>(resolve => { finish = resolve; });
  await page.route('**/api/auth/me', route => route.fulfill({ headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf' }, json: {} }));
  await page.route('**/api/sandbox', route => route.fulfill({ json: { Enabled: true, Capabilities: { Shell: true, Terminal: true } } }));
  await page.route('**/api/resources?*', route => route.fulfill({ json: { Items: [{ ID: 'compute', State: 'available', SandboxID: 'local', Name: 'Compute' }] } }));
  await page.route('**/api/grants?*', route => route.fulfill({ json: { Items: granted ? [{ ResourceID: 'compute', Scope: {}, Actions: ['execute'] }] : [] } }));
  await page.route('**/api/assistants/terminal-fixture/shell-tasks*', route => route.fulfill({ json: { Items: [] } }));
  await page.route('**/api/assistants/terminal-fixture/browser', async route => {
    if (route.request().method() === 'GET') return route.fulfill({ json: { Available: true, SandboxAvailable: true } });
    requests++; await pending; await route.fulfill({ status: 500, json: { code: 'internal' } });
  });
  await page.route('**/api/assistants/terminal-fixture/terminals', async route => { requests++; await pending; await route.fulfill({ status: 500, json: { code: 'internal' } }); });
  await page.goto(`/tests/fixtures/terminal-feedback.html?panel=${panel}`);
  const start = page.locator('.session-start'), button = start.getByRole('button', { name: panel === 'browser' ? 'Start browser' : 'Open terminal', exact: true });
  await expect(start).toContainText('No authorized compute resource is available.');
  if (page.viewportSize()!.width < 768) {
    const drawer = page.locator('.ui-drawer');
    await expect.poll(async () => Math.round((await drawer.boundingBox())!.height)).toBe(Math.round(page.viewportSize()!.height * 0.9));
  }
  await expect(button).toBeDisabled();
  await expect(page.getByRole('button', { name: 'New session', exact: true })).toHaveCount(0);
  granted = true;
  await page.reload();
  await expect(button).toBeEnabled();
  // Empty resource selectors must not leave a separate toolbar above the start state.
  await expect(page.locator('.terminal-panel > .execution-options')).toHaveCount(0);
  await page.setViewportSize({ width: 360, height: 420 });
  await button.scrollIntoViewIfNeeded();
  await expect(button).toBeInViewport();
  expect(await start.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
  await button.click();
  const busy = start.getByRole('button', { name: panel === 'browser' ? 'Preparing…' : 'Opening…', exact: true });
  await expect(busy).toBeDisabled();
  await expect(busy).toHaveAttribute('aria-busy', 'true');
  await busy.press('Enter');
  expect(requests).toBe(1);
  finish();
  await expect(button).toBeEnabled();
  await expect(page.locator(panel === 'browser' ? '.ui-toast--warning' : '.ui-toast--danger')).toBeVisible();
});
