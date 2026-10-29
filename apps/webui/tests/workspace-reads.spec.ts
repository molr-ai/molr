import { test, expect } from '@playwright/test';

test('workspace reads join background requests and cancel replaced or unmounted reads', async ({ page }) => {
  await page.goto('/');
  const result = await page.evaluate(async () => {
    const path = '/src/interfaces/official/features/workspace-files/read.ts';
    const { workspaceReader } = await import(path);
    const reader = workspaceReader();
    const calls: string[] = [], aborted: string[] = [], completed: string[] = [];
    const releases = new Map<string, () => void>();
    const load = (name: string) => async (signal: AbortSignal) => {
      calls.push(name);
      await new Promise<void>(resolve => {
        releases.set(name, resolve);
        signal.addEventListener('abort', () => { aborted.push(name); resolve(); }, { once: true });
      });
      if (!signal.aborted) completed.push(name);
    };
    const initial = reader.run(load('initial'));
    await Promise.resolve();
    const background = reader.run(load('background'));
    const same = initial === background;
    const next = reader.run(load('next'), true);
    await initial; await Promise.resolve();
    releases.get('next')!(); await next;
    const unmount = reader.run(load('unmount'));
    await Promise.resolve(); reader.cancel(); await unmount;
    const controller = new AbortController();
    const polling = reader.run(load('polling'), false, controller.signal);
    await Promise.resolve(); controller.abort(); await polling;
    return { same, calls, aborted, completed };
  });
  expect(result).toEqual({ same: true, calls: ['initial', 'next', 'unmount', 'polling'], aborted: ['initial', 'unmount', 'polling'], completed: ['next'] });
});

test('directory polling recovers recreated paths and resumes after an explicit access retry', async ({ page }) => {
  await page.clock.install(); await page.clock.pauseAt(new Date());
  let status = 404, calls = 0;
  await page.route('**/api/assistants/reader/workspace?*', async route => {
    calls++;
    await route.fulfill({ status, json: status === 200 ? { Entries: [{ Name: 'recovered.txt', Path: 'recovered.txt', Type: 'file', Version: 'v1' }], Version: 'directory-v1' } : { code: status === 404 ? 'not_found' : 'forbidden' } });
  });
  await page.goto('/tests/fixtures/workspace-directory.html');
  await expect(page.locator('output')).toHaveText('Error');
  expect(calls).toBe(1);
  status = 200;
  await page.clock.runFor(5000);
  await expect(page.locator('output')).toHaveText('recovered.txt');
  status = 403;
  await page.clock.runFor(5000);
  await expect(page.locator('output')).toHaveText('Error');
  const denied = calls;
  await page.clock.runFor(20000); expect(calls).toBe(denied);
  status = 200;
  await page.getByRole('button', { name: 'Reload', exact: true }).click();
  await expect(page.locator('output')).toHaveText('recovered.txt');
  await expect.poll(() => calls).toBe(denied + 2);
  await page.clock.runFor(5000);
  await expect.poll(() => calls).toBe(denied + 3);
  await page.getByRole('button', { name: 'Unmount', exact: true }).click();
  await page.clock.runFor(20000); expect(calls).toBe(denied + 3);
});
