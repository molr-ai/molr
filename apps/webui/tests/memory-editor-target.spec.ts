import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

test('a delayed memory read cannot redirect a new draft into an existing file', async ({ page }, info) => {
  await page.goto('/#/account/memory');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.locator('.memory-workspace')).toBeVisible();
  const originalName = `original-${info.project.name}.md`, draftName = `new-draft-${info.project.name}.md`;
  const originalPath = '.agents/memory/' + originalName, draftPath = '.agents/memory/' + draftName;
  const originalText = 'Original memory must remain unchanged.', draftText = 'This text belongs only to the newly created file.';
  const dialog = page.getByRole('dialog');
  await page.getByRole('button', { name: 'New file', exact: true }).click();
  await dialog.getByLabel('Descriptive filename (including .md)').fill(originalName);
  await dialog.getByLabel('Memory text', { exact: true }).fill(originalText);
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const toastClose = page.locator('.ui-toasts').getByRole('button', { name: 'Close', exact: true });
  while (await toastClose.count()) await toastClose.first().click();
  let release!: () => void, started!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const pending = new Promise<void>(resolve => { started = resolve; });
  const matchesRead = (url: URL) => url.pathname === '/api/memories/file' && url.searchParams.get('path') === originalPath;
  await page.route(matchesRead, async route => {
    const response = await route.fetch();
    started(); await gate; await route.fulfill({ response });
  });
  try {
    await page.getByRole('button', { name: originalName, exact: true }).click();
    await pending;
    await page.getByRole('button', { name: 'New file', exact: true }).click();
    await dialog.getByLabel('Descriptive filename (including .md)').fill(draftName);
    await dialog.getByLabel('Memory text', { exact: true }).fill(draftText);
    const returned = page.waitForResponse(response => matchesRead(new URL(response.url())));
    release(); await returned;
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
    const saving = page.waitForRequest(request => request.method() === 'POST' && ['/api/memories', '/api/memories/update'].includes(new URL(request.url()).pathname));
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    const saved = await saving;
    await expect(dialog).not.toBeVisible();
    const read = (path: string) => page.request.get('/api/memories/file?' + new URLSearchParams({ scope: 'global', assistant_id: '', path }));
    const original = await (await read(originalPath)).json();
    const created = await read(draftPath);
    console.log(JSON.stringify({ savedEndpoint: new URL(saved.url()).pathname, savedPath: saved.postDataJSON().Path, originalText: original.Text, newFileStatus: created.status() }));
    expect(original.Text).toBe(originalText);
    expect(created.status()).toBe(200);
    expect((await created.json()).Text).toBe(draftText);
  } finally { release(); }
});

