import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { currencyText } from '../src/currency';

test('money display rounds up to cents without increasing exact cents', () => {
  for (const [micros, expected] of [
    [0, '0.00'], [1, '0.01'], [9_999, '0.01'], [10_000, '0.01'],
    [10_001, '0.02'], [16_283, '0.02'], [70_000, '0.07'],
    [290_000, '0.29'], [618_558, '0.62'], [999_999, '1.00'],
    [1_000_000, '1.00'], [4_152_126, '4.16'], [4_168_409, '4.17'],
  ] as const) {
    for (const locale of ['zh', 'en']) {
      for (const Currency of ['USD', 'CNY']) {
        expect(currencyText({ Currency, Micros: micros }, locale)).toBe(`${expected} ${Currency}`);
      }
    }
  }
});

const base = `http://127.0.0.1:${process.env.MOLR_TEST_PORT ?? '5193'}`;
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${new URL(base).port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data });
  expect(response.status(), await response.text()).toBe(200);
  return response.json();
}

test('usage combines native currencies into one preferred amount and marks only conversions', async ({ page }, info) => {
  test.setTimeout(120_000);
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const originalSettings = await (await page.request.get('/api/settings')).json();
  const setCurrency = async (Currency: string) => {
    const settings = await (await page.request.get('/api/settings')).json();
    await post(page, '/api/settings', { Settings: { ...settings, Currency }, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: settings.Version } });
  };
  try {
    const origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
    for (const Currency of ['CNY', 'USD']) {
      const name = `Money ${Currency} ${info.project.name} ${Date.now()}`;
      const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency, InputMicrosPerMillion: 100_000, OutputMicrosPerMillion: 200_000, CachedInputMicrosPerMillion: 0 } }] }, Mutation: { IdempotencyKey: crypto.randomUUID() } });
      const models = await (await page.request.get('/api/models')).json();
      const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: { ResourceID: string }) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency, Micros: 1_000_000 }, Period: 'daily' }, Mutation: { IdempotencyKey: crypto.randomUUID() } });
      await expect.poll(async () => {
        const usage = await (await page.request.get(`/api/usage?assistant_id=${assistant.ID}&current_period=true`)).json();
        return usage.OriginalSpent?.some((v: { Currency: string; Micros: number }) => v.Currency === Currency && v.Micros > 0);
      }, { timeout: 60_000 }).toBe(true);
      await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).State).toBe('idle');
    }
    for (const Currency of ['CNY', 'USD']) {
      await setCurrency(Currency);
      await page.goto('/#/account/usage');
      await page.reload();
      const summary = (await (await page.request.get('/api/account/quotas')).json()).Summary;
      const rates = (await (await page.request.get('/api/settings/currency')).json()).Rates.units;
      const total = summary.Spent.reduce((sum: number, v: { Currency: string; Micros: number }) => sum + Math.ceil(v.Micros * rates[Currency] / rates[v.Currency]), 0);
      const displayed = new Intl.NumberFormat('en', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(Math.ceil(total / 10_000) / 100);
      await expect(page.locator('.usage-overview').first().locator('dd').first()).toHaveText(`≈ ${displayed} ${Currency}`);
      await expect(page.locator('.usage-overview').first().locator('dd').nth(1)).toHaveText(`0.00 ${Currency}`);
      await expect(page.locator('.usage-money').first()).not.toContainText(Currency === 'CNY' ? 'USD' : 'CNY');
    }
  } finally {
    await setCurrency(originalSettings.Currency);
  }
});
