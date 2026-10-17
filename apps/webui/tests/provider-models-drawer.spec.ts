import { test, expect } from '@playwright/test';

test('provider drawer slides between models and configuration, restores scroll and saves', async ({ page }, info) => {
  await page.goto('/#/resources');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const base = new URL(page.url()).origin;
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${new URL(base).port}`)!.value;
  const name = `Drawer ${info.project.name} ${Date.now()}`;
  const response = await page.request.post('/api/resources', { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: {
    Name: name, Kind: 'model', Provider: 'openai-compatible', Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 },
    Binding: { Endpoint: 'http://127.0.0.1:18882', AllowedOrigins: ['http://127.0.0.1:18882'], Models: Array.from({ length: 16 }, (_, index) => ({ Name: `drawer-model-${index}`, SupportsTools: true })) },
  } });
  expect(response.ok(), await response.text()).toBe(true);
  const resource = await response.json();
  await page.reload();
  const trigger = page.getByRole('button', { name, exact: true });
  await trigger.click();
  const drawer = page.locator('.provider-models-drawer');
  await expect(drawer).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(1);
  const list = drawer.locator('.provider-models-page').first();
  const configure = list.getByRole('button', { name: 'Configure model', exact: true }).last();
  const modelName = await list.locator('li').last().locator('strong').innerText();
  await configure.scrollIntoViewIfNeeded();
  const scroll = await list.evaluate(element => element.scrollTop);
  expect(scroll).toBeGreaterThan(0);
  await configure.click();
  const back = drawer.getByRole('button', { name: 'Back', exact: true });
  await expect(back).toBeFocused();
  await expect(list).toHaveAttribute('inert', '');
  await expect(drawer).toHaveAccessibleName('Configure model');
  const context = drawer.getByLabel('Manual context limit (tokens, optional)', { exact: true });
  await context.fill('65536');
  await back.click();
  await expect(configure).toBeFocused();
  expect(await list.evaluate(element => element.scrollTop)).toBe(scroll);
  await configure.click();
  await expect(context).toHaveValue('');
  await context.fill('65536');
  await drawer.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(drawer).toHaveAccessibleName(name);
  await expect(list.locator('li').last()).toContainText('65,536 tokens');
  const catalog = await (await page.request.get('/api/models')).json();
  expect(catalog.find((model: { ResourceID: string; Name: string }) => model.ResourceID === resource.ID && model.Name === modelName).ContextLimit).toBe(65536);
  await drawer.getByRole('button', { name: 'Add model', exact: true }).click();
  await drawer.getByLabel('Model identifier', { exact: true }).fill('added-in-drawer');
  await drawer.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(drawer).toHaveAccessibleName(name);
  await expect(list).toContainText('added-in-drawer');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await list.getByRole('button', { name: 'Configure model', exact: true }).first().click();
  await expect(drawer.locator('.provider-models-pages')).toHaveCSS('transition-duration', '0s');
  await drawer.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.keyboard.press('Escape');
  await expect(drawer).not.toBeVisible();
  await expect(trigger).toBeFocused();

  // Returning while a read is pending must not reopen configuration when it completes.
  await trigger.click();
  let release!: () => void;
  const pending = new Promise<void>(resolve => { release = resolve; });
  await page.route(`**/api/resources/${resource.ID}`, async route => { await pending; await route.continue(); });
  await list.getByRole('button', { name: 'Configure model', exact: true }).first().click();
  await back.click();
  release();
  await expect(drawer).toHaveAccessibleName(name);
  await expect(drawer.locator('.provider-models-pages')).toHaveAttribute('data-detail', 'false');
  await page.unrouteAll({ behavior: 'wait' });
});