async function loginMemory(page: Page) {
  await page.goto('/#/account/memory');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.locator('.memory-workspace')).toBeVisible();
}
const memoryURL = (path: string) => '/api/memories/file?' + new URLSearchParams({ scope: 'global', assistant_id: '', path });
async function mutate(page: Page, endpoint: string, fields: object) {
  const origin = new URL(page.url()).origin;
  const token = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${new URL(origin).port}`)!.value;
  const result = await page.request.post(endpoint, { headers: { Origin: origin, 'X-CSRF-Token': decodeURIComponent(token) }, data: { Scope: 'global', AssistantID: '', ...fields, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(result.status(), await result.text()).toBe(200);
  const data = await result.json();
  await page.locator('.memory-workspace').getByRole('button', { name: 'Refresh', exact: true }).click();
  return data;
}
async function seedMemory(page: Page, name: string, text: string) {
  const path = '.agents/memory/' + name;
  await mutate(page, '/api/memories', { Path: path, Text: text, Context: '', Sources: [] });
  return path;
}
async function holdRead(page: Page, path: string, status?: number) {
  let release!: () => void, started!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const pending = new Promise<void>(resolve => { started = resolve; });
  const matches = (url: URL) => url.pathname === '/api/memories/file' && url.searchParams.get('path') === path;
  await page.route(matches, async route => {
    const response = status ? undefined : await route.fetch();
    started(); await gate;
    await route.fulfill(response ? { response } : { status, json: { code: status === 404 ? 'not_found' : 'invalid_argument' } });
  });
  return { pending, release, finish: async () => {
    const returned = page.waitForResponse(response => matches(new URL(response.url())));
    release(); await returned; await page.unroute(matches);
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
  } };
}

test('the latest memory read owns the editor even when an older read returns last', async ({ page }, info) => {
  await loginMemory(page);
  const first = `slow-${info.project.name}.md`, second = `fast-${info.project.name}.md`;
  const firstPath = await seedMemory(page, first, 'First original');
  const secondPath = await seedMemory(page, second, 'Second original');
  const held = await holdRead(page, firstPath);
  try {
    await page.getByRole('button', { name: first, exact: true }).click(); await held.pending;
    await page.getByRole('button', { name: second, exact: true }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByLabel('Memory text', { exact: true })).toHaveValue('Second original');
    await dialog.getByLabel('Memory text', { exact: true }).fill('Second changed');
    await held.finish();
    await expect(dialog.locator('code')).toContainText(secondPath);
    await expect(dialog.getByLabel('Memory text', { exact: true })).toHaveValue('Second changed');
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect((await (await page.request.get(memoryURL(firstPath))).json()).Text).toBe('First original');
    expect((await (await page.request.get(memoryURL(secondPath))).json()).Text).toBe('Second changed');
  } finally { held.release(); }
});

test('a memory deep link opens an independent draft and leaving the pane discards pending reads', async ({ page }, info) => {
  await loginMemory(page);
  const first = `navigation-a-${info.project.name}.md`, second = `navigation-b-${info.project.name}.md`;
  const firstPath = await seedMemory(page, first, 'Navigation first');
  const secondPath = await seedMemory(page, second, 'Navigation second');
  await page.getByRole('button', { name: first, exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Memory text', { exact: true }).fill('Unsaved first draft');
  await page.evaluate(path => { location.hash = '/account/memory?' + new URLSearchParams({ scope: 'global', path }); }, secondPath);
  await expect(dialog.getByLabel('Memory text', { exact: true })).toHaveValue('Navigation second');
  await expect(dialog.locator('code')).toContainText(secondPath);
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  const held = await holdRead(page, firstPath);
  try {
    await page.getByRole('button', { name: first, exact: true }).click(); await held.pending;
    await page.evaluate(() => { location.hash = '/account/theme'; });
    await expect(page.locator('.memory-workspace')).not.toBeVisible();
    await held.finish();
    await expect(dialog).toHaveCount(0);
    await page.evaluate(() => { location.hash = '/account/memory'; });
    await expect(page.locator('.memory-workspace')).toBeVisible();
    await expect(dialog).toHaveCount(0);
  } finally { held.release(); }
});

test('directory navigation invalidates a delayed memory read', async ({ page }, info) => {
  await loginMemory(page);
  const name = `directory-read-${info.project.name}.md`, folder = `destination-${info.project.name}`;
  const path = await seedMemory(page, name, 'Keep at original path');
  await mutate(page, '/api/memories/mkdir', { Path: '.agents/memory/' + folder });
  const held = await holdRead(page, path);
  try {
    await page.getByRole('button', { name, exact: true }).click(); await held.pending;
    await page.getByRole('button', { name: folder, exact: true }).click();
    await expect(page.getByRole('navigation', { name: 'Memory folders', exact: true })).toContainText(folder);
    await held.finish();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await page.getByRole('button', { name: 'New file', exact: true }).click();
    await expect(page.getByRole('dialog').locator('code')).toContainText('/global/.agents/memory/' + folder + '/');
  } finally { held.release(); }
});

test('a stale core-memory 404 cannot replace a new draft and a current 404 still opens core creation', async ({ page }) => {
  await loginMemory(page);
  const held = await holdRead(page, 'MEMORY.md', 404);
  try {
    await page.getByRole('button', { name: 'View / edit core memory', exact: true }).click(); await held.pending;
    await page.getByRole('button', { name: 'New file', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('Descriptive filename (including .md)').fill('kept-draft.md');
    await dialog.getByLabel('Memory text', { exact: true }).fill('Keep new draft');
    await held.finish();
    await expect(dialog.getByLabel('Descriptive filename (including .md)')).toHaveValue('kept-draft.md');
    await expect(dialog.getByLabel('Memory text', { exact: true })).toHaveValue('Keep new draft');
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await page.route(url => url.pathname === '/api/memories/file' && url.searchParams.get('path') === 'MEMORY.md', route => route.fulfill({ status: 404, json: { code: 'not_found' } }));
    await page.getByRole('button', { name: 'View / edit core memory', exact: true }).click();
    await expect(dialog.locator('code')).toHaveText('/global/MEMORY.md');
    await expect(dialog.getByLabel('Descriptive filename (including .md)')).toHaveCount(0);
    await expect(dialog.getByLabel('Memory text', { exact: true })).toHaveValue('');
  } finally { held.release(); }
});

test('late memory read errors stay discarded while current errors remain recoverable', async ({ page }, info) => {
  await loginMemory(page);
  const name = `errors-${info.project.name}.md`, path = await seedMemory(page, name, 'Recoverable file');
  const held = await holdRead(page, path, 400);
  try {
    await page.getByRole('button', { name, exact: true }).click(); await held.pending;
    await page.getByRole('button', { name: 'New file', exact: true }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
    await held.finish();
    await expect(page.locator('.ui-toast--danger:not([hidden])')).toHaveCount(0);
    await expect(page.getByRole('dialog')).toHaveCount(0);
    const matches = (url: URL) => url.pathname === '/api/memories/file' && url.searchParams.get('path') === path;
    await page.route(matches, route => route.fulfill({ status: 400, json: { code: 'invalid_argument' } }));
    await page.getByRole('button', { name, exact: true }).click();
    await expect(page.locator('.ui-toast--danger')).toBeVisible();
    await page.unroute(matches);
    await page.getByRole('button', { name, exact: true }).click();
    await expect(page.getByRole('dialog').getByLabel('Memory text', { exact: true })).toHaveValue('Recoverable file');
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
    await page.route(matches, route => route.fulfill({ status: 503, json: { code: 'unavailable' } }));
    const unavailable = page.locator('.ui-toast--danger');
    await page.getByRole('button', { name, exact: true }).click();
    await expect(unavailable).toBeVisible();
    await unavailable.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(unavailable).not.toBeVisible();
    await page.getByRole('button', { name, exact: true }).click();
    await expect(unavailable).toBeVisible();
  } finally { held.release(); }
});

test('finishing an earlier memory save cannot close a newer editor', async ({ page }, info) => {
  await loginMemory(page);
  const first = `save-a-${info.project.name}.md`, second = `save-b-${info.project.name}.md`;
  const firstPath = await seedMemory(page, first, 'Before save');
  const secondPath = await seedMemory(page, second, 'New editor original');
  await page.getByRole('button', { name: first, exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Memory text', { exact: true }).fill('Saved first file');
  let release!: () => void, started!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; }), pending = new Promise<void>(resolve => { started = resolve; });
  await page.route('**/api/memories/update', async route => {
    const response = await route.fetch(); started(); await gate; await route.fulfill({ response });
  });
  try {
    await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await pending;
    await page.evaluate(path => { location.hash = '/account/memory?' + new URLSearchParams({ scope: 'global', path }); }, secondPath);
    await expect(dialog.getByLabel('Memory text', { exact: true })).toHaveValue('New editor original');
    await dialog.getByLabel('Memory text', { exact: true }).fill('Keep this unsaved draft');
    const returned = page.waitForResponse(response => new URL(response.url()).pathname === '/api/memories/update');
    release(); await returned;
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
    await expect(dialog).toBeVisible();
    await expect(dialog.getByLabel('Memory text', { exact: true })).toHaveValue('Keep this unsaved draft');
    expect((await (await page.request.get(memoryURL(firstPath))).json()).Text).toBe('Saved first file');
    expect((await (await page.request.get(memoryURL(secondPath))).json()).Text).toBe('New editor original');
  } finally { release(); }
});

test('a delayed memory sources read cannot overlay a newer draft', async ({ page }, info) => {
  await loginMemory(page);
  const name = `sources-${info.project.name}.md`, path = await seedMemory(page, name, 'Sources original');
  const held = await holdRead(page, path);
  try {
    const row = page.locator('.memory-workspace .settings-row-item').filter({ has: page.getByRole('button', { name, exact: true }) });
    await row.getByRole('button', { name: 'Sources', exact: true }).click(); await held.pending;
    await page.getByRole('button', { name: 'New file', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('Memory text', { exact: true }).fill('Keep source-independent draft');
    await held.finish();
    await expect(dialog).toHaveCount(1);
    await expect(dialog).toHaveAccessibleName('New memory file');
    await expect(dialog.getByLabel('Memory text', { exact: true })).toHaveValue('Keep source-independent draft');
  } finally { held.release(); }
});
