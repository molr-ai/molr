import { test, expect, type Page } from '@playwright/test';

const base = `http://127.0.0.1:${process.env.MOLR_TEST_PORT ?? process.env.MOLR_E2E_PORT ?? 5193}`;
async function login(page: Page) {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
}
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${new URL(base).port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 }, ...data } });
  expect(response.ok(), await response.text()).toBe(true);
  return response.json();
}
async function hire(page: Page, name: string) {
  const endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const provider = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [new URL(endpoint).origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  return post(page, '/api/assistants', { Name: name, ModelID: models.find((m: { ResourceID: string }) => m.ResourceID === provider.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
}
async function openPlans(page: Page, assistantID: string) {
  await page.goto(`/#/assistants/${assistantID}`);
  await page.locator('.chat-header').getByRole('button', { name: 'Plans', exact: true }).click();
  const panel = page.locator('.assistant-plans');
  await expect(panel).toBeVisible();
  return panel;
}

test('chat plans show only ten nearest assistant items in a flat list and keep an empty state', async ({ page }, info) => {
  if (info.project.name === 'desktop') await page.setViewportSize({ width: 1920, height: 1000 });
  await login(page);
  const suffix = crypto.randomUUID(), a = await hire(page, `Panel A ${suffix}`), b = await hire(page, `Panel B ${suffix}`);
  for (let i = 0; i < 12; i++) await post(page, '/api/todos', { Title: `Scoped ${suffix} ${i}`, AssistantID: a.ID, DueAt: new Date(Date.now() + (24 + i) * 3600_000).toISOString() });
  const other = await post(page, '/api/todos', { Title: `Other ${suffix}`, AssistantID: b.ID, DueAt: new Date(Date.now() + 3600_000).toISOString() });
  await post(page, '/api/todos', { Title: `Personal ${suffix}`, DueAt: new Date(Date.now() + 3600_000).toISOString() });
  const panel = await openPlans(page, a.ID), rows = panel.locator('.assistant-plan-row');
  await expect(rows).toHaveCount(10);
  await expect(rows.first()).toContainText(`Scoped ${suffix} 0`);
  await expect(rows.last()).toContainText(`Scoped ${suffix} 9`);
  await expect(panel).not.toContainText(`Other ${suffix}`);
  await expect(panel).not.toContainText(`Personal ${suffix}`);
  await expect(panel.locator('.plans-header, .plans-commandbar, .plans-statusbar, .plans-load-more, .interactive-list-row')).toHaveCount(0);
  await expect(panel.locator('.ui-dialog__header h2')).toBeVisible();
  await expect(panel.locator('.ui-dialog__header h2')).not.toHaveClass('ui-sr-only');
  await rows.first().click({ position: { x: 5, y: 5 } });
  await expect(rows.first().locator('.plan-expanded')).toBeVisible();
  const viewport = page.viewportSize()!;
  await page.setViewportSize({ width: info.project.name === 'desktop' ? 1100 : viewport.width, height: 420 });
  const body = panel.locator('.ui-dialog__body');
  await expect.poll(() => body.evaluate(e => e.scrollHeight > e.clientHeight)).toBe(true);
  await expect(panel.locator('.plans-master')).toHaveCSS('overflow-y', 'visible');
  await expect(panel.locator('.plans-content')).toHaveCSS('overflow-y', 'visible');
  await body.evaluate(e => { e.scrollTop = e.scrollHeight; });
  await expect.poll(() => body.evaluate(e => e.scrollTop)).toBeGreaterThan(0);
  await expect(panel.locator('.ui-dialog__header h2')).toBeInViewport();
  await expect(rows.last()).toBeInViewport();
  await body.evaluate(e => { e.scrollTop = 0; });
  await page.setViewportSize(viewport);
  await rows.first().getByRole('button', { name: 'Close details', exact: true }).click();
  await rows.first().getByRole('button', { name: 'Plan actions', exact: true }).click();
  await expect(page.getByRole('menu', { name: 'Plan actions' })).toBeVisible();
  await expect(rows.first().locator('.plan-expanded')).toHaveCount(0);
  await page.keyboard.press('Escape');
  await post(page, '/api/todos/delete', { ID: other.ID, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: other.Version } });
  const empty = await openPlans(page, b.ID);
  await expect(empty.locator('.plans-empty')).toBeVisible();
  await expect(empty.locator('.plans-empty button')).toHaveCount(0);
  await post(page, '/api/todos', { Title: `Undated ${suffix}`, AssistantID: b.ID });
  await page.reload();
  await page.locator('.chat-header').getByRole('button', { name: 'Plans', exact: true }).click();
  await expect(page.locator('.assistant-plan-row')).toHaveCount(1);
  await expect(page.locator('.assistant-plan-row')).toContainText(`Undated ${suffix}`);
});

test('chat scrollbar has a transparent track and hides after scrolling becomes idle', async ({ page }) => {
  await login(page);
  const assistant = await hire(page, `Scroll ${crypto.randomUUID()}`);
  const items = Array.from({ length: 30 }, (_, i) => ({ ID: `scroll-message-${i}`, ConversationID: assistant.ConversationID, Sequence: i + 1, Sender: 'user', Content: { Kind: 'text', Text: `Scroll fixture ${i}\n\n${'Long message for scrolling. '.repeat(20)}`, AttachmentIDs: null, Card: null }, CreatedAt: new Date().toISOString(), AcceptedAt: null }));
  await page.route(`**/api/conversations/${assistant.ConversationID}/messages?*`, route => route.fulfill({ json: { Items: new URL(route.request().url()).searchParams.has('after_sequence') ? [] : items, NextCursor: '' } }));
  await page.goto(`/#/assistants/${assistant.ID}`);
  const log = page.getByRole('log');
  await expect(log.locator('.ui-chat-message')).toHaveCount(30);
  await expect.poll(() => log.evaluate(e => e.scrollHeight > e.clientHeight)).toBe(true);
  await expect(log).not.toHaveAttribute('data-scrolling', '');
  await expect(log).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  await expect(log).toHaveCSS('scrollbar-width', 'none');
  await log.evaluate(e => { e.scrollTop = 0; });
  await expect(log).toHaveAttribute('data-scrolling', '');
  await expect(page.locator('.webui-scrollbar-thumb:visible').first()).toBeVisible();
  await page.waitForTimeout(600);
  await log.evaluate(e => { e.scrollTop = 200; });
  await page.waitForTimeout(600);
  await expect(log).toHaveAttribute('data-scrolling', '');
  await expect(log).not.toHaveAttribute('data-scrolling', '');
  await expect(page.locator('.webui-scrollbars')).toHaveCount(0);
  await log.evaluate(e => { e.scrollTop = 400; });
  await page.goto('/#/plans');
  await expect(page.locator('.plans-workspace')).toBeVisible();
});
