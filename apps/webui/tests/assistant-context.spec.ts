import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
const port = process.env.MOLR_TEST_PORT ?? '5193';
const base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: Record<string, unknown>) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ...((data.Mutation ?? {}) as object) } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}
test('assistant context threshold persists, reaches endpoints and follows model changes', async ({ page }, info) => {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Context ${info.project.name}-${Date.now()}`, endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [256, 128, 32].map(size => ({ Name: `${name} ${size}K`, ContextLimit: size*1024, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } })) } });
  const models = (await (await page.request.get('/api/models')).json()).filter((m: any) => m.ResourceID === resource.ID);
  const large = models.find((m: any) => m.ContextLimit === 256*1024), medium = models.find((m: any) => m.ContextLimit === 128*1024), small = models.find((m: any) => m.ContextLimit === 32*1024);
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: large.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  const get = async () => (await page.request.get(`/api/assistants/${assistant.ID}`)).json();
  const open = async () => { await page.locator('.chat-assistant-info').click(); await expect(page.getByRole('slider', { name: 'Context compression threshold' })).toBeVisible(); };
  const save = async () => { await page.locator('.assistant-info').getByRole('button', { name: 'Save', exact: true }).click(); await expect(page.getByRole('slider')).toHaveCount(0); };
  await page.goto(`/#/assistants/${assistant.ID}`); await open();
  const slider = page.getByRole('slider', { name: 'Context compression threshold' });
  await expect(slider).toHaveValue('131072');
  await expect(slider).toHaveAttribute('max', '209715');
  await page.locator('.assistant-context-field .settings-help').click();
  await expect(page.getByRole('tooltip')).toContainText('current tool batch');
  await page.keyboard.press('Escape');
  // Mouse interaction and keyboard access share the same persisted value.
  const box = (await slider.boundingBox())!;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  expect(Number(await slider.inputValue())).toBeGreaterThan(65536);
  await slider.press('End'); await expect(slider).toHaveValue('209715');
  await slider.press('Home'); await expect(slider).toHaveValue('65536');
  await save(); await page.reload(); await open(); await expect(slider).toHaveValue('65536');
  expect((await get()).ContextCompressionTokens).toBe(65536);
  await page.getByRole('button', { name: 'Restore default', exact: true }).click(); await save();
  expect((await get()).ContextCompressionTokens).toBe(0);
  const changeModel = async (id: string) => { const current = await get(); await post(page, '/api/assistants/update', { ID: current.ID, Name: current.Name, ModelID: id, Budget: current.Budget, Mutation: { ExpectedVersion: current.Version } }); await page.reload(); await open(); };
  await changeModel(medium.ID); await expect(slider).toHaveValue('104857'); await expect(slider).toBeEnabled();
  await page.locator('.assistant-info').getByRole('button', { name: 'Cancel', exact: true }).click();
  await changeModel(small.ID); await expect(slider).toHaveValue('26214'); await expect(slider).toBeDisabled();
  await expect(slider).toHaveAttribute('min', '26214'); await expect(slider).toHaveAttribute('max', '26214');
});
