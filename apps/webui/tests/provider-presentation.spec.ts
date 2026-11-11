import { test, expect } from '@playwright/test';

test('model menus have icons and provider menu text does not inherit heading weight', async ({ page }) => {
  await page.goto('/#/resources');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  const menu = page.getByRole('menu', { name: 'More', exact: true });
  await expect(menu).toBeVisible();
  for (const item of await menu.getByRole('menuitem').all()) await expect(item.locator('svg')).toHaveCount(1);
  await page.keyboard.press('Escape');
  const base = new URL(page.url()).origin;
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${new URL(base).port}`)!.value;
  const result = await page.request.post('/api/resources', { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: {
    Name: 'OpenAI', Kind: 'model', Provider: 'openai-compatible', Binding: { PricingProvider: 'custom', Endpoint: 'http://127.0.0.1:18882', AllowedOrigins: ['http://127.0.0.1:18882'] }, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 },
  } });
  expect(result.ok(), await result.text()).toBe(true);
  await page.goto(`/#/resources/${(await result.json()).ID}`);
  await page.reload();
  await expect(page.locator('.resources-provider-option[aria-current]').locator('[data-provider-icon]')).toHaveAttribute('data-provider-icon', 'fallback');
  await expect(page.locator('.resources-provider-option[aria-current] .lucide-bot')).toHaveCount(1);
  await page.locator('.provider-models-actions').getByRole('button', { name: 'More', exact: true }).click();
  await expect(menu).toBeVisible();
  for (const item of await menu.getByRole('menuitem').all()) await expect(item).toHaveCSS('font-weight', '400');
});

test('sandbox list uses system logos and retains the server icon for unknown systems', async ({ page }) => {
  // Keep the real sandbox response shape while presenting other platform descriptors.
  await page.route('**/api/sandboxes', async route => {
    const response = await route.fetch();
    const [local] = await response.json();
    await route.fulfill({ response, json: ['linux', 'darwin', 'windows', '', 'unknown'].map((os, index) => ({
      ...local, ID: index ? `system-${index}` : local.ID, Name: `System ${index}`, Local: index === 0,
      Info: { ...local.Info, Capabilities: { ...local.Info.Capabilities, OS: os } },
    })) });
  });
  await page.goto('/#/resources');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('tab', { name: 'Sandboxes', exact: true }).click();
  const rows = page.locator('.sandbox-item');
  await expect(rows).toHaveCount(5);
  for (const [index, expected] of ['linux', 'darwin', 'windows', 'fallback', 'fallback'].entries()) {
    await expect(rows.nth(index).locator('[data-system-icon]')).toHaveAttribute('data-system-icon', expected);
  }
  await expect(rows.nth(3).locator('.lucide-server')).toHaveCount(1);
  await expect(rows.nth(4).locator('.lucide-server')).toHaveCount(1);
});
