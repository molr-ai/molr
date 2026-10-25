import { test, expect } from '@playwright/test';

test('user creation retries the committed operation, rotates keys after edits and releases abandoned forms', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.locator('.workspace')).toBeVisible();
  // Also validate the worktree Runtime's actual sandbox through this UI's proxy.
  const sandbox = await page.evaluate(async () => {
    const path = '/@id/@molr/sdk'; const { api } = await import(path);
    return api.get('/api/sandbox');
  });
  expect(sandbox.Enabled).toBe(true);
  expect(sandbox.Capabilities).toMatchObject({ Reason: 'available', Shell: true, ProcessIsolation: true, FileIsolation: true, NetworkIsolation: true, ResourceLimits: true });
  await page.goto('/#/settings/users');
  const dialog = page.getByRole('dialog', { name: 'Add user', exact: true });
  const open = async (username: string) => {
    await page.getByRole('button', { name: 'Add user', exact: true }).click();
    await dialog.getByLabel('Username', { exact: true }).fill(username);
    await dialog.getByLabel('Initial password', { exact: true }).fill('synthetic-password-2026!');
  };
  const dismissNotice = async () => {
    const close = page.getByRole('list', { name: 'Notifications' }).getByRole('button', { name: 'Close', exact: true }).first();
    await expect(close).toBeVisible();
    await close.click();
  };
  const keys: string[] = [];
  let behavior: 'drop-committed' | 'unavailable' | 'success' = 'drop-committed';
  await page.route('**/api/admin/users', async route => {
    if (route.request().method() !== 'POST') return route.continue();
    keys.push(route.request().postDataJSON().Mutation.IdempotencyKey);
    if (behavior === 'drop-committed') {
      behavior = 'success';
      const response = await route.fetch();
      expect(response.ok()).toBe(true);
      await route.abort('failed');
    } else if (behavior === 'unavailable') {
      await route.fulfill({ status: 503, contentType: 'application/json', body: '{"code":"unavailable"}' });
    } else await route.continue();
  });
  const username = `retry-${Date.now()}`;
  await open(username);
  const save = dialog.getByRole('button', { name: 'Save', exact: true });
  await save.click();
  await expect.poll(() => keys.length).toBe(1);
  await expect(save).toBeEnabled();
  await dismissNotice();
  await save.click();
  await expect(dialog).not.toBeVisible();
  expect(keys[1]).toBe(keys[0]);
  const count = await page.evaluate(async username => {
    const path = '/@id/@molr/sdk'; const { api } = await import(path);
    const users = await api.all('/api/admin/users');
    return users.filter((user: { Username: string }) => user.Username === username).length;
  }, username);
  expect(count).toBe(1);

  behavior = 'unavailable';
  const changedUser = `edited-${Date.now()}`;
  await open(changedUser);
  await save.click();
  await expect.poll(() => keys.length).toBe(3);
  await expect(save).toBeEnabled();
  await dismissNotice();
  // An unrelated re-render caused by the error must not replace the key.
  await save.click();
  await expect.poll(() => keys.length).toBe(4);
  expect(keys[3]).toBe(keys[2]);
  await expect(save).toBeEnabled();
  await dismissNotice();
  await dialog.getByLabel('Initial password', { exact: true }).fill('changed-synthetic-password-2026!');
  await save.click();
  await expect.poll(() => keys.length).toBe(5);
  expect(keys[4]).not.toBe(keys[3]);
  await expect(save).toBeEnabled();
  await dismissNotice();
  // Returning to an earlier value is another edit, not a lookup in a body cache.
  await dialog.getByLabel('Initial password', { exact: true }).fill('synthetic-password-2026!');
  await save.click();
  await expect.poll(() => keys.length).toBe(6);
  expect(keys[5]).not.toBe(keys[2]);
  await expect(save).toBeEnabled();
  await dismissNotice();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await open(changedUser);
  behavior = 'success';
  await save.click();
  await expect(dialog).not.toBeVisible();
  expect(keys[6]).not.toBe(keys[5]);
  expect(errors).toEqual([]);
  if (process.env.MOLR_TEST_SESSION_FILE) await page.context().storageState({ path: process.env.MOLR_TEST_SESSION_FILE });
});
