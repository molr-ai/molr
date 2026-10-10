import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { Assistant, FileInfo, Model } from '../src/api/types';

const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post<T>(page: Page, path: string, data: object): Promise<T> {
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}

async function setup(page: Page, suffix: string) {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Preview reuse ${suffix}-${Date.now()}`, endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post<{ ID: string }>(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000 } }] } });
  const models: Model[] = await (await page.request.get('/api/models')).json();
  const assistant = await post<Assistant>(page, '/api/assistants', { Name: name, ModelID: models.find(model => model.ResourceID === resource.ID)!.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toBeEnabled();
  return assistant;
}
const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="240" height="160"><rect width="240" height="160"/></svg>';
async function uploadFile(page: Page, name: string): Promise<FileInfo> {
  const response = page.waitForResponse(response => new URL(response.url()).pathname === '/api/files' && response.request().method() === 'POST');
  await page.locator('.chat-input input[type=file]').setInputFiles({ name, mimeType: 'image/svg+xml', buffer: Buffer.from(svg) });
  return (await response).json();
}
declare global { interface Window { previewURLs: { active: string[]; revoked: string[] } } }
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.previewURLs = { active: [], revoked: [] };
    const create = URL.createObjectURL.bind(URL), revoke = URL.revokeObjectURL.bind(URL);
    URL.createObjectURL = blob => { const url = create(blob); if (blob instanceof Blob && blob.type.startsWith('image/')) window.previewURLs.active.push(url); return url; };
    URL.revokeObjectURL = url => { window.previewURLs.active = window.previewURLs.active.filter(item => item !== url); window.previewURLs.revoked.push(url); revoke(url); };
  });
});

test('adding and removing draft images keeps unchanged previews without downloading them again', async ({ page }, info) => {
  const assistant = await setup(page, info.project.name);
  const downloads = new Map<string, number>();
  page.on('request', request => { const path = new URL(request.url()).pathname; if (request.method() === 'GET' && /^\/api\/files\/[^/]+\/content$/.test(path)) downloads.set(path, (downloads.get(path) ?? 0) + 1); });
  const draft = page.locator('.composer-attachments');
  async function upload(name: string) {
    const file = await uploadFile(page, name);
    await expect(draft.getByRole('img', { name, exact: true })).toHaveJSProperty('naturalWidth', 240);
    return file;
  }
  const first = await upload('first.svg'), firstImage = draft.getByRole('img', { name: 'first.svg', exact: true });
  const original = await firstImage.getAttribute('src'), path = `/api/files/${first.ID}/content`;
  expect(downloads.get(path)).toBe(1);
  await upload('second.svg');
  const secondURL = await draft.getByRole('img', { name: 'second.svg', exact: true }).getAttribute('src');
  await expect(firstImage).toHaveJSProperty('naturalWidth', 240);
  expect.soft(downloads.get(path)).toBe(1);
  expect.soft(await firstImage.getAttribute('src')).toBe(original);
  await draft.getByRole('button', { name: 'Remove attachment second.svg', exact: true }).click();
  await expect(draft.getByRole('img', { name: 'second.svg', exact: true })).toHaveCount(0);
  await expect(firstImage).toHaveJSProperty('naturalWidth', 240);
  expect.soft(downloads.get(path)).toBe(1);
  expect.soft(await firstImage.getAttribute('src')).toBe(original);
  expect(await page.evaluate(url => window.previewURLs.revoked.includes(url!), secondURL)).toBe(true);
  await info.attach('preview-downloads', { body: JSON.stringify({ downloads: Object.fromEntries(downloads), original, final: await firstImage.getAttribute('src') }, null, 2), contentType: 'application/json' });
  await page.evaluate(() => { location.hash = '/account/theme'; });
  await expect(draft).toHaveCount(0);
  await expect.poll(() => page.evaluate(url => window.previewURLs.revoked.includes(url!), original)).toBe(true);
  await page.evaluate(id => { location.hash = '/assistants/' + id; }, assistant.ID);
  await expect(firstImage).toHaveJSProperty('naturalWidth', 240);
  expect(await firstImage.getAttribute('src')).not.toBe(original);
});

test('unchanged pending previews survive additions and removed previews cannot publish late', async ({ page }, info) => {
  await setup(page, info.project.name);
  let release!: () => void, delayedPath = '', started!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; }), pending = new Promise<void>(resolve => { started = resolve; });
  const downloads = new Map<string, number>();
  page.on('request', request => { const path = new URL(request.url()).pathname; if (/^\/api\/files\/[^/]+\/content$/.test(path)) downloads.set(path, (downloads.get(path) ?? 0) + 1); });
  await page.route('**/api/files/*/content', async route => {
    if (delayedPath) { await route.continue(); return; }
    delayedPath = new URL(route.request().url()).pathname;
    const response = await route.fetch(); started(); await gate;
    await route.fulfill({ response }).catch(() => {}); // The removed image's request has been canceled.
  });
  try {
    const first = await uploadFile(page, 'pending.svg'); await pending;
    await uploadFile(page, 'ready.svg');
    const draft = page.locator('.composer-attachments'), ready = draft.getByRole('img', { name: 'ready.svg', exact: true });
    await expect(ready).toHaveJSProperty('naturalWidth', 240);
    const readyURL = await ready.getAttribute('src');
    expect(downloads.get(`/api/files/${first.ID}/content`)).toBe(1);
    const aborted = page.waitForEvent('requestfailed', { predicate: request => new URL(request.url()).pathname === delayedPath });
    await draft.getByRole('button', { name: 'Remove attachment pending.svg', exact: true }).click();
    release(); await aborted;
    await expect(draft.getByRole('img', { name: 'pending.svg', exact: true })).toHaveCount(0);
    await expect(ready).toHaveAttribute('src', readyURL!);
    expect(downloads.get(`/api/files/${first.ID}/content`)).toBe(1);
    await page.evaluate(() => { location.hash = '/account/theme'; });
    await expect.poll(() => page.evaluate(url => window.previewURLs.revoked.includes(url!), readyURL)).toBe(true);
  } finally { release(); }
});

test('failed image previews retry on attachment changes while successful previews stay reusable', async ({ page }, info) => {
  await setup(page, info.project.name);
  let fail = true;
  await page.route('**/api/files/*/content', async route => {
    if (fail) { fail = false; await route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":{"code":"unavailable"}}' }); }
    else await route.continue();
  });
  const first = await uploadFile(page, 'retry.svg');
  const draft = page.locator('.composer-attachments');
  await expect(draft.locator('.ui-image-gallery__error')).toBeVisible();
  const downloaded = page.waitForResponse(response => new URL(response.url()).pathname === `/api/files/${first.ID}/content` && response.status() === 200);
  await uploadFile(page, 'next.svg'); await downloaded;
  await expect(draft.getByRole('img', { name: 'retry.svg', exact: true })).toHaveJSProperty('naturalWidth', 240);
  await expect(draft.getByRole('img', { name: 'next.svg', exact: true })).toHaveJSProperty('naturalWidth', 240);
  await expect(draft.locator('.ui-image-gallery__error')).toHaveCount(0);
});
