import { readLayout } from '../../../scripts/test-support/layout.ts';
import { selectPlanRange } from './plan-controls';
import { expectFitsWebView } from './viewport';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { createHmac } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test, expect } from '@playwright/test';
import type { Locator, Page, Request, Route } from '@playwright/test';
const base = `http://127.0.0.1:${process.env.MOLR_TEST_PORT ?? process.env.MOLR_E2E_PORT ?? 5193}`;
const csrfCookie = `molr_dev_csrf_p${new URL(base).port}`, sessionCookie = `molr_dev_session_p${new URL(base).port}`;
const modelOrigin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
// Resource cards keep model and provider actions in a menu.
async function cardAction(page: Page, card: Locator, name: string) {
  const drawer = page.locator('.provider-models-drawer');
  if (await drawer.isVisible()) { await drawer.locator('.ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click(); await expect(drawer).not.toBeVisible(); }
  await card.getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menu', { name: 'More', exact: true }).getByRole('menuitem', { name, exact: true }).click();
}
// Desktop exposes settings in the sidebar; mobile exposes the account in the assistant list.
async function openUserSettings(page: Page) {
  const drawer = page.locator('.provider-models-drawer');
  if (await drawer.isVisible()) await drawer.locator('.ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click();
  const navigation = page.getByRole('navigation', { name: 'Molr', exact: true });
  const sidebar = navigation.getByRole('button', { name: 'User settings', exact: true });
  if (await sidebar.isVisible()) await sidebar.click();
  else {
    const account = page.getByRole('button', { name: /^User settings · / });
    if (!(await account.isVisible())) {
      const back = page.getByRole('button', { name: 'Back to assistants', exact: true });
      if (await back.isVisible()) {
        // Dismissal may already be returning to the list while the old chat fades out.
        await back.or(account).first().click();
        await expect(account.or(page.locator('.settings-nav'))).toBeVisible();
        if (await page.locator('.settings-nav').isVisible()) return;
      } else {
        const toastClose = page.locator('.ui-toasts').getByRole('button', { name: 'Close', exact: true });
        while (await toastClose.count()) await toastClose.first().click();
        await navigation.getByRole('button', { name: 'Assistants', exact: true }).click();
      }
    }
    await account.click();
  }
  await expect(page.locator('.settings-nav')).toBeAttached();
}
// Settings use a master/detail layout: a section nav plus the visible pane.
const settingsNavItem = (page: Page, name: string) => page.locator('.settings-nav > button').filter({ hasText: new RegExp(`^${name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`) });
async function openSettingsSection(page: Page, name: string) {
  const item = settingsNavItem(page, name);
  await item.waitFor({ state: 'attached' });
  if (!(await item.isVisible())) await page.locator('.settings-header').getByRole('button', { name: 'Back', exact: true }).click();
  await item.click();
}
const settingsPane = (page: Page) => page.locator('.settings-pane:visible');
// Provider rows open model lists in a drawer.
const resourceRow = (page: Page, name: string) => page.locator('.resource-row').filter({ has: page.getByRole('heading', { name, exact: true }) });
const resourceCategory = (page: Page, name: string) => page.getByRole('tab', { name, exact: true });
async function openProviderModels(row: Locator) {
  await row.locator('h2 button').click();
}
async function login(page: Page) {
  await page.goto('/');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
}
async function choose(page: Page, control: string, option: string | RegExp) {
  await page.getByRole('combobox', { name: control, exact: true }).click();
  await page.getByRole('option', { name: option, exact: typeof option === 'string' }).click();
}
async function post<T = any>(page: Page, path: string, data: object, mutation = true): Promise<T> {
  const token = (await page.context().cookies()).find(cookie => cookie.name === csrfCookie)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': token }, data: mutation ? { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 } } : data });
  expect(response.status(), await response.text()).toBe(200); return await response.json();
}

async function verifyIdentity(page: Page, code?: string) {
  const dialog = page.getByRole('dialog', { name: 'Verify identity', exact: true });
  await dialog.getByLabel('Runtime sign-in password', { exact: true }).fill('webui-e2e-password-2026!');
  if (code) await dialog.getByLabel('Authenticator or recovery code', { exact: true }).fill(code);
  await dialog.getByRole('button', { name: 'Verify', exact: true }).click();
  await expect(dialog).not.toBeVisible();
}

test('real authentication restores across reload and tabs; proxy preserves CSRF protection', async ({ page, context }) => {
  await login(page);
  const cookies = await context.cookies();
  expect(cookies.find(cookie => cookie.name === sessionCookie)?.httpOnly).toBe(true);
  expect(cookies.find(cookie => cookie.name === csrfCookie)?.httpOnly).toBe(false);
  expect(await page.evaluate(() => document.cookie)).not.toContain('molr_dev_session');
  await page.reload(); await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const second = await context.newPage(); await second.goto('/'); await expect(second.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const bad = await page.request.post('/api/todos', { headers: { Origin: 'https://attacker.invalid', 'X-CSRF-Token': cookies.find(c => c.name === csrfCookie)!.value }, data: {} });
  expect(bad.status()).toBe(403);
  const missing = await page.request.post('/api/todos', { headers: { Origin: base }, data: {} }); expect(missing.status()).toBe(403);
  await page.evaluate(() => { location.hash = '#/account/security'; });
  await openSettingsSection(page, 'Security'); await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  await expect(second.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible({ timeout: 15000 });
});

test('model setup, automatic hire permission, real agent reply, attachments and persisted feedback', async ({ page }, info) => {
  await login(page);
  const suffix = `${info.project.name}-${Date.now()}`, resourceName = `Model ${suffix}`, assistantName = `Assistant ${suffix}`;
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
  const form = page.getByRole('dialog', { name: 'Connect provider', exact: true });
  await form.getByRole('button', { name: 'Custom service', exact: true }).click();
  await form.getByText('More configuration', { exact: true }).click();
  await form.getByRole('textbox', { name: 'Name', exact: true }).fill(resourceName);
  await form.getByRole('textbox', { name: 'Service URL', exact: true }).fill(modelOrigin);
  await form.getByRole('button', { name: 'Connect provider', exact: true }).click(); await expect(form).not.toBeVisible();
  await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
  const providerCard = resourceRow(page, resourceName);
  await openProviderModels(providerCard);
  await cardAction(page, providerCard, 'Add model');
  const modelForm = page.getByRole('dialog', { name: 'Add model', exact: true });
  await expect(modelForm.getByLabel('API Key', { exact: false })).toHaveCount(0);
  await modelForm.getByRole('textbox', { name: 'Model identifier', exact: true }).fill(resourceName);
  for (const label of ['Input price (USD / million tokens)', 'Output price (USD / million tokens)', 'Cached input price (USD / million tokens)']) await modelForm.getByRole('spinbutton', { name: label, exact: true }).fill('0');
  await modelForm.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(modelForm).not.toBeVisible();
  await page.locator('.provider-models-drawer > .ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click();
  await expect(providerCard).toContainText(resourceName);
  await page.getByRole('navigation').getByRole('button', { name: 'Assistants', exact: true }).click();
  await page.getByRole('button', { name: 'Hire new assistant', exact: true }).first().click();
  const hire = page.getByRole('dialog', { name: 'Hire new assistant', exact: true });
  await hire.getByRole('textbox', { name: 'Assistant name' }).fill(assistantName);
  await choose(page, 'Model', resourceName);
  await hire.getByRole('spinbutton', { name: 'Spending limit (USD)', exact: true }).fill('0');
  await hire.getByRole('button', { name: 'Hire new assistant', exact: true }).click(); await expect(hire).not.toBeVisible();
  await expect(page.locator('.chat-header h1')).toHaveText(assistantName);
  // Birth publishes its own reply. Finish that turn before testing user feedback.
  await expect(page.locator('.ui-chat-message--assistant')).toHaveCount(1, { timeout: 20000 });
  const assistantID = new URL(page.url()).hash.match(/^#\/assistants\/([^?]+)/)![1];
  const assistant = await (await page.request.get(`/api/assistants/${assistantID}`)).json();
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistantID}/status`)).json()).State).toBe('idle');
  const before = await (await page.request.get(`/api/conversations/${assistant.ConversationID}`)).json();
  const savedNotices = page.locator('.ui-toasts').getByRole('button', { name: 'Close', exact: true });
  while (await savedNotices.count()) await savedNotices.first().click();
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Please acknowledge this real message.');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.locator('.ui-chat-message--assistant')).toHaveCount(2, { timeout: 20000 });
  // Birth and user-turn responses can have identical fixture text. Bind feedback
  // assertions to the new persisted message rather than the last visible text.
  let replyID = '';
  await expect.poll(async () => {
    const messages = await (await page.request.get(`/api/conversations/${assistant.ConversationID}/messages?after_sequence=${before.LastSequence}`)).json();
    replyID = messages.Items.find((message: { ID: string; Sender: string }) => message.Sender === 'assistant')?.ID ?? '';
    return replyID;
  }, { timeout: 20000 }).not.toBe('');
  const replyArticle = page.locator(`[data-message-id="${replyID}"] .ui-chat-message--assistant`);
  const reply = replyArticle.getByText('Runtime delivered this reply through conversation_publish.', { exact: true });
  await expect(reply).toBeVisible();
  await expect(page.getByText('Internal reasoning is not a chat message.', { exact: true })).toHaveCount(0);
  await replyArticle.getByRole('button', { name: 'Like', exact: true }).click(); await expect(replyArticle.getByRole('button', { name: 'Like', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(replyArticle.locator('.message-feedback')).toHaveAttribute('aria-busy', 'false');
  await page.reload(); await expect(reply).toBeVisible(); await expect(replyArticle.getByRole('button', { name: 'Like', exact: true })).toHaveAttribute('aria-pressed', 'true');
  const like = replyArticle.getByRole('button', { name: 'Like', exact: true });
  const dislike = replyArticle.getByRole('button', { name: 'Dislike', exact: true });
  await expect(replyArticle.locator('[data-animate]')).toHaveCount(0);
  await expect(like).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  await expect(like).toHaveCSS('border-width', '0px');
  await expect(like.locator('.message-feedback__glyph')).toHaveCSS('width', '15px');
  await expect(like.locator('.message-feedback__glyph')).toHaveCSS('stroke-width', '1.7px');
  await expect(replyArticle.locator('.message-feedback')).toHaveCSS('gap', '8px');
  const selectedColor = await like.evaluate(el => getComputedStyle(el).color);
  await expect(like.locator('.message-feedback__cuff')).toHaveCSS('fill', selectedColor);
  expect(await like.locator('.message-feedback__palm').evaluate(el => getComputedStyle(el).fill)).not.toBe(selectedColor);
  await dislike.focus();
  await dislike.press('Space');
  await expect(dislike).toHaveAttribute('aria-pressed', 'true');
  await expect(like).toHaveAttribute('aria-pressed', 'false');
  await expect(dislike.locator('[data-animate]')).toHaveCount(0);
  await expect(replyArticle.locator('.message-feedback')).toHaveAttribute('aria-busy', 'false');
  await page.reload();
  await expect(dislike).toHaveAttribute('aria-pressed', 'true');
  await expect(replyArticle.locator('[data-animate]')).toHaveCount(0);
  await dislike.click();
  await expect(dislike).toHaveAttribute('aria-pressed', 'false');
  await expect(replyArticle.locator('[data-animate]')).toHaveCount(0);
  await expect(replyArticle.locator('.message-feedback')).toHaveAttribute('aria-busy', 'false');
  await page.reload();
  await expect(dislike).toHaveAttribute('aria-pressed', 'false');
  await expect(like).toHaveAttribute('aria-pressed', 'false');
  // The account preference suppresses decoration as well as movement.
  await page.locator('html').evaluate(el => el.setAttribute('data-reduced-motion', 'true'));
  await like.click();
  await expect(like).toHaveAttribute('aria-pressed', 'true');
  await expect(like.locator('.message-feedback__motion')).toHaveCSS('animation-name', 'none');
  await expect(like.locator('.message-feedback__accent')).toBeHidden();
  await page.locator('html').evaluate(el => el.removeAttribute('data-reduced-motion'));

  await page.locator('input[type="file"]').setInputFiles({ name: '测试资料.txt', mimeType: 'text/plain', buffer: Buffer.from('real attachment') });
  await expect(page.locator('.composer-attachments')).toContainText('测试资料.txt');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.getByRole('link', { name: /测试资料.txt/ })).toBeVisible();
  const downloadEvent = page.waitForEvent('download');
  await page.getByRole('link', { name: /测试资料.txt/ }).click();
  const download = await downloadEvent; expect(download.suggestedFilename()).toBe('测试资料.txt');
  const downloadedPath = await download.path(); expect(await readFile(downloadedPath!, 'utf8')).toBe('real attachment');
  await page.screenshot({ path: `/tmp/molr-webui-${info.project.name}.png` });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('feedback responds immediately, isolates pending writes and restores failures', async ({ page }, info) => {
  test.setTimeout(90_000);
  await login(page);
  const assistant = await seedAssistant(page, `Feedback transition ${info.project.name}-${Date.now()}`);
  await page.goto(`/#/assistants/${assistant.ID}`);
  await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'First feedback example' } });
  const replies = page.locator('.ui-chat-message--assistant');
  await expect(replies).toHaveCount(1, { timeout: 20_000 });
  await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'Second feedback example' } });
  await expect(replies).toHaveCount(2, { timeout: 20_000 });
  const first = replies.first(), second = replies.last();
  const like = first.getByRole('button', { name: 'Like', exact: true });
  const dislike = first.getByRole('button', { name: 'Dislike', exact: true });
  let release!: () => void, entered!: () => void, firstWrites = 0;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const started = new Promise<void>(resolve => { entered = resolve; });
  const messageID = await first.locator('..').getAttribute('data-message-id');
  const feedbackPath = '**/api/conversations/feedback';
  await page.route(feedbackPath, async route => {
    if (route.request().postDataJSON().MessageID === messageID) { firstWrites++; entered(); await gate; }
    await route.continue();
  });
  await page.evaluate(() => {
    (window as any).feedbackAnimations = [];
    document.addEventListener('animationstart', event => (window as any).feedbackAnimations.push(event.animationName));
  });
  try {
    await like.click(); await started;
    await expect(like).toHaveAttribute('aria-pressed', 'true');
    await expect(first.locator('.message-feedback')).toHaveAttribute('aria-busy', 'true');
    await expect.poll(() => page.evaluate(() => (window as any).feedbackAnimations.includes('feedback-lift'))).toBe(true);
    await expect(like.locator('.message-feedback__pending')).toHaveCSS('opacity', '1');
    await expect(like).toHaveAttribute('title', 'Saving like…');
    await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toBeEnabled();
    await dislike.evaluate((el: HTMLButtonElement) => el.click());
    await like.evaluate((el: HTMLButtonElement) => el.click());
    expect(firstWrites).toBe(1);
    await expect(like).toHaveAttribute('aria-pressed', 'true');
    // A different message remains independently writable.
    await second.getByRole('button', { name: 'Dislike', exact: true }).click();
    await expect(second.locator('.message-feedback')).toHaveAttribute('aria-busy', 'false');
    await expect(second.getByRole('button', { name: 'Dislike', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await first.screenshot({ path: info.outputPath('feedback-saving.png') });
  } finally { release(); }
  await expect(first.locator('.message-feedback')).toHaveAttribute('aria-busy', 'false');
  await page.unroute(feedbackPath);
  await expect(like).toHaveAttribute('aria-pressed', 'true');
  expect(await page.evaluate(() => (window as any).feedbackAnimations.filter((name: string) => name === 'feedback-lift').length)).toBe(1);
  await page.reload(); await expect(like).toHaveAttribute('aria-pressed', 'true');
  await expect(first.locator('[data-animate]')).toHaveCount(0);

  // Definite save failure restores the previously confirmed selection.
  let fail!: () => void;
  const failure = new Promise<void>(resolve => { fail = resolve; });
  await page.route(feedbackPath, async route => { await failure; await route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ code: 'forbidden' }) }); });
  try {
    await dislike.click();
    await expect(dislike).toHaveAttribute('aria-pressed', 'true');
    await expect(like).toHaveAttribute('aria-pressed', 'false');
    await expect(first.locator('.message-feedback')).toHaveAttribute('aria-busy', 'true');
  } finally { fail(); }
  await expect(first.locator('.message-feedback')).toHaveAttribute('aria-busy', 'false');
  await expect(like).toHaveAttribute('aria-pressed', 'true');
  await expect(dislike).toHaveAttribute('aria-pressed', 'false');
  await expect(page.getByRole('list', { name: 'Notifications', exact: true })).toContainText('Could not save feedback. Please try again.');
  await page.unroute(feedbackPath);

  // Acknowledged changes survive delayed metadata reads without flickering back.
  let releaseMetadata!: () => void, metadataFetched!: () => void, metadataDelivered!: () => void;
  const metadataGate = new Promise<void>(resolve => { releaseMetadata = resolve; });
  const staleRead = new Promise<void>(resolve => { metadataFetched = resolve; });
  const delivered = new Promise<void>(resolve => { metadataDelivered = resolve; });
  const metadataPattern = `**/api/conversations/${assistant.ConversationID}/messages?*`;
  let captured = false;
  const metadataHandler = async (route: import('@playwright/test').Route) => {
    if (captured || new URL(route.request().url()).searchParams.get('message_id') !== messageID) { await route.continue(); return; }
    captured = true;
    const response = await route.fetch();
    expect((await response.json()).Items[0].Feedback).toBe('dislike');
    metadataFetched(); await metadataGate; await route.fulfill({ response }); metadataDelivered();
  };
  await page.route(metadataPattern, metadataHandler);
  let freshRead: Promise<unknown> | undefined;
  try {
    await dislike.click();
    await expect(first.locator('.message-feedback')).toHaveAttribute('aria-busy', 'false');
    await staleRead;
    await expect(dislike).toHaveAttribute('aria-pressed', 'true');
    freshRead = page.waitForResponse(async response => {
      if (new URL(response.url()).searchParams.get('message_id') !== messageID) return false;
      return (await response.json()).Items?.[0]?.Feedback === 'like';
    });
    await like.click();
    await expect(first.locator('.message-feedback')).toHaveAttribute('aria-busy', 'false');
    await expect(like).toHaveAttribute('aria-pressed', 'true');
    await like.evaluate(el => {
      (window as any).feedbackFlashes = [];
      new MutationObserver(() => { if (el.getAttribute('aria-pressed') !== 'true') (window as any).feedbackFlashes.push(false); }).observe(el, { attributes: true, attributeFilter: ['aria-pressed'] });
    });
  } finally {
    releaseMetadata();
    if (captured) await delivered;
    await page.unroute(metadataPattern, metadataHandler);
  }
  await freshRead;
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(resolve)));
  expect(await page.evaluate(() => (window as any).feedbackFlashes)).toEqual([]);
  await expect(like).toHaveAttribute('aria-pressed', 'true');
  await page.reload(); await expect(like).toHaveAttribute('aria-pressed', 'true');

  // Cancel is immediate too; reduced motion keeps pending dots static.
  const cancellation = await holdRead(page, feedbackPath);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  try {
    await like.click(); await cancellation.started;
    await expect(like).toHaveAttribute('aria-pressed', 'false');
    await expect(like).toHaveAttribute('title', 'Removing feedback…');
    await expect(like.locator('.message-feedback__pending i').first()).toHaveCSS('animation-name', 'none');
    await expect(like.locator('.message-feedback__motion')).toHaveCSS('animation-name', 'none');
  } finally { await cancellation.release(); }
  await expect(first.locator('.message-feedback')).toHaveAttribute('aria-busy', 'false');
  await page.reload(); await expect(like).toHaveAttribute('aria-pressed', 'false');
  await expect(dislike).toHaveAttribute('aria-pressed', 'false');
});

test('todos, schedules and memory persist through real APIs and browser reload', async ({ page }, info) => {
  await login(page); const suffix = `${info.project.name}-${Date.now()}`;
  await page.getByRole('navigation').getByRole('button', { name: 'Plans', exact: true }).click();
  await page.getByRole('button', { name: 'New plan', exact: true }).first().click();
  await page.getByRole('textbox', { name: 'What needs doing?', exact: true }).fill(`Todo ${suffix}`);
  await page.getByRole('dialog').getByRole('button', { name: 'Save', exact: true }).click();
  await selectPlanRange(page, /No date/);
  const personal = page.locator('.plan-row').filter({ has: page.getByRole('heading', { name: `Todo ${suffix}`, exact: true }) });
  await personal.getByRole('button', { name: /Execution history/ }).click(); await page.getByRole('region', { name: `Todo ${suffix} · Plan details`, exact: true }).getByRole('button', { name: 'Complete this execution' }).click();
  await page.getByRole('dialog', { name: 'Complete this execution' }).getByRole('button', { name: 'Complete this execution' }).click();
  await expect(personal).toHaveAttribute('data-completed', 'true'); await page.reload();
  await selectPlanRange(page, /No date/);
  await expect(personal).toHaveAttribute('data-completed', 'true');
  await page.getByRole('navigation').getByRole('button', { name: 'Plans', exact: true }).click();
  await page.getByRole('button', { name: 'New plan', exact: true }).first().click();
  await page.getByRole('dialog').getByRole('combobox', { name: 'Plan type', exact: true }).click();
  await page.getByRole('option', { name: 'Time block', exact: true }).click();
  await page.getByRole('textbox', { name: 'What needs doing?', exact: true }).fill(`Schedule ${suffix}`);
  const savedBlock = page.waitForResponse(r => r.url().endsWith('/api/schedules') && r.request().method() === 'POST');
  await page.getByRole('dialog').getByRole('button', { name: 'Save', exact: true }).click();
  const block = await (await savedBlock).json();
  const eventRange = () => selectPlanRange(page, Date.parse(block.Trigger.StartAt) > Date.now() ? 'Upcoming' : /^Overdue/);
  await eventRange();
  await expect(page.getByRole('heading', { name: `Schedule ${suffix}`, exact: true })).toBeVisible();
  await page.reload(); await eventRange(); await expect(page.getByRole('heading', { name: `Schedule ${suffix}`, exact: true })).toBeVisible();
  await openUserSettings(page);
  await openSettingsSection(page, 'Memory'); await page.getByRole('button', { name: 'New file', exact: true }).click();
  await page.getByLabel('Descriptive filename (including .md)').fill(`reload-preference-${info.project.name}.md`);
  await page.getByRole('textbox', { name: 'Memory text' }).fill(`Memory ${suffix}`);
  await page.getByRole('dialog').getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.reload(); await openSettingsSection(page, 'Memory');
  await page.getByRole('button', { name: `reload-preference-${info.project.name}.md`, exact: true }).click();
  await expect(page.getByRole('dialog').getByRole('textbox', { name: 'Memory text' })).toHaveValue(`Memory ${suffix}`);
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
  await openSettingsSection(page, 'Theme');
  const saveMotion = page.waitForResponse(response => response.url().endsWith('/api/settings') && response.request().method() === 'POST' && response.status() === 200);
  await page.getByRole('switch', { name: 'Reduce motion', exact: true }).check(); await saveMotion;
  await page.reload();
  await openSettingsSection(page, 'Theme');
  await expect(page.getByRole('switch', { name: 'Reduce motion', exact: true })).toBeChecked();
  await expect(page.locator('html')).toHaveAttribute('data-reduced-motion', 'true');
  await page.getByRole('switch', { name: 'Reduce motion', exact: true }).uncheck();
  await expect(page.locator('html')).toHaveAttribute('data-reduced-motion', 'false');
});

test('message pagination and optimistic version conflicts are real, not simulated', async ({ page }) => {
  await login(page);
  const resource = await post(page, '/api/resources', { Name: 'Pagination model', Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: modelOrigin, AllowedOrigins: [modelOrigin], Models: [{ Name: 'pagination', ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } });
  const assistant = await post(page, '/api/assistants', { Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' }, Name: 'Pagination assistant', ModelID: (await (await page.request.get('/api/models')).json()).find((model: { ResourceID: string }) => model.ResourceID === resource.ID).ID });
  for (let index = 0; index < 55; index++) await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: `History ${index}` } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await expect(page.getByRole('log').getByText('History 54', { exact: true })).toBeVisible();
  await expect(page.getByRole('log').getByText('History 0', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Load earlier messages', exact: true }).click();
  await expect(page.getByRole('log').getByText('History 0', { exact: true })).toBeVisible();
  const todo = await post(page, '/api/todos', { Title: 'Conflict test' });
  const token = (await page.context().cookies()).find(cookie => cookie.name === csrfCookie)!.value;
  const data = { ID: todo.ID, Title: 'First change', Mutation: { ExpectedVersion: todo.Version, IdempotencyKey: crypto.randomUUID() } };
  const first = await page.request.post('/api/todos/update', { headers: { Origin: base, 'X-CSRF-Token': token }, data }); expect(first.status()).toBe(200);
  const stale = await page.request.post('/api/todos/update', { headers: { Origin: base, 'X-CSRF-Token': token }, data: { ...data, Title: 'Stale change', Mutation: { ...data.Mutation, IdempotencyKey: crypto.randomUUID() } } }); expect(stale.status()).toBe(409);
});

async function seedAssistant(page: Page, name: string) {
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: modelOrigin, AllowedOrigins: [modelOrigin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } });
  return await post(page, '/api/assistants', { Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' }, Name: name, ModelID: (await (await page.request.get('/api/models')).json()).find((model: { ResourceID: string }) => model.ResourceID === resource.ID).ID });
}
async function assistantMenu(page: Page, label: string) { await page.locator('.chat-header').getByRole('button', { name: 'More', exact: true }).click(); await page.getByRole('menuitem', { name: label, exact: true }).click(); }

test('waiting notices explain real pricing, permission and budget blockers', async ({ page }, info) => {
  test.setTimeout(90_000);
  await login(page);
  for (const reason of ['model_pricing_missing', 'model_not_authorized', 'budget_exceeded']) {
    const name = `${reason}-${info.project.name}-${Date.now()}`;
    const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: modelOrigin, AllowedOrigins: [modelOrigin], Models: [{ Name: name, ContextLimit: 8192, SupportsTools: true, Pricing: reason === 'model_pricing_missing' ? null : { Currency: 'USD', InputMicrosPerMillion: reason === 'budget_exceeded' ? 1000000 : 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } });
    const model = (await (await page.request.get('/api/models')).json()).find((model: { ResourceID: string }) => model.ResourceID === resource.ID);
    const assistant = await post(page, '/api/assistants', { Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' }, Name: name, ModelID: model.ID });
    if (reason === 'model_not_authorized') {
      const grants = (await (await page.request.get(`/api/grants?assistant_id=${assistant.ID}`)).json()).Items;
      await post(page, '/api/grants/revoke', { GrantID: grants[0].ID, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: grants[0].Version } }, false);
    }
    await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'Check this execution requirement.' } });
    await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).WaitingReason, { timeout: 15000 }).toBe(reason);
    await page.goto(`/#/assistants/${assistant.ID}`);
    const notice = page.getByRole('status').filter({ hasText: 'Assistant needs attention' });
    await expect(notice).toBeVisible();
    if (reason === 'model_pricing_missing') {
      await expect(notice).toContainText('Model prices are not configured');
      await expect(notice.getByRole('button', { name: 'Manage model access', exact: true })).toHaveCount(0);
      // A stored generic failure can explain a known current pricing blocker too.
      await page.route(`**/api/assistants/${assistant.ID}/status`, route => route.fulfill({ json: { AssistantID: assistant.ID, State: 'waiting', WaitingReason: 'failed_precondition' } }));
      await page.reload();
      await expect(notice).toContainText('Model prices are not configured');
      await notice.getByRole('button', { name: 'Configure model prices', exact: true }).click();
      await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
    } else if (reason === 'model_not_authorized') {
      await expect(notice).toContainText('The assistant cannot call this model');
      await notice.getByRole('button', { name: 'Manage model access', exact: true }).click();
      await expect(page.getByRole('dialog', { name: 'Assistant information', exact: true })).toBeVisible();
    } else {
      await expect(notice).toContainText('remaining allowance does not cover');
      await notice.getByRole('button', { name: 'Adjust budget', exact: true }).click();
      await expect(page.getByRole('dialog', { name: 'Assistant information', exact: true })).toBeVisible();
      await expect(page.getByRole('spinbutton', { name: 'Spending limit (USD)', exact: true })).toBeVisible();
      await page.getByRole('dialog', { name: 'Assistant information', exact: true }).getByRole('button', { name: 'Cancel', exact: true }).click();
    }
  }
});

test('resource permissions follow capabilities and clear irrelevant scopes', async ({ page }, info) => {
  const directory = await mkdtemp(join(tmpdir(), 'molr-permissions-'));
  try {
    await login(page);
    const name = `Permissions ${info.project.name}-${Date.now()}`;
    const assistant = await seedAssistant(page, name);
    const storage = await post(page, '/api/resources', { Name: `${name} files`, Kind: 'storage', Provider: 'local-directory', Binding: { RootPath: directory } });
    await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
    await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Draft remains while managing permissions');
    await assistantMenu(page, 'Assistant information');
    await page.getByRole('dialog', { name: 'Assistant information', exact: true }).getByRole('tab', { name: 'Permissions', exact: true }).click();
    const drawer = page.getByRole('dialog', { name: 'Assistant information', exact: true });
    await expect(drawer).toHaveClass(/ui-drawer/);
    await expect(drawer).toHaveAttribute('data-side', info.project.name === 'mobile' ? 'bottom' : 'right');
    await expect(drawer.locator('.ui-dialog__header')).toContainText(name);
    await expect(page.locator('.chat-panel')).toHaveCount(1);
    await expect(page.getByText('View models · Call models', { exact: true })).toBeVisible();
    await drawer.getByRole('button', { name: 'Local workspace information', exact: true }).click();
    await expect(page.getByRole('tooltip')).toContainText('Each assistant has one private /work on this Runtime.');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Add permission', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Add permission', exact: true });
    await expect(dialog).toContainText(`Grant access to: ${name}`);
    await expect(dialog.getByRole('button', { name: 'Forever', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await dialog.getByRole('button', { name: '7 days', exact: true }).click();
    await dialog.getByRole('button', { name: 'Custom', exact: true }).click();
    const calendar = page.getByRole('dialog', { name: 'Date and time', exact: true });
    await expect(calendar.getByRole('grid', { name: 'Calendar', exact: true })).toBeVisible();
    if (info.project.name === 'mobile') await expect(calendar).toHaveAttribute('data-side', 'bottom');
    await calendar.getByRole('button', { name: 'Today', exact: true }).click();
    await calendar.getByRole('listbox', { name: 'Hour', exact: true }).focus(); await page.keyboard.press('Home');
    await calendar.getByRole('listbox', { name: 'Minute', exact: true }).focus(); await page.keyboard.press('Home');
    await expect(calendar.getByRole('button', { name: 'Apply', exact: true })).toBeDisabled();
    await expect(calendar.getByRole('alert')).toContainText('Choose a time within the allowed range');
    await calendar.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(dialog.getByRole('button', { name: '7 days', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await choose(page, 'Resource type', 'Sandboxes');
    await choose(page, 'Permission type', 'Storage');
    await choose(page, 'Storage resource', storage.Name);
    const actions = dialog.getByRole('combobox', { name: 'Allowed actions', exact: true });
    await actions.click();
    await expect(page.getByRole('option', { name: 'Read files', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(page.getByRole('option')).toHaveCount(2);
    await page.getByRole('option', { name: 'Write files', exact: true }).click();
    await page.keyboard.press('Escape');
    await dialog.getByRole('textbox', { name: 'Allowed path prefixes (one per line)', exact: true }).fill('/reports');
    await choose(page, 'Resource type', 'Models');
    await expect(dialog.getByRole('combobox', { name: 'Resources', exact: true })).toHaveCount(0);
    await choose(page, 'Model', name);
    await expect(dialog.getByRole('textbox', { name: 'Allowed path prefixes (one per line)', exact: true })).toHaveCount(0);
    await actions.click();
    await expect(page.getByRole('option')).toHaveCount(2);
    await expect(page.getByRole('option', { name: 'View models', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(page.getByRole('option', { name: 'Call models', exact: true })).toHaveAttribute('aria-selected', 'false');
    await page.getByRole('option', { name: 'Call models', exact: true }).click();
    await page.keyboard.press('Escape');
    await expect(dialog.getByRole('textbox', { name: 'Allowed origins (one per line)', exact: true })).toHaveCount(0);
    // Returning to storage clears the old path and resets to supported read access.
    await choose(page, 'Resource type', 'Sandboxes');
    await choose(page, 'Permission type', 'Storage');
    await choose(page, 'Storage resource', storage.Name);
    await expect(dialog.getByRole('textbox', { name: 'Allowed path prefixes (one per line)', exact: true })).toHaveValue('');
    await actions.click();
    await expect(page.getByRole('option', { name: 'Write files', exact: true })).toHaveAttribute('aria-selected', 'false');
    await page.getByRole('option', { name: 'Write files', exact: true }).click();
    await page.keyboard.press('Escape');
    await dialog.getByRole('textbox', { name: 'Allowed path prefixes (one per line)', exact: true }).fill('/reports');
    await dialog.screenshot({ path: `/tmp/molr-grant-editor-${info.project.name}.png` });
    const saved = page.waitForResponse(response => response.url().endsWith('/api/grants') && response.request().method() === 'POST');
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    const response = await saved;
    expect(response.status()).toBe(200);
    const grant = await response.json();
    expect(grant.AssistantID).toBe(assistant.ID);
    expect(Date.parse(grant.ExpiresAt) - Date.now()).toBeGreaterThan(6.9 * 86400_000);
    expect(Date.parse(grant.ExpiresAt) - Date.now()).toBeLessThanOrEqual(7 * 86400_000);
    expect(grant.ResourceID).toBe(storage.ID);
    expect(grant.Actions).toEqual(['read', 'write']);
    expect(grant.Scope).not.toHaveProperty('Origins');
    expect(grant.Scope.PathPrefixes).toEqual(['/reports']);
    await expect(dialog).not.toBeVisible();
    await expect(page.getByText('Read files · Write files', { exact: true })).toBeVisible();
    const storageGrant = drawer.locator('.card').filter({ has: page.getByRole('button', { name: `Local sandbox · ${storage.Name}`, exact: true }) });
    await expect(storageGrant.getByText('/reports', { exact: true })).toHaveCount(0);
    await storageGrant.getByRole('button', { name: `Local sandbox · ${storage.Name}`, exact: true }).click();
    const permissionDetails = page.getByRole('dialog', { name: 'Permission details', exact: true });
    await expect(permissionDetails).toContainText('/reports');
    await permissionDetails.locator(':scope > .ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click();
    await storageGrant.getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Revoke permission', exact: true }).click();
    const revokeDialog = page.getByRole('dialog', { name: 'Revoke permission', exact: true });
    await revokeDialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await storageGrant.getByRole('button', { name: 'Edit', exact: true }).click();
    const editPermission = page.getByRole('dialog', { name: 'Edit permission', exact: true });
    await expect(editPermission.getByRole('combobox', { name: 'Resource type', exact: true })).toBeDisabled();
    await expect(editPermission.getByRole('textbox', { name: 'Allowed path prefixes (one per line)', exact: true })).toHaveValue('/reports');
    await editPermission.getByRole('textbox', { name: 'Allowed path prefixes (one per line)', exact: true }).fill('/archive');
    await editPermission.getByRole('button', { name: 'Forever', exact: true }).click();
    const updatePermission = page.waitForResponse(response => response.url().endsWith('/api/grants/update') && response.request().method() === 'POST');
    await editPermission.getByRole('button', { name: 'Save', exact: true }).click();
    const updatedGrantResponse = await updatePermission;
    expect(updatedGrantResponse.status()).toBe(200);
    const updatedGrant = await updatedGrantResponse.json();
    expect(updatedGrant.ID).toBe(grant.ID); expect(updatedGrant.Version).toBe(grant.Version + 1);
    expect(updatedGrant.Scope.PathPrefixes).toEqual(['/archive']); expect(updatedGrant.ExpiresAt).toBeNull();
    await expect(editPermission).not.toBeVisible();
    await drawer.getByRole('tab', { name: 'Tools', exact: true }).click();
    const capabilities = await (await page.request.get(`/api/assistants/${assistant.ID}/capabilities`)).json();
    const readFile = capabilities.Tools.find((tool: { Name: string }) => tool.Name === 'file_read');
    expect(readFile.ResourceIDs).toContain(storage.ID);
    expect(['workspace_and_resources', 'scoped_resources']).toContain(readFile.Reason);
    // Delegated Runtime instances also expose the assistant's own workspace.
    await expect(drawer.getByRole('article').filter({ has: page.getByRole('heading', { name: 'Read file', exact: true }) })).toContainText(readFile.Reason === 'workspace_and_resources' ? 'Your workspace and authorized external storage are available within their scopes.' : 'Limited to authorized storage resources and paths.');
    await drawer.locator(':scope > .ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click();
    await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toHaveValue('Draft remains while managing permissions');
    await assistantMenu(page, 'Clone assistant');
    await page.getByRole('combobox', { name: 'Permissions', exact: true }).click();
    await expect(page.getByRole('option', { name: `${name} · ${name} · View models · Call models`, exact: true })).toBeVisible();
    await expect(page.getByRole('option', { name: `${storage.Name} · Read files · Write files`, exact: true })).toBeVisible();
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('model selector groups providers and automatically searches larger catalogs', async ({ page }, info) => {
  await login(page);
  const suffix = `${info.project.name}-${Date.now()}`;
  const providers: { ID: string; Name: string }[] = [];
  for (const providerName of [`Alpha ${suffix}`, `Beta ${suffix}`]) {
    providers.push(await post(page, '/api/resources', { Name: providerName, Kind: 'model', Provider: 'openai-compatible', Binding: {
      Endpoint: `${modelOrigin}/v1/chat/completions`, AllowedOrigins: [modelOrigin],
      Models: ['Disabled chat', 'Shared chat', 'Small chat', 'Zeta chat'].map(Name => ({ Name, ContextLimit: 8192, SupportsTools: Name !== 'Disabled chat', Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } })),
    } }));
  }
  const catalog = (await (await page.request.get('/api/models')).json()).filter((model: { ResourceID: string }) => providers.some(provider => provider.ID === model.ResourceID));
  let visibleCount = 7;
  // Limit this UI fixture to the real models created above, independently of other test data.
  await page.route(url => url.pathname === '/api/models', route => route.fulfill({ json: catalog.slice(0, visibleCount) }));
  await page.reload();
  await page.getByRole('button', { name: 'Hire new assistant', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Hire new assistant', exact: true });
  const trigger = dialog.getByRole('combobox', { name: 'Model', exact: true });
  await trigger.click();
  await expect(page.getByRole('listbox', { name: 'Model', exact: true }).getByRole('option')).toHaveCount(7);
  await expect(page.getByRole('combobox', { name: 'Search Model', exact: true })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  visibleCount = 8;
  await page.reload();
  await page.getByRole('button', { name: 'Hire new assistant', exact: true }).first().click();
  await trigger.click();
  const search = page.getByRole('combobox', { name: 'Search Model', exact: true });
  const list = page.getByRole('listbox', { name: 'Model', exact: true });
  await expect(search).toBeFocused();
  await expect(list.getByRole('group')).toHaveCount(2);
  await expect(list.getByRole('group', { name: providers[0].Name, exact: true }).getByRole('option')).toHaveCount(4);
  await expect(list.getByRole('group', { name: providers[1].Name, exact: true }).getByRole('option')).toHaveCount(4);
  await search.fill('Disabled chat');
  await expect(list.getByRole('option')).toHaveCount(2);
  await search.press('Enter');
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
  await search.fill(`Beta ${suffix}`);
  await expect(list.getByRole('group')).toHaveCount(1);
  await expect(list.getByRole('option')).toHaveCount(4);
  await search.fill(`Beta ${suffix} Shared chat`);
  await expect(list.getByRole('option')).toHaveCount(1);
  await search.press('Enter');
  await expect(trigger).toContainText('Shared chat');
  await trigger.click();
  await expect(search).toHaveValue('');
  await expect(list.getByRole('group', { name: providers[1].Name, exact: true }).getByRole('option', { name: 'Shared chat', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(list.getByRole('group', { name: providers[0].Name, exact: true }).getByRole('option', { name: 'Shared chat', exact: true })).toHaveAttribute('aria-selected', 'false');
  await search.fill('no-such-model');
  await expect(list.getByRole('group')).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: 'No matching options' })).toBeVisible();
  await search.press('Escape');
  await dialog.getByRole('textbox', { name: 'Assistant name', exact: true }).fill(`Grouped assistant ${suffix}`);
  await dialog.getByRole('spinbutton', { name: 'Spending limit (USD)', exact: true }).fill('0');
  await dialog.getByRole('button', { name: 'Hire new assistant', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const assistants = (await (await page.request.get('/api/assistants')).json()).Items;
  expect(assistants.find((assistant: { Name: string }) => assistant.Name === `Grouped assistant ${suffix}`).ModelID).toBe(catalog.find((model: { ResourceID: string; Name: string }) => model.ResourceID === providers[1].ID && model.Name === 'Shared chat').ID);
  await assistantMenu(page, 'Assistant information');
    await page.getByRole('dialog', { name: 'Assistant information', exact: true }).getByRole('tab', { name: 'Permissions', exact: true }).click();
  await page.getByRole('button', { name: 'Add permission', exact: true }).click();
  const grantDialog = page.getByRole('dialog', { name: 'Add permission', exact: true });
  await choose(page, 'Resource type', 'Models');
  await expect(grantDialog.getByRole('combobox', { name: 'Resources', exact: true })).toHaveCount(0);
  await grantDialog.getByRole('combobox', { name: 'Model', exact: true }).click();
  await expect(list.getByRole('group')).toHaveCount(2);
  await search.fill(`Alpha ${suffix} Disabled chat`);
  const permissionModel = list.getByRole('option', { name: 'Disabled chat', exact: true });
  await expect(permissionModel).not.toHaveAttribute('aria-disabled', 'true');
  await permissionModel.click();
  const grantSaved = page.waitForResponse(response => response.url().endsWith('/api/grants') && response.request().method() === 'POST');
  await grantDialog.getByRole('button', { name: 'Save', exact: true }).click();
  const grantResponse = await grantSaved;
  expect(grantResponse.status()).toBe(200);
  const grant = await grantResponse.json();
  expect(grant.ResourceID).toBe(providers[0].ID);
  expect(grant.Scope.ModelID).toBe(catalog.find((model: { ResourceID: string; Name: string }) => model.ResourceID === providers[0].ID && model.Name === 'Disabled chat').ID);
  expect(grant.Actions).toEqual(['read']);
  await expect(grantDialog).not.toBeVisible();
  await expect(page.getByRole('dialog', { name: 'Assistant information', exact: true }).getByRole('button', { name: 'Disabled chat', exact: true })).toBeVisible();
});

test('credential fields keep login autofill separate from provider secrets', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('textbox', { name: 'Username', exact: true })).toHaveAttribute('autocomplete', 'username');
  await expect(page.getByLabel('Password', { exact: true })).toHaveAttribute('autocomplete', 'current-password');
  await expect(page.getByLabel('Password', { exact: true })).toHaveAttribute('type', 'password');
  await login(page);
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
  const key = dialog.getByLabel('API key', { exact: true });
  await expect(key).toHaveAttribute('type', 'text');
  await expect(key).toHaveCSS('-webkit-text-security', 'disc');
  await expect(key).toHaveAttribute('autocomplete', 'off');
  await expect(key).toHaveAttribute('spellcheck', 'false');
  await expect(key).toHaveAttribute('data-1p-ignore', 'true');
  const openAIInput = await key.elementHandle();
  const openAIName = await key.getAttribute('name');
  await key.fill('openai-only-secret');
  await dialog.getByRole('button', { name: 'Show secret', exact: true }).click();
  await expect(key).toHaveCSS('-webkit-text-security', 'none');
  await expect(key).toHaveValue('openai-only-secret');
  await expect(key).toHaveAttribute('autocomplete', 'off');
  const hide = dialog.getByRole('button', { name: 'Hide secret', exact: true });
  await expect(hide).toHaveAttribute('aria-controls', (await key.getAttribute('id'))!);
  await hide.focus(); await page.keyboard.press('Enter');
  await expect(key).toHaveCSS('-webkit-text-security', 'disc');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Show secret', exact: true }).click();
  await dialog.getByRole('button', { name: 'DeepSeek · 深度求索', exact: true }).click();
  expect(await openAIInput!.evaluate(node => node.isConnected)).toBe(false);
  expect(await key.getAttribute('name')).not.toBe(openAIName);
  await expect(key).toHaveCSS('-webkit-text-security', 'disc');
  await expect(dialog.getByRole('button', { name: 'Show secret', exact: true })).toBeVisible();
  await expect(key).toHaveValue('');
  await key.fill('deepseek-only-secret');
  await dialog.getByRole('button', { name: 'OpenAI', exact: true }).click();
  await expect(key).toHaveValue('openai-only-secret');
  await expect(key).toHaveCSS('-webkit-text-security', 'disc');
  await dialog.getByRole('button', { name: 'DeepSeek · 深度求索', exact: true }).click();
  await expect(key).toHaveValue('deepseek-only-secret');
  await dialog.getByText('More configuration', { exact: true }).click();
  await expect(dialog.getByRole('textbox', { name: 'Name', exact: true })).toHaveAttribute('autocomplete', 'off');
  await expect(dialog.getByRole('textbox', { name: 'Service URL', exact: true })).toHaveAttribute('autocomplete', 'off');
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain('-only-secret');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
  await expect(key).toHaveValue('');
  await expect(key).toHaveCSS('-webkit-text-security', 'disc');
});

test('secret fields stay concealed when CSS text masking is unavailable', async ({ page }) => {
  await page.addInitScript(() => {
    const supports = CSS.supports.bind(CSS);
    CSS.supports = ((property: string, value?: string) => property === '-webkit-text-security' ? false : value === undefined ? supports(property) : supports(property, value)) as typeof CSS.supports;
  });
  await login(page);
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
  const key = dialog.getByLabel('API key', { exact: true });
  await expect(key).toHaveAttribute('type', 'password');
  await expect(key).toHaveAttribute('autocomplete', 'section-provider-openai-new-api-key new-password');
  await key.fill('fallback-openai-secret');
  await dialog.getByRole('button', { name: 'Show secret', exact: true }).click();
  await expect(key).toHaveAttribute('type', 'text');
  await expect(key).toHaveValue('fallback-openai-secret');
  await dialog.getByRole('button', { name: 'Hide secret', exact: true }).click();
  await expect(key).toHaveAttribute('type', 'password');
  await expect(key).toHaveValue('fallback-openai-secret');
  await dialog.getByRole('button', { name: 'DeepSeek · 深度求索', exact: true }).click();
  await expect(key).toHaveValue('');
  await expect(key).toHaveAttribute('autocomplete', 'section-provider-deepseek-new-api-key new-password');
});

test('provider search and cloud/local filters preserve drafts and preset endpoints', async ({ page }) => {
  await login(page);
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
  const choices = dialog.locator('.provider-connect-provider-options');
  const search = dialog.getByRole('searchbox', { name: 'Search providers' });
  await expect(search).toHaveAttribute('autocomplete', 'off');
  await expect.poll(() => dialog.locator('.provider-connect-filters').evaluate(element => {
    const filters = element.querySelector('fieldset')!.getBoundingClientRect();
    const search = element.querySelector('input[type=search]')!.getBoundingClientRect();
    return filters.bottom < search.top;
  })).toBe(true);
  const form = dialog.getByRole('form', { name: 'Provider configuration' });
  await form.getByLabel('API key', { exact: true }).fill('retained-openai-draft');
  for (const [query, name, endpoint] of [
    ['  阿里云  ', 'Aliyun Bailian · 阿里云百炼', 'https://dashscope.aliyuncs.com/compatible-mode/v1'],
    ['智谱', 'Zhipu · 智谱', 'https://open.bigmodel.cn/api/paas/v4'],
    ['kImI', 'Moonshot AI · 月之暗面', 'https://api.moonshot.cn/v1'],
  ]) {
    await search.fill(query);
    await expect(choices.getByRole('button')).toHaveText([name]);
    await choices.getByRole('button', { name, exact: true }).click();
    await expect(form.getByLabel('API key', { exact: true })).toHaveValue('');
    await form.getByText('More configuration', { exact: true }).click();
    await expect(form.getByRole('textbox', { name: 'Service URL', exact: true })).toHaveValue(endpoint);
    await form.getByLabel('API key', { exact: true }).fill(`draft-${name}`);
  }
  await dialog.getByRole('radio', { name: 'Local', exact: true }).check();
  await expect(choices.getByRole('button')).toHaveCount(0);
  await expect(dialog.locator('.provider-connect-providers').getByRole('status')).toContainText('No matching providers');
  await search.fill('');
  await expect(choices.getByRole('button')).toHaveText(['llama.cpp', 'LM Studio', 'Ollama', 'SGLang', 'vLLM', 'Custom service']);
  await choices.getByRole('button', { name: 'Ollama', exact: true }).click();
  await expect(form.getByRole('combobox', { name: 'Authentication' })).toContainText('No authentication');
  await dialog.getByRole('radio', { name: 'Cloud', exact: true }).check();
  await expect(choices.getByRole('button')).toHaveText(['Aliyun Bailian · 阿里云百炼', 'Anthropic', 'DeepSeek · 深度求索', 'Google Gemini', 'MiniMax', 'Moonshot AI · 月之暗面', 'OpenAI', 'OpenRouter', 'SiliconFlow · 硅基流动', 'Volcengine · 火山方舟', 'Z.AI', 'Zhipu · 智谱', 'Custom service']);
  await choices.getByRole('button', { name: 'Moonshot AI · 月之暗面', exact: true }).click();
  await expect(form.getByLabel('API key', { exact: true })).toHaveValue('draft-Moonshot AI · 月之暗面');
  await choices.getByRole('button', { name: 'OpenAI', exact: true }).click();
  await expect(form.getByLabel('API key', { exact: true })).toHaveValue('retained-openai-draft');
  await dialog.getByRole('radio', { name: 'All', exact: true }).check();
  await expect(choices.getByRole('button')).toHaveCount(18);
  await search.fill('nothing-matches');
  await expect(dialog.locator('.provider-connect-providers').getByRole('status')).toContainText('No matching providers');
  await dialog.getByRole('button', { name: 'Clear search', exact: true }).click();
  await expect(choices.getByRole('button')).toHaveCount(18);
});

test('provider connection discovers models without manual configuration and keeps credentials scoped', async ({ page }, info) => {
  await login(page);
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
  const providers = dialog.getByRole('group', { name: 'Model providers', exact: true });
  const form = dialog.getByRole('form', { name: 'Provider configuration', exact: true });
  await expect(providers.getByRole('button')).toHaveText(['Aliyun Bailian · 阿里云百炼', 'Anthropic', 'DeepSeek · 深度求索', 'Google Gemini', 'llama.cpp', 'LM Studio', 'MiniMax', 'Moonshot AI · 月之暗面', 'Ollama', 'OpenAI', 'OpenRouter', 'SGLang', 'SiliconFlow · 硅基流动', 'vLLM', 'Volcengine · 火山方舟', 'Z.AI', 'Zhipu · 智谱', 'Custom service']);
  await expect(form.getByRole('textbox', { name: 'Service URL', exact: true })).not.toBeVisible();
  await expect(form.getByRole('textbox', { name: 'Model identifier', exact: true })).toHaveCount(0);
  await expect(form.getByRole('spinbutton')).toHaveCount(0);
  await expect(form.getByLabel('API Key', { exact: false })).toBeVisible();
  await expect(form.getByRole('combobox', { name: 'Authentication', exact: true })).toHaveCount(0);
  await form.getByLabel('API Key', { exact: false }).fill('draft-openai-key');
  await providers.getByRole('button', { name: 'DeepSeek · 深度求索', exact: true }).click();
  await expect(form.getByRole('combobox', { name: 'Authentication', exact: true })).toHaveCount(0);
  await expect(form.getByLabel('API Key', { exact: false })).toHaveValue('');
  await providers.getByRole('button', { name: 'OpenAI', exact: true }).click();
  await expect(form.getByLabel('API Key', { exact: false })).toHaveValue('draft-openai-key');
  await providers.getByRole('button', { name: 'Custom service', exact: true }).click();
  await expect(form.getByRole('combobox', { name: 'Authentication', exact: true })).toContainText('No authentication');
  await form.getByRole('textbox', { name: 'Service URL', exact: true }).fill(`${modelOrigin}/v1`);
  await form.getByText('More configuration', { exact: true }).click();
  const name = `Provider ${info.project.name}-${Date.now()}`;
  await form.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
  await expect(form.getByRole('switch')).toHaveCount(0);
  const [providersBox, formBox] = await readLayout(providers, form);
  if (info.project.name === 'desktop') {
    expect(providersBox.x + providersBox.width).toBeLessThan(formBox.x);
    const viewport = page.viewportSize()!;
    await page.setViewportSize({ width: viewport.width, height: 600 });
    await expect.poll(async () => dialog.evaluate(element => {
      const box = element.getBoundingClientRect();
      return box.height <= innerHeight * 0.9 + 1 && Math.abs(box.top - (innerHeight - box.bottom)) < 1;
    })).toBe(true);
    await page.setViewportSize(viewport);
  } else {
    expect(providersBox.y + providersBox.height).toBeLessThan(formBox.y);
    await expect(dialog).toHaveAttribute('data-contained', 'true');
  }
  await dialog.screenshot({ path: `/tmp/molr-provider-connect-${info.project.name}.png` });
  await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(dialog.getByText('Connected · 2 models detected', { exact: true })).toBeVisible();
  const created = page.waitForResponse(response => response.url().endsWith('/api/resources/provider') && response.request().method() === 'POST');
  await dialog.getByRole('button', { name: 'Connect provider', exact: true }).click();
  const resource = await (await created).json();
  await expect(dialog).not.toBeVisible();
  const card = resourceRow(page, name);
  await openProviderModels(card);
  await expect(page.locator('.provider-models-page').first()).toContainText('discovered-one'); await expect(page.locator('.provider-models-page').first()).toContainText('discovered-two');
  await expect(page.locator('.provider-models-page').first().getByText('Pricing not configured', { exact: true })).toHaveCount(2);
  const catalog = await (await page.request.get('/api/models')).json();
  const models = catalog.filter((model: { ResourceID: string }) => model.ResourceID === resource.ID);
  expect(models).toHaveLength(2); expect(models.every((model: { ID: string }) => model.ID !== resource.ID)).toBe(true);
  await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
  await page.locator('.provider-models-page').first().locator('li').filter({ hasText: 'discovered-one' }).locator('.resource-model-open').click();
  const configuration = page.getByRole('dialog', { name: 'Configure model', exact: true });
  const prices = configuration.getByRole('spinbutton', { name: /^(Input|Output|Cached input) price/ });
  await expect(prices).toHaveCount(3);
  for (const field of await prices.all()) await field.fill('0');
  await configuration.getByRole('spinbutton', { name: 'Manual context limit (tokens, optional)', exact: true }).fill('8192');
  await configuration.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(configuration).not.toBeVisible();
  await expect(page.locator('.provider-models-page').first().getByText('Pricing not configured', { exact: true })).toHaveCount(1);
  await cardAction(page, card, 'Refresh models');
  await openProviderModels(card);
  await expect(page.locator('.provider-models-page').first()).toContainText('discovered-two');
  await expect(page.locator('.provider-models-page').first().getByText('Pricing not configured', { exact: true })).toHaveCount(1);
});

test('provider API key is stored in the vault and used for model discovery', async ({ page }, info) => {
  await login(page);
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
  await dialog.getByRole('button', { name: 'Custom service', exact: true }).click();
  await choose(page, 'Authentication', 'API key');
  await dialog.getByLabel('API Key', { exact: false }).fill('test-provider-secret');
  await expect(dialog.getByLabel('Runtime sign-in password', { exact: true })).toHaveCount(0);
  await expect(dialog.getByLabel('Authenticator or recovery code', { exact: true })).toHaveCount(0);
  await dialog.getByRole('textbox', { name: 'Service URL', exact: true }).fill(`${modelOrigin}/private/v1`);
  await dialog.getByText('More configuration', { exact: true }).click();
  const name = `Private provider ${info.project.name}-${Date.now()}`;
  await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
  await expect(dialog.locator('.ui-alert--warning')).toContainText('Data privacy');
  const beforeResources = (await (await page.request.get('/api/resources')).json()).Items.map((row: { ID: string }) => row.ID);
  const beforeCredentials = (await (await page.request.get('/api/credentials')).json()).Items.map((row: { ID: string }) => row.ID);
  await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(dialog.getByText('Connected · 2 models detected', { exact: true })).toBeVisible();
  await expect(page.getByRole('dialog', { name: 'Verify identity', exact: true })).toHaveCount(0);
  expect((await (await page.request.get('/api/resources')).json()).Items.map((row: { ID: string }) => row.ID)).toEqual(beforeResources);
  expect((await (await page.request.get('/api/credentials')).json()).Items.map((row: { ID: string }) => row.ID)).toEqual(beforeCredentials);
  await dialog.getByLabel('API Key', { exact: false }).fill('wrong-key');
  await expect(dialog.getByText('Connected · 2 models detected', { exact: true })).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(dialog.getByRole('alert')).toBeVisible();
  await dialog.getByLabel('API Key', { exact: false }).fill('test-provider-secret');
  await expect(dialog.getByRole('alert')).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Connect provider', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Verify identity', exact: true })).toHaveCount(0);
  await expect(dialog).not.toBeVisible();
  const card = resourceRow(page, name);
  await openProviderModels(card);
  await expect(page.locator('.provider-models-page').first()).toContainText('discovered-one');
  const credentials = await (await page.request.get('/api/credentials')).text();
  expect(credentials).toContain(name);
  expect(credentials).not.toContain('test-provider-secret');
  expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain('test-provider-secret');
  await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
  await cardAction(page, card, 'Edit provider');
  const editor = page.getByRole('dialog', { name: 'Edit provider', exact: true });
  await expect(editor.getByText('API key configured', { exact: true })).toBeVisible();
  await expect(editor.getByLabel('API key', { exact: true })).toHaveCount(0);
  await expect(editor.getByRole('combobox', { name: 'Credential', exact: true })).toHaveCount(0);
  await editor.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(editor.getByText('Connected · 2 models detected', { exact: true })).toBeVisible();
  await editor.getByRole('button', { name: 'Replace API key', exact: true }).click();
  await editor.getByLabel('API key', { exact: true }).fill('rotated-provider-secret');
  await editor.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Verify identity', exact: true })).toHaveCount(0);
  await expect(editor).not.toBeVisible();
  const afterCredentials = (await (await page.request.get('/api/credentials')).json()).Items;
  expect(afterCredentials.filter((row: { Label: string }) => row.Label === name)).toHaveLength(1);
  const updatedKey = afterCredentials.find((row: { Label: string }) => row.Label === name);
  expect(updatedKey.Version).toBe(2);
  await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
  await cardAction(page, card, 'Edit provider');
  await editor.getByRole('textbox', { name: 'Name', exact: true }).fill(name + ' renamed');
  await editor.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(editor.getByText('Connected · 2 models detected', { exact: true })).toBeVisible();
  await editor.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(editor).not.toBeVisible();
  await expect(page.getByRole('heading', { name: name + ' renamed', exact: true })).toBeVisible();
  await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
  const renamed = resourceRow(page, name + ' renamed');
  await cardAction(page, renamed, 'Delete provider');
  const deletion = page.getByRole('dialog', { name: 'Delete provider', exact: true });
  await expect(deletion).toContainText('Assistants and chat history are kept.');
  await expect(deletion.locator('.ui-dialog__body')).toHaveCount(0);
  await deletion.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(renamed).toBeVisible();
  await cardAction(page, renamed, 'Delete provider');
  await deletion.getByRole('button', { name: 'Confirm', exact: true }).click();
  await expect(deletion).not.toBeVisible();
  await expect(page.getByRole('dialog', { name: 'Verify identity', exact: true })).toHaveCount(0);
  await expect(renamed).toHaveCount(0);
  expect((await (await page.request.get('/api/credentials')).json()).Items.some((row: { ID: string }) => row.ID === updatedKey.ID)).toBe(false);
  await openUserSettings(page);
  await expect(settingsNavItem(page, 'Model credentials')).toHaveCount(0);
});

test('security page manages optional verification and authenticator enrollment', async ({ page }, info) => {
  await login(page);
  let codes: string[] = [];
  const proof = async (purpose: string, mfa_code = '') => (await post(page, '/api/auth/reauthenticate', { password: 'webui-e2e-password-2026!', mfa_code, purpose }, false)).verification;
  const closeToasts = async () => { while (await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).count()) await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).first().click(); };
  try {
    await openUserSettings(page);
    await openSettingsSection(page, 'Security');
    await expect(settingsNavItem(page, 'Additional verification')).toHaveCount(0);
    const toggle = page.getByRole('switch', { name: 'Verify sensitive actions', exact: true });
    await expect(toggle).not.toBeChecked();
    await toggle.click();
    await verifyIdentity(page);
    await expect(toggle).toBeChecked();
    await page.reload();
    await openSettingsSection(page, 'Security');
    await expect(toggle).toBeChecked();
    await page.getByRole('button', { name: 'Bind authenticator', exact: true }).click();
    await verifyIdentity(page);
    const enrollment = page.getByRole('dialog', { name: 'Bind authenticator', exact: true });
    const secret = await enrollment.getByLabel('Authenticator setup key', { exact: true }).inputValue();
    const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
    const bits = [...secret.replace(/=+$/, '')].map(char => alphabet.indexOf(char).toString(2).padStart(5, '0')).join('');
    const key = Buffer.from(bits.match(/.{8}/g)!.map(byte => parseInt(byte, 2)));
    const counter = Buffer.alloc(8); counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000)));
    const digest = createHmac('sha1', key).update(counter).digest(), offset = digest[digest.length - 1] & 15;
    const code = String((digest.readUInt32BE(offset) & 0x7fffffff) % 1000000).padStart(6, '0');
    await enrollment.getByLabel('Verification code', { exact: true }).fill(code);
    const confirmation = page.waitForResponse(response => response.url().endsWith('/api/auth/mfa/confirm'));
    await enrollment.getByRole('button', { name: 'Enable authenticator', exact: true }).click();
    codes = (await (await confirmation).json()).recovery_codes;
    expect(codes).toHaveLength(10);
    const recovery = page.getByRole('dialog', { name: 'Recovery codes', exact: true });
    await expect(recovery.getByRole('textbox', { name: 'Recovery codes', exact: true })).toHaveValue(codes.join('\n'));
    await closeToasts();
    const downloadEvent = page.waitForEvent('download');
    await recovery.getByRole('button', { name: 'Download recovery codes', exact: true }).click();
    expect((await downloadEvent).suggestedFilename()).toBe('molr-recovery-codes.txt');
    await recovery.getByRole('button', { name: 'I saved my codes', exact: true }).click();
    await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
    await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Connect provider', exact: true });
    await expect(editor.getByLabel('Authenticator or recovery code', { exact: true })).toHaveCount(0);
    await editor.getByLabel('API key', { exact: true }).fill('test-mfa-secret');
    await editor.getByText('More configuration', { exact: true }).click();
    await editor.getByRole('textbox', { name: 'Name', exact: true }).fill(`MFA provider ${info.project.name}`);
    await editor.getByRole('textbox', { name: 'Service URL', exact: true }).fill(`${modelOrigin}/v1`);
    await editor.getByRole('button', { name: 'Connect provider', exact: true }).click();
    const verification = page.getByRole('dialog', { name: 'Verify identity', exact: true });
    await expect(verification.getByLabel('Authenticator or recovery code', { exact: true })).toBeVisible();
    await verification.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(editor.getByLabel('API key', { exact: true })).toHaveValue('test-mfa-secret');
    await editor.getByRole('button', { name: 'Connect provider', exact: true }).click();
    await verifyIdentity(page, codes[0]);
    await expect(editor).not.toBeVisible();
    await closeToasts();
    const providerCard = resourceRow(page, `MFA provider ${info.project.name}`);
    await cardAction(page, providerCard, 'Delete provider');
    const deletion = page.getByRole('dialog', { name: 'Delete provider', exact: true });
    await deletion.getByRole('button', { name: 'Confirm', exact: true }).click();
    await verifyIdentity(page, codes[2]);
    await expect(deletion).not.toBeVisible();
    await expect(providerCard).toHaveCount(0);
    await closeToasts();
    await openUserSettings(page);
    await openSettingsSection(page, 'Security');
    await page.getByRole('button', { name: 'Unbind authenticator', exact: true }).click();
    await verifyIdentity(page, codes[1]);
    await expect(page.getByRole('button', { name: 'Bind authenticator', exact: true })).toBeVisible();
    await toggle.click();
    await verifyIdentity(page);
    await expect(toggle).not.toBeChecked();
  } finally {
    let status = await (await page.request.get('/api/auth/security')).json();
    if (status.MFAEnabled && codes.length) await post(page, '/api/auth/mfa/disable', { verification: await proof('mfa.disable', codes[9]) }, false);
    status = await (await page.request.get('/api/auth/security')).json();
    if (status.ReauthenticationEnabled) {
      const verification = await proof('security.update');
      status = await (await page.request.get('/api/auth/security')).json();
      await post(page, '/api/auth/security', { enabled: false, expected_version: status.Version, verification }, false);
    }
  }
});

test('disconnected providers can be deleted while assistants are kept', async ({ page }, info) => {
  await login(page);
  const name = `Delete disconnected ${info.project.name}-${Date.now()}`;
  const assistant = await seedAssistant(page, name);
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  const card = resourceRow(page, name);
  await openProviderModels(card);
  await cardAction(page, card, 'Disconnect resource');
  await page.getByRole('dialog', { name: 'Disconnect resource', exact: true }).getByRole('button', { name: 'Confirm', exact: true }).click();
  await card.getByRole('button', { name: 'More', exact: true }).click();
  await expect(page.getByRole('menuitem', { name: 'Check connection', exact: true })).toBeDisabled();
  await page.keyboard.press('Escape');
  await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
  await cardAction(page, card, 'Delete provider');
  await page.getByRole('dialog', { name: 'Delete provider', exact: true }).getByRole('button', { name: 'Confirm', exact: true }).click();
  await expect(card).toHaveCount(0);
  await page.reload();
  await expect(card).toHaveCount(0);
  expect((await (await page.request.get(`/api/assistants/${assistant.ID}`)).json()).ID).toBe(assistant.ID);
});

test('resource APIs cannot change deployment network policy', async ({ page }) => {
  await login(page);
  const token = (await page.context().cookies()).find(cookie => cookie.name === csrfCookie)!.value;
  for (const extra of [{ AllowPrivateNetwork: true }, { AllowedIPs: ['127.0.0.2'] }]) {
    const response = await page.request.post('/api/resources', { headers: { Origin: base, 'X-CSRF-Token': token }, data: {
      Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 }, Name: 'Forbidden policy override', Kind: 'model', Provider: 'openai-compatible',
      Binding: { Endpoint: 'http://127.0.0.2:18882/v1/chat/completions', AllowedOrigins: ['http://127.0.0.2:18882'], ...extra },
    } });
    expect(response.status()).toBe(400);
    expect((await response.json()).code).toBe('invalid_argument');
  }
  const response = await page.request.post('/api/resources', { headers: { Origin: base, 'X-CSRF-Token': token }, data: {
    Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 }, Name: 'Unapproved local address', Kind: 'model', Provider: 'openai-compatible',
    Binding: { Endpoint: 'http://127.0.0.2:18882', AllowedOrigins: ['http://127.0.0.2:18882'] },
  } });
  expect(response.status()).toBe(400);
});

test('resource tabs expose tools and sandbox-scoped resources', async ({ page }) => {
  await login(page);
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await expect(page.locator('.resources-categories > .ui-tabs__list').getByRole('tab')).toHaveText(['Models', 'Tools', 'Skills', 'Sandboxes']);
  await resourceCategory(page, 'Tools').click();
  await expect(page.locator('.tools-catalog-row').filter({ has: page.getByText('Read file', { exact: true }) })).toBeVisible();
  await resourceCategory(page, 'Sandboxes').click();
  await expect(page.getByRole('button', { name: 'Connect sandbox', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Local workspace /work', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Connect sandbox', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect sandbox', exact: true });
  for (const label of ['Service URL', 'Connection code']) await expect(dialog.getByLabel(label, { exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('local command environment is prepared automatically without sandbox paths', async ({ page }) => {
  await login(page);
  const status = await (await page.request.get('/api/sandbox')).json();
  expect(status.Enabled).toBe(true); expect(status.Capabilities.Shell).toBe(true);
  expect(status.Capabilities.DiskProtection).toBe('file_limit_and_free_space_guard');
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await resourceCategory(page, 'Sandboxes').click();
  await resourceCategory(page, 'Compute').click();
  await expect(page.getByRole('heading', { name: 'Local sandbox', exact: true }).first()).toBeVisible();
  await expect(page.getByRole('button', { name: 'Connect compute', exact: true })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Requestable compute capacity', exact: true })).toBeVisible();
  await expect(page.locator('.sandbox-limits')).toContainText('MiB');
});

test('resource links select their sandbox and resource category', async ({ page }, info) => {
  const directory = await mkdtemp(join(tmpdir(), 'molr-resource-tab-'));
  try {
    await login(page);
    const name = `Sandbox files ${info.project.name} ${Date.now()}`;
    const resource = await post(page, '/api/resources', { Name: name, Kind: 'storage', Provider: 'local-directory', Binding: { RootPath: directory } });
    await page.goto(`/#/resources/${resource.ID}`); await page.reload();
    await expect(resourceCategory(page, 'Sandboxes')).toHaveAttribute('aria-selected', 'true');
    await expect(resourceCategory(page, 'Storage')).toHaveAttribute('aria-selected', 'true');
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
    const card = page.locator('.sandbox-resource').filter({ has: page.getByRole('heading', { name, exact: true }) });
    await expect(card).toContainText(directory);
    await card.getByRole('button', { name: `More · ${name}`, exact: true }).click();
    await page.getByRole('menuitem', { name: 'Disconnect resource', exact: true }).click();
    const confirmation = page.getByRole('dialog', { name: 'Disconnect resource', exact: true });
    await confirmation.getByRole('button', { name: 'Confirm', exact: true }).click();
    await expect(confirmation).not.toBeVisible();
    await expect(card).toContainText('Disconnected');
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test('assistant pinning, clone, archive, rehire and search use persisted data', async ({ page }, info) => {
  await login(page); const suffix = `${info.project.name}-${Date.now()}`, name = `Lifecycle ${suffix}`, cloneName = `Copy ${suffix}`;
  const assistant = await seedAssistant(page, name);
  const group = await post(page, '/api/assistants/groups', { Name: `Existing group ${suffix}`, Position: 0 });
  await post(page, '/api/assistants/place', { ID: assistant.ID, GroupID: group.ID, Pinned: false, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: assistant.Version } }, false);
  await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
  await expect(page.locator('.chat-header h1')).toHaveText(name);
  await page.locator('.chat-header').getByRole('button', { name: 'More', exact: true }).click();
  await expect(page.getByRole('menuitem', { name: 'Conversation group', exact: true })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await assistantMenu(page, 'Pin conversation');
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}`)).json()).Pinned).toBe(true);
  expect((await (await page.request.get(`/api/assistants/${assistant.ID}`)).json()).GroupID).toBe(group.ID);
  await assistantMenu(page, 'Clone assistant'); const clone = page.getByRole('dialog', { name: 'Clone assistant', exact: true });
  await clone.getByRole('textbox', { name: 'Assistant name', exact: true }).fill(cloneName); await clone.getByRole('button', { name: 'Confirm', exact: true }).click();
  await expect(page.locator('.chat-header h1')).toHaveText(cloneName);
  await assistantMenu(page, 'Dismiss assistant'); await page.getByRole('dialog', { name: 'Dismiss assistant', exact: true }).getByRole('button', { name: 'Confirm', exact: true }).click();
  await openUserSettings(page); await openSettingsSection(page, 'Former assistants');
  const former = settingsPane(page).locator('.settings-row-item').filter({ hasText: cloneName });
  await former.getByRole('button', { name: 'Assistant conversation', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toBeDisabled();
  await openUserSettings(page); await openSettingsSection(page, 'Former assistants');
  await former.getByRole('button', { name: 'Rehire assistant', exact: true }).click(); await page.getByRole('dialog', { name: 'Rehire assistant', exact: true }).getByRole('button', { name: 'Confirm', exact: true }).click(); await expect(former).toHaveCount(0);
  await page.getByRole('navigation').getByRole('button', { name: 'Assistants', exact: true }).click(); await page.getByRole('button', { name: 'Search', exact: true }).first().click();
  const search = page.getByRole('dialog', { name: 'Search', exact: true }); await search.getByRole('searchbox').fill(cloneName);
  await search.getByRole('button').filter({ hasText: cloneName }).first().click(); await expect(page.locator('.chat-header h1')).toHaveText(cloneName);
});

test('task controls and protected data export reach the backend', async ({ page }, info) => {
  await login(page); const suffix = `${info.project.name}-${Date.now()}`; const assistant = await seedAssistant(page, `Tasks ${suffix}`);
  await page.goto(`/#/assistants/${assistant.ID}`); await page.locator('.chat-header').getByRole('button', { name: 'Plans', exact: true }).click();
  await post(page, '/api/tasks', { AssistantID: assistant.ID, Goal: `Goal ${suffix}`, AcceptanceCriteria: ['A reviewed report'] });
  await page.reload(); await page.locator('.chat-header').getByRole('button', { name: 'Plans', exact: true }).click();
  const tasks = page.getByRole('dialog', { name: 'Plans', exact: true });
  await selectPlanRange(tasks, /^No date/);
  await expect(tasks.getByRole('heading', { name: `Goal ${suffix}`, exact: true })).toBeVisible();
  await tasks.getByRole('button', { name: /Execution history/ }).click(); await tasks.locator('.plan-execution summary').click();
  await tasks.getByRole('button', { name: 'Pause task', exact: true }).click(); await expect(tasks.getByRole('button', { name: 'Resume task', exact: true })).toBeVisible();
  while (await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).count()) await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).first().click();
  await tasks.getByRole('button', { name: 'Cancel task', exact: true }).click(); await page.getByRole('dialog', { name: 'Cancel task', exact: true }).getByRole('button', { name: 'Confirm', exact: true }).click(); await expect(tasks.locator('.plan-execution summary')).toContainText('Canceled');
  await page.keyboard.press('Escape'); await expect(tasks).not.toBeVisible();
  await openUserSettings(page);
  await expect(settingsNavItem(page, 'Model credentials')).toHaveCount(0);
  await openSettingsSection(page, 'Personal data export'); await page.getByRole('button', { name: 'Export data', exact: true }).click();
  const data = page.getByRole('dialog', { name: 'Export data', exact: true }); await data.getByRole('button', { name: 'Export data', exact: true }).click(); await expect(page.getByRole('dialog', { name: 'Verify identity', exact: true })).toHaveCount(0); await expect(data).not.toBeVisible();
  await expect(settingsPane(page).getByRole('link')).toBeVisible();
});

test('a lost send response keeps the draft and retry reuses its idempotency key', async ({ page }, info) => {
  await login(page); const assistant = await seedAssistant(page, `Retry ${info.project.name}-${Date.now()}`);
  await page.goto(`/#/assistants/${assistant.ID}`);
  const text = `Uncertain delivery ${Date.now()}`; const keys: string[] = []; let lose = true;
  await page.route('**/api/conversations/messages', async route => {
    keys.push(route.request().postDataJSON().Mutation.IdempotencyKey);
    const response = await route.fetch();
    if (lose) { lose = false; await route.abort('failed'); } else await route.fulfill({ response });
  });
  const composer = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  await composer.fill(text); await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'service is unavailable' })).toBeVisible(); await expect(composer).toHaveValue(text);
  await page.locator('.ui-toast--danger').getByRole('button', { name: 'Retry', exact: true }).click(); await expect(composer).toHaveValue('');
  expect(keys).toHaveLength(2); expect(keys[0]).toBe(keys[1]);
  const history = await (await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json();
  expect(history.Items.filter((message: { Content: { Text: string } }) => message.Content.Text === text)).toHaveLength(1);
});

test('assistant list search filters locally without opening a panel or changing the conversation', async ({ page }, info) => {
  await login(page);
  const suffix = `${info.project.name}-${Date.now()}`;
  const first = await seedAssistant(page, `研究 Alpha ${suffix}`);
  const second = await seedAssistant(page, `写作 Beta ${suffix}`);
  await page.goto(`/#/assistants/${first.ID}`);
  await page.reload();
  if (info.project.name === 'mobile') await page.getByRole('button', { name: 'Back to assistants', exact: true }).click();
  const sidebar = page.locator('.workspace-sidebar');
  const search = sidebar.getByRole('searchbox', { name: 'Search assistants', exact: true });
  const list = sidebar.locator('.assistant-list');
  await expect(sidebar.getByRole('button', { name: 'More', exact: true })).toHaveCount(0);
  await expect(list.getByRole('button', { name: second.Name, exact: true })).toBeVisible();
  await search.fill(`  BETA ${suffix.toUpperCase()}  `);
  await expect(list.getByRole('button')).toHaveCount(1);
  await expect(list.getByRole('button', { name: second.Name, exact: true })).toBeVisible();
  await expect(page.getByRole('dialog', { name: 'Search', exact: true })).toHaveCount(0);
  if (info.project.name === 'desktop') await expect(page.locator('.chat-header h1')).toHaveText(first.Name);
  await search.fill('不匹配的助理名称');
  await expect(list.getByRole('heading', { name: 'No matching assistants', exact: true })).toBeVisible();
  await expect(list).toContainText('Try another name, or clear the search to see all assistants.');
  await expect(list.getByRole('heading', { name: 'No assistants yet', exact: true })).toHaveCount(0);
  await expect(list.locator('.assistant-list__group-heading')).toHaveCount(0);
  await sidebar.screenshot({ path: `/tmp/molr-assistant-no-matches-${info.project.name}.png` });
  await list.getByRole('button', { name: 'Clear search', exact: true }).click();
  await expect(search).toBeFocused();
  await expect(search).toHaveValue('');
  await expect(list.getByRole('button', { name: first.Name, exact: true })).toBeVisible();
  await expect(list.getByRole('button', { name: second.Name, exact: true })).toBeVisible();
  await search.fill(`研究 Alpha ${suffix}`);
  await expect(list.getByRole('button')).toHaveCount(1);
  await sidebar.screenshot({ path: `/tmp/molr-assistant-filter-${info.project.name}.png` });
  await list.getByRole('button', { name: first.Name, exact: true }).click();
  await expect(page.locator('.chat-header h1')).toHaveText(first.Name);
});

test('empty assistant list offers onboarding even with a search term', async ({ page }, info) => {
  await login(page);
  await seedAssistant(page, `Empty-state model ${info.project.name}-${Date.now()}`);
  // Isolate presentation states without deleting assistants used by other tests.
  await page.route(url => url.pathname === '/api/assistants', route => route.request().method() === 'GET'
    ? route.fulfill({ json: { Items: [], NextCursor: '' } }) : route.continue());
  await page.route('**/api/models', route => route.fulfill({ json: [] }));
  await page.goto('/#/assistants'); await page.reload();
  const sidebar = page.locator('.workspace-sidebar'), list = sidebar.locator('.assistant-list');
  const search = sidebar.getByRole('searchbox', { name: 'Search assistants', exact: true });
  await expect(list.getByRole('heading', { name: 'No assistants yet', exact: true })).toBeVisible();
  await expect(list).toContainText('Connect a model in Resources before hiring an assistant.');
  await expect(sidebar.locator('.sidebar-footer')).toHaveCount(0);
  if (info.project.name === 'desktop') {
    const placeholder = page.locator('.workspace-empty');
    await expect(placeholder.getByRole('heading', { name: 'Select an assistant to start a conversation', exact: true })).toBeVisible();
    await expect(placeholder.locator('.workspace-empty__logo img')).toBeVisible();
    await expect(placeholder.locator('button, a, input, [tabindex]')).toHaveCount(0);
  }
  await search.fill('nothing');
  await expect(list.getByRole('heading', { name: 'No assistants yet', exact: true })).toBeVisible();
  await expect(list.getByRole('heading', { name: 'No matching assistants', exact: true })).toHaveCount(0);
  await list.getByRole('button', { name: 'Connect a model', exact: true }).click();
  await expect(page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true })).toHaveAttribute('aria-current', 'page');
  await page.unroute('**/api/models');
  await page.goto('/#/assistants'); await page.reload();
  await expect(list.getByRole('button', { name: 'Hire new assistant', exact: true })).toBeVisible();
  await expect(list).toContainText('Hire an assistant to start your first conversation.');
  await sidebar.screenshot({ path: `/tmp/molr-assistant-empty-${info.project.name}.png` });
  await list.getByRole('button', { name: 'Hire new assistant', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Hire new assistant', exact: true })).toBeVisible();
});

test('search matches the shared design, filters real results and opens tasks', async ({ page }, info) => {
  await login(page);
  const name = `Search ${info.project.name}-${Date.now()}`;
  const assistant = await seedAssistant(page, name);
  const task = await post(page, '/api/tasks', { AssistantID: assistant.ID, Goal: `${name} goal`, AcceptanceCriteria: ['A reviewed report'], ResourceIDs: [] });
  await page.goto(`/#/assistants/${assistant.ID}`);
  if (info.project.name === 'mobile') await page.getByRole('button', { name: 'Back to assistants', exact: true }).click();
  else await page.getByRole('navigation').getByRole('button', { name: 'Assistants', exact: true }).click();
  const trigger = page.getByRole('button', { name: 'Search', exact: true }).first();
  if (info.project.name === 'desktop') await page.locator('.workspace-nav').screenshot({ path: '/tmp/molr-workspace-navigation.png' });
  await page.locator('.workspace-sidebar').screenshot({ path: `/tmp/molr-assistant-sidebar-${info.project.name}.png` });
  await trigger.click();
  const panel = page.getByRole('dialog', { name: 'Search', exact: true }), input = panel.getByRole('searchbox');
  await expect(input).toBeFocused();
  await expect(panel.locator('.ui-dialog__header').getByRole('searchbox')).toBeVisible();
  await expect(input).toHaveCSS('font-size', '16px');
  await expect(panel.locator('.workspace-search__suggestions .button').first()).toHaveCSS('font-weight', '400');
  await expect(panel.locator('.ui-dialog__header > button')).toHaveCount(0);
  await expect(panel.locator('.ui-dialog__footer')).toHaveCount(info.project.name === 'mobile' ? 0 : 1);
  const quick = panel.getByRole('region', { name: 'Quick navigation' });
  await expect(quick.getByRole('button')).toHaveCount(4);
  expect(await panel.locator('.workspace-search__shortcuts').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length)).toBe(2);
  if (info.project.name === 'mobile') {
    await expect(panel).toHaveAttribute('data-contained', 'true');
    await expect(panel).toHaveAttribute('data-side', 'bottom');
  }
  await panel.screenshot({ path: `/tmp/molr-webui-search-home-${info.project.name}.png` });
  const filter = panel.getByRole('combobox', { name: 'Filter search types' });
  await filter.click();
  await expect(panel.getByRole('option', { name: 'All', exact: true })).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('Escape');
  await input.fill(name);
  for (const source of ['Assistants', 'Resources', 'Tasks']) await expect(panel.getByRole('region', { name: source, exact: true })).toBeVisible();
  await expect(panel.locator('mark').first()).toHaveText(name);
  const clear = panel.getByRole('button', { name: 'Clear search', exact: true });
  const [clearBox, filterBox] = await readLayout(clear, filter);
  expect(clearBox.x + clearBox.width).toBeLessThanOrEqual(filterBox.x);
  await filter.click();
  await panel.getByRole('option', { name: 'Resources', exact: true }).click();
  await panel.getByRole('option', { name: 'Assistants', exact: true }).click();
  await page.keyboard.press('Escape');
  await expect(panel.getByRole('region', { name: 'Resources', exact: true })).toBeVisible();
  await expect(panel.getByRole('region', { name: 'Tasks', exact: true })).toHaveCount(0);
  await panel.screenshot({ path: `/tmp/molr-webui-search-results-${info.project.name}.png` });
  await input.fill('no-match-at-all-search-123');
  await expect(panel.getByRole('heading', { name: 'No matching results', exact: true })).toBeVisible();
  await panel.getByRole('button', { name: 'Start a new search', exact: true }).click();
  await expect(input).toHaveValue(''); await expect(input).toBeFocused();
  await expect(filter).toHaveAttribute('data-filter-active', 'false');
  await input.fill(name);
  await panel.getByRole('region', { name: 'Tasks', exact: true }).getByRole('button').click();
  await expect(panel).not.toBeVisible();
  const tasks = page.getByRole('dialog', { name: 'Plan details', exact: true });
  await expect(tasks.getByRole('heading', { name: task.Goal, exact: true })).toBeVisible();
  await expect(tasks.locator('.plan-execution[open]')).toContainText(task.Goal);
  await page.keyboard.press('Escape'); await expect(tasks).not.toBeVisible();
  if (info.project.name === 'mobile') await page.getByRole('button', { name: 'Back to assistants', exact: true }).click();
  else await page.getByRole('navigation').getByRole('button', { name: 'Assistants', exact: true }).click();
  await trigger.click(); await input.press('ArrowDown');
  await expect(quick.getByRole('button').first()).toBeFocused();
  await page.keyboard.press('End'); await expect(quick.getByRole('button').last()).toBeFocused();
  await page.keyboard.press('Home'); await page.keyboard.press('ArrowDown'); await page.keyboard.press('Enter');
  await expect(panel).not.toBeVisible(); await expect(page.getByRole('heading', { name: 'Resources', exact: true })).toBeVisible();
});

// Wait for page queries before advancing connection timers. Successful responses
// now wake recovery, so late initial loads must not race a simulated timeout.
function trackPageQueries(page: Page) {
  const pending = new Set<Request>();
  page.on('request', request => {
    const path = new URL(request.url()).pathname;
    if (path.startsWith('/api/') && path !== '/api/events/stream') pending.add(request);
  });
  page.on('requestfinished', request => pending.delete(request));
  page.on('requestfailed', request => pending.delete(request));
  return () => expect.poll(() => pending.size).toBe(0);
}

test('connection logo tracks real offline recovery and keeps its cached artwork', async ({ page, context }, info) => {
  const mobile = info.project.name === 'mobile';
  await login(page);
  const logo = page.locator('.connection-logo:visible');
  if (!mobile) await expect(logo).toHaveAttribute('data-connection', 'connected');
  await expect(page.locator('.connection-dot')).toHaveCount(0);
  const still = logo.locator('.connection-logo__still img');
  if (!mobile) await expect.poll(() => still.evaluate(el => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
  // Interrupt the actual browser transport; offline must not depend on fetching
  // a new SVG, and an online event must restore the real authenticated SSE.
  await context.setOffline(true);
  await expect(mobile ? page.locator('.connection-banner') : logo).toHaveAttribute('data-connection', 'disconnected');
  if (!mobile) {
    await expect(logo.locator('.connection-logo__art')).toHaveCSS('opacity', '0.24');
    await expect(logo.locator('.connection-logo__still')).toBeVisible();
    expect(await still.evaluate(el => (el as HTMLImageElement).complete && (el as HTMLImageElement).naturalWidth > 0)).toBe(true);
  }
  await context.setOffline(false);
  if (!mobile) {
    await expect(logo).toHaveAttribute('data-connection', 'recovered');
    await expect(logo.locator('.connection-logo__motion img')).toHaveAttribute('src', /-greet\.svg/);
    await expect(logo).toHaveAttribute('data-connection', 'connected');
  }
  await expect(page.locator('.connection-banner')).toHaveCount(0);
});

test('a stalled connection times out and clicking the logo retries the real stream', async ({ page }, info) => {
  const mobile = info.project.name === 'mobile';
  await page.clock.install();
  const settleQueries = trackPageQueries(page);
  await page.route('**/api/events/stream*', () => {});
  const failedQueries = new Set<string>();
  const unavailableQueries = /\/api\/(assistants|plans)(\?|$)/;
  await page.route(unavailableQueries, route => {
    failedQueries.add(new URL(route.request().url()).pathname);
    return route.fulfill({ status: 503, body: '' });
  });
  await login(page);
  await expect(page.locator('.assistant-workspace')).toBeVisible();
  await settleQueries();
  const logo = page.locator('.connection-logo:visible');
  const indicator = mobile ? page.locator('.connection-banner') : logo;
  await expect(indicator).toHaveAttribute('data-connection', 'reconnecting');
  if (!mobile) await expect(logo.locator('.connection-logo__motion img')).toHaveAttribute('src', /-loading\.svg/);
  await page.clock.fastForward(21_000);
  await expect(indicator).toHaveAttribute('data-connection', 'failed');
  await expect(page.locator('.connection-banner')).toContainText('Live updates unavailable. Retrying automatically.');
  const dangerColors = await page.evaluate(() => {
    const reference = document.createElement('div'); reference.className = 'ui-alert ui-alert--danger'; document.body.append(reference);
    const { backgroundColor, color } = getComputedStyle(reference); reference.remove();
    return { backgroundColor, color };
  });
  await expect(page.locator('.connection-banner')).toHaveCSS('background-color', dangerColors.backgroundColor);
  await expect(page.locator('.connection-banner')).toHaveCSS('color', dangerColors.color);
  if (!mobile) await expect(logo.locator('.connection-logo__art')).toHaveCSS('opacity', '0.24');
  // Both the workspace and the active page fail, but the shared banner is
  // sufficient. The banner uses Alert's danger colors in either theme.
  await page.getByRole('navigation').getByRole('button', { name: 'Plans', exact: true }).click();
  await expect.poll(() => failedQueries.size).toBe(2);
  await settleQueries();
  if (await indicator.getAttribute('data-connection') === 'reconnecting') await page.clock.fastForward(21_000);
  await expect(page.getByText('The service is unavailable. Try again shortly.', { exact: true })).toHaveCount(0);
  await page.unroute(unavailableQueries);
  await page.unroute('**/api/events/stream*');
  await indicator.getByRole('button', { name: /Retry now/ }).click();
  if (!mobile) await expect(logo).toHaveAttribute('data-connection', 'recovered');
  await page.clock.fastForward(2500);
  if (!mobile) await expect(logo).toHaveAttribute('data-connection', 'connected');
  await expect(page.locator('.connection-banner')).toHaveCount(0);
  // A specific service error while the connection is healthy still needs
  // its own feedback; suppression must not silence normal operation errors.
  await page.getByRole('navigation').getByRole('button', { name: 'Assistants', exact: true }).click();
  await page.route('**/api/plans?*', route => route.fulfill({ status: 503, body: '' }));
  await page.getByRole('navigation').getByRole('button', { name: 'Plans', exact: true }).click();
  await expect(page.getByText('The service is unavailable. Try again shortly.', { exact: true })).toBeVisible();
});

for (const trigger of ['timer', 'get', 'post'] as const) {
  test(`failed live updates recover through ${trigger} without an online event or manual retry`, async ({ page }) => {
    await page.clock.install();
    const settleQueries = trackPageQueries(page);
    let attempts = 0, pendingStream: Route | undefined;
    await page.route('**/api/events/stream*', route => { attempts++; pendingStream = route; });
    await login(page);
    await expect(page.locator('.assistant-workspace')).toBeVisible();
    await settleQueries();
    const settings = await (await page.request.get('/api/settings')).json();
    const notice = page.locator('.connection-banner');
    await expect(notice).toHaveAttribute('data-connection', 'reconnecting');
    await page.clock.fastForward(21_000);
    await expect(notice).toHaveAttribute('data-connection', 'failed');
    const before = attempts;
    expect(before).toBeGreaterThan(0);
    expect(await page.evaluate(() => navigator.onLine)).toBe(true);
    if (trigger !== 'timer') {
      const clientPath = `/@fs${new URL('../../../packages/sdk/src/client.ts', import.meta.url).pathname}`;
      // The same SDK instance used by the page, with real authenticated API responses.
      await page.evaluate(async ({ path, trigger, settings }) => {
        const { api } = await import(/* @vite-ignore */ path);
        if (trigger === 'get') await api.get('/api/assistants');
        else await api.mutate('/api/settings', { Settings: settings }, { idempotencyKey: crypto.randomUUID(), expectedVersion: settings.Version });
      }, { path: clientPath, trigger, settings });
      await expect.poll(() => attempts).toBe(before + 1);
      // API success starts an attempt; it cannot hide the notice before SSE ready.
      await expect(notice).toHaveAttribute('data-connection', 'reconnecting');
      await pendingStream!.continue();
    } else {
      await page.unroute('**/api/events/stream*');
      await page.clock.fastForward(5500);
    }
    await expect(notice).toHaveCount(0);
  });
}

test('connection notices stay above page cards across routes and short viewports', async ({ page }) => {
  await page.clock.install();
  const settleQueries = trackPageQueries(page);
  await page.route('**/api/events/stream*', () => {});
  await login(page);
  await expect(page.locator('.assistant-workspace')).toBeVisible();
  await settleQueries();
  await page.clock.fastForward(21_000);
  const notice = page.locator('.connection-banner');
  await expect(notice).toHaveAttribute('data-connection', 'failed');
  const viewport = page.viewportSize()!;
  for (const height of [viewport.height, 420]) {
    await page.setViewportSize({ ...viewport, height });
    for (const [route, ready] of Object.entries({ assistants: '.assistant-workspace', plans: '.plans-workspace', resources: '.resources-workspace', account: '.settings-workspace', settings: '.settings-workspace' })) {
      await page.evaluate(route => { location.hash = `#/${route}`; }, route);
      await expect(page.locator(`.workspace-main[data-page='${route}'] ${ready}`)).toBeVisible();
      await settleQueries();
      // Successful navigation queries may switch the notice to reconnecting.
      await expect(notice).toBeVisible();
      await expectFitsWebView(page);
      const [bannerBox, cardBox] = await readLayout(notice, page.locator('.workspace-main'));
      expect(cardBox.y - (bannerBox.y + bannerBox.height)).toBeCloseTo(8, 0);
      await expect(notice).toHaveCSS('border-radius', '16px');
      const button = notice.getByRole('button', { name: 'Retry now' });
      const color = await notice.evaluate(element => getComputedStyle(element).color);
      if (await button.count()) await expect(button).toHaveCSS('color', color);
    }
  }
  await page.unroute('**/api/events/stream*');
  // Finish the current attempt and allow automatic recovery after removing the outage.
  await page.clock.fastForward(21_000);
  await page.clock.fastForward(5500);
  await expect(notice).toHaveCount(0);
  await expectFitsWebView(page);
});

test('settings save independently, About shows Runtime details and Security signs out the session', async ({ page, context }, info) => {
  await login(page);
  await openUserSettings(page);
  const navItems = page.locator('.settings-nav > button');
  await expect(navItems).toHaveText(['General', 'Profile', 'Security', 'Notification settings', 'Theme', 'Usage', 'Memory', 'Personal data export', 'Former assistants', 'About']);
  await openSettingsSection(page, 'General');
  await expect(navItems.nth(0)).toHaveAttribute('aria-current', 'page');
  const general = settingsPane(page);
  await expect(general.getByRole('combobox', { name: 'Language', exact: true })).toBeVisible();
  await expect(general.getByRole('combobox', { name: 'Display mode', exact: true })).toHaveCount(0);
  await expect(settingsNavItem(page, 'Theme')).toHaveText('Theme');
  await expect(settingsNavItem(page, 'Preferences')).toHaveCount(0);
  await expect(general.getByRole('button', { name: 'Save', exact: true })).toHaveCount(0);
  await expect(general.getByRole('checkbox')).toHaveCount(0);
  for (const control of await general.getByRole('combobox').all()) {
    await expect(control).toHaveCSS('min-height', '40px');
    await expect(control).toHaveCSS('border-top-width', '1px');
  }
  await general.getByRole('combobox', { name: 'Timezone', exact: true }).click();
  const timezoneSearch = page.getByRole('combobox', { name: 'Search Timezone', exact: true });
  await expect(timezoneSearch).toBeFocused();
  await timezoneSearch.fill('Asia/Shanghai');
  await expect(page.getByRole('listbox', { name: 'Timezone', exact: true }).getByRole('option')).toHaveCount(1);
  await timezoneSearch.press('Enter');
  await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).Timezone).toBe('Asia/Shanghai');
  await openSettingsSection(page, 'Theme');
  await choose(page, 'Display mode', /^Light /);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).Theme).toBe('light');
  const settings = await (await page.request.get('/api/settings')).json();
  expect(settings).toMatchObject({ Timezone: 'Asia/Shanghai', Theme: 'light' });
  await page.reload(); await openSettingsSection(page, 'General');
  await expect(general.getByRole('combobox', { name: 'Timezone', exact: true })).toContainText('Shanghai');
  await general.screenshot({ path: info.outputPath('general-settings.png') });
  await choose(page, 'Language', '简体中文');
  await expect(settingsNavItem(page, '常规')).toHaveText('常规');
  await choose(page, '语言', 'English');
  await expect(settingsNavItem(page, 'General')).toHaveText('General');
  await openSettingsSection(page, 'Theme');
  await choose(page, 'Display mode', /^System /);
  await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).Theme).toBe('system');
  await expect(settingsNavItem(page, 'Connection')).toHaveCount(0);
  await openSettingsSection(page, 'About');
  const about = settingsPane(page);
  await expect(about.locator('.update-runtime-value').first()).toHaveText('Molr Runtime');
  await expect(about.locator('.update-runtime-value').last()).toHaveText(`http://127.0.0.1:${process.env.MOLR_TEST_RUNTIME_PORT ?? process.env.MOLR_E2E_RUNTIME_PORT ?? 18881}`);
  await expect(about.getByRole('button', { name: 'Disconnect', exact: true })).toHaveCount(0);
  await about.screenshot({ path: info.outputPath('about-runtime.png') });
  await openSettingsSection(page, 'Security');
  await expect(settingsPane(page).locator('.security-devices')).toContainText(' · Web');
  const cookies = (await context.cookies()).map(cookie => `${cookie.name}=${cookie.value}`).join('; ');
  await settingsPane(page).getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  expect((await page.request.get('/api/auth/me', { headers: { Cookie: cookies } })).status()).toBe(401);
  await page.reload(); await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
});

test('instant settings roll back a failed switch update and preserve other preferences', async ({ page }) => {
  await login(page);
  await openUserSettings(page);
  await openSettingsSection(page, 'Theme');
  const panel = settingsPane(page);
  const toggle = panel.getByRole('switch', { name: 'Reduce motion', exact: true });
  const before = await (await page.request.get('/api/settings')).json();
  const desired = !before.ReducedMotion;
  await page.route('**/api/settings', route => route.request().method() === 'POST' ? route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ code: 'unavailable' }) }) : route.continue());
  await toggle.setChecked(desired);
  await expect(page.locator('.ui-toast--danger').getByRole('alert')).toContainText('service is unavailable');
  await expect(toggle).toBeChecked({ checked: before.ReducedMotion });
  await page.unroute('**/api/settings');
  await toggle.setChecked(desired);
  await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).ReducedMotion).toBe(desired);
  const after = await (await page.request.get('/api/settings')).json();
  expect(after.DefaultModelID).toBe(before.DefaultModelID);
  expect(after).not.toHaveProperty('DefaultTaskBudget');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await page.reload(); await openSettingsSection(page, 'Theme'); await expect(toggle).toBeChecked({ checked: desired });
  await toggle.setChecked(before.ReducedMotion);
  await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).ReducedMotion).toBe(before.ReducedMotion);
});

async function holdRead(page: Page, pattern: string) {
  let release!: () => void, arrived!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const started = new Promise<void>(resolve => { arrived = resolve; });
  const handler = async (route: import('@playwright/test').Route) => { arrived(); await gate; await route.continue().catch(() => {}); };
  await page.route(pattern, handler);
  return { started, release: async () => { release(); await page.unroute(pattern, handler); } };
}
async function pauseLoadingClock(page: Page) {
  await page.clock.install({ time: new Date('2026-10-01T00:00:00Z') });
  await page.clock.pauseAt(new Date('2026-10-01T00:00:01Z'));
}

test('fast pages skip skeletons, slow first reads show them, and revisits retain data', async ({ page }, info) => {
  await login(page);
  const title = `Cached todo ${info.project.name}-${Date.now()}`;
  const todo = await post(page, '/api/todos', { Title: title });
  await pauseLoadingClock(page);
  const quick = await holdRead(page, '**/api/plans?*');
  await page.getByRole('navigation').getByRole('button', { name: 'Plans', exact: true }).click(); await selectPlanRange(page, /^No date/); await quick.started;
  await page.clock.runFor(100); await expect(page.locator('.ui-skeleton')).toHaveCount(0);
  await quick.release(); await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
  await page.clock.runFor(500); await expect(page.locator('.ui-skeleton')).toHaveCount(0);
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  const updatedTitle = `${title} updated`;
  await post(page, '/api/todos/update', { ID: todo.ID, Title: updatedTitle, Mutation: { ExpectedVersion: todo.Version, IdempotencyKey: crypto.randomUUID() } }, false);
  const revisit = await holdRead(page, '**/api/plans?*');
  await page.getByRole('navigation').getByRole('button', { name: 'Plans', exact: true }).click(); await selectPlanRange(page, /^No date/); await revisit.started;
  await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
  await page.clock.runFor(1000); await expect(page.locator('.ui-skeleton')).toHaveCount(0);
  await revisit.release(); await expect(page.getByRole('heading', { name: updatedTitle, exact: true })).toBeVisible();
  const slow = await holdRead(page, '**/api/plans?*');
  await page.getByRole('group', { name: 'Plan view' }).getByRole('button', { name: 'Calendar', exact: true }).click(); await page.getByRole('button', { name: 'Next month', exact: true }).click(); await slow.started;
  await page.clock.runFor(250); await expect(page.locator('.ui-skeleton')).toHaveCount(0);
  await page.clock.runFor(100); await expect(page.locator('.ui-skeleton').first()).toBeVisible();
  if (info.project.name === 'desktop') await expect(page.locator('.ui-skeleton').first()).toHaveCSS('background-image', /linear-gradient/);
  await slow.release(); await expect(page.locator('.ui-skeleton')).toHaveCount(0);
});

test('cached content is discarded on access denial and when signing out', async ({ page }, info) => {
  await login(page); const title = `Private snapshot ${info.project.name}-${Date.now()}`;
  await post(page, '/api/todos', { Title: title });
  await page.getByRole('navigation').getByRole('button', { name: 'Plans', exact: true }).click(); await selectPlanRange(page, /^No date/);
  await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await page.route('**/api/plans?*', route => route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ code: 'forbidden' }) }));
  await page.getByRole('navigation').getByRole('button', { name: 'Plans', exact: true }).click(); await selectPlanRange(page, /^No date/);
  await expect(page.getByRole('alert')).toContainText('Permission or security verification failed');
  await expect(page.getByRole('heading', { name: title, exact: true })).toHaveCount(0);
  await page.unroute('**/api/plans?*');
  await page.locator('.ui-toasts').getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  const denied = await holdRead(page, '**/api/plans?*');
  await page.getByRole('navigation').getByRole('button', { name: 'Plans', exact: true }).click(); await selectPlanRange(page, /^No date/); await denied.started;
  await expect(page.getByRole('heading', { name: title, exact: true })).toHaveCount(0);
  await expect(page.locator('.ui-skeleton').first()).toBeVisible();
  await denied.release(); await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
  await page.evaluate(() => { location.hash = '#/account/security'; });
  await openSettingsSection(page, 'Security');
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  // Sign in within the existing document, so a full page reload cannot conceal
  // an incorrectly retained cache from the previous authenticated session.
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const fresh = await holdRead(page, '**/api/plans?*');
  await page.getByRole('navigation').getByRole('button', { name: 'Plans', exact: true }).click(); await selectPlanRange(page, /^No date/); await fresh.started;
  await expect(page.getByRole('heading', { name: title, exact: true })).toHaveCount(0);
  await expect(page.locator('.ui-skeleton').first()).toBeVisible();
  await fresh.release(); await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
});

test('returning to a chat displays its own cached history while refreshing', async ({ page }, info) => {
  await login(page);
  const first = await seedAssistant(page, `Cached chat ${info.project.name}-${Date.now()}`);
  const second = await seedAssistant(page, `Other chat ${info.project.name}-${Date.now()}`);
  const text = `Stored conversation ${Date.now()}`;
  await post(page, '/api/conversations/messages', { ConversationID: first.ConversationID, Content: { Kind: 'text', Text: text } });
  await page.goto(`/#/assistants/${first.ID}`);
  await expect(page.getByRole('log').getByText(text, { exact: true })).toBeVisible();
  await page.evaluate(id => { location.hash = `/assistants/${id}`; }, second.ID);
  await expect(page.locator('.chat-header h1')).toHaveText(second.Name);
  await expect(page.getByRole('log').getByText(text, { exact: true })).toHaveCount(0);
  const history = await holdRead(page, `**/api/conversations/${first.ConversationID}/messages?*`);
  await page.evaluate(id => { location.hash = `/assistants/${id}`; }, first.ID); await history.started;
  await expect(page.getByRole('log').getByText(text, { exact: true })).toBeVisible();
  await page.waitForTimeout(400); await expect(page.getByRole('log').locator('.ui-skeleton')).toHaveCount(0);
  await history.release();
});


test('DeepSeek built-in pricing stays automatic when model settings are saved', async ({ page }, info) => {
  await login(page);
  const name = `DeepSeek pricing ${info.project.name}-${Date.now()}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible',
    Binding: { PricingProvider: 'deepseek', Endpoint: 'https://api.deepseek.com/v1/chat/completions', AllowedOrigins: ['https://api.deepseek.com'], Models: [{ Name: 'deepseek-flash', ContextLimit: 32768, SupportsTools: true }] } });
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await page.reload();
  const card = resourceRow(page, name);
  await openProviderModels(card);
  await expect(page.locator('.provider-models-page').first().getByText('Pricing not configured', { exact: true })).toHaveCount(0);
  const configure = async () => {
    await page.locator('.provider-models-page').first().locator('.resource-model-open').click();
    return page.getByRole('dialog', { name: 'Configure model', exact: true });
  };
  let dialog = await configure();
  await expect(dialog.getByRole('switch', { name: 'Custom pricing', exact: true })).not.toBeChecked();
  await expect(dialog.getByRole('spinbutton')).toHaveCount(2);
  await dialog.getByRole('spinbutton', { name: 'Manual context limit (tokens, optional)', exact: true }).fill('8192');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const catalog = async () => (await (await page.request.get('/api/models')).json()).find((model: { ResourceID: string }) => model.ResourceID === resource.ID);
  expect((await catalog()).PricingInfo.Source).toBe('builtin');
  dialog = await configure();
  await dialog.getByRole('switch', { name: 'Custom pricing', exact: true }).click();
  for (const field of await dialog.getByRole('spinbutton', { name: /price .*million tokens/ }).all()) await field.fill('0');
  await dialog.getByRole('spinbutton', { name: 'Manual context limit (tokens, optional)', exact: true }).fill('8192');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect((await catalog()).PricingInfo.Source).toBe('manual');
  dialog = await configure();
  await dialog.getByRole('switch', { name: 'Custom pricing', exact: true }).click();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect((await catalog()).PricingInfo.Source).toBe('builtin');
});

test('model menu refreshes pricing and reports the default built-in source', async ({ page }) => {
  await login(page);
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  const response = page.waitForResponse(response => response.url().endsWith('/api/models/refresh-pricing') && response.request().method() === 'POST');
  await page.getByRole('menuitem', { name: 'Refresh model pricing', exact: true }).click();
  expect((await response).status()).toBe(200);
  await expect(page.locator('.ui-toasts').getByText('No external pricing source configured; using built-in prices', { exact: true })).toBeVisible();
});

test('official provider prices preserve currencies and usage tiers', async ({ page }, info) => {
  await login(page);
  const suffix = `${info.project.name}-${Date.now()}`;
  const cases = [
    ['zhipu', 'https://open.bigmodel.cn/api/paas/v4/chat/completions', 'glm-4.7', 'CNY'],
    ['zai', 'https://api.z.ai/api/paas/v4/chat/completions', 'glm-5.3', 'USD'],
    ['openai', 'https://api.openai.com/v1/chat/completions', 'gpt-5.4', 'USD'],
    ['minimax', 'https://api.minimax.cn/v1/chat/completions', 'MiniMax-M3', 'CNY'],
    ['gemini', 'https://generativelanguage.googleapis.com/v1beta/openai/chat/completions', 'gemini-3.1-pro-preview', 'USD'],
    ['anthropic', 'https://api.anthropic.com/v1/messages', 'claude-haiku-5-5', 'USD'],
    ['moonshot', 'https://api.moonshot.cn/v1/chat/completions', 'kimi-k2.6', 'CNY'],
  ];
  let zhipuID = '';
  for (const [preset, endpoint, model, currency] of cases) {
    const resource = await post(page, '/api/resources', { Name: `Auto ${preset} ${suffix}`, Kind: 'model', Provider: 'openai-compatible', Binding: { PricingProvider: preset, Endpoint: endpoint, AllowedOrigins: [new URL(endpoint).origin], Models: [{ Name: model, ContextLimit: 32768, SupportsTools: true }] } });
    const models = await (await page.request.get('/api/models')).json();
    const item = models.find((value: { ResourceID: string }) => value.ResourceID === resource.ID);
    expect(item.PricingInfo.Source).toBe('builtin'); expect(item.Pricing.Currency).toBe(currency); expect(item.PricingInfo.Period).toBe('');
    if (preset === 'zhipu') zhipuID = resource.ID;
  }
  await page.goto('/#/resources'); await page.reload();
  await openProviderModels(resourceRow(page, `Auto zhipu ${suffix}`));
  await page.locator('.provider-models-page').first().locator('.resource-model-open').click();
  const dialog = page.getByRole('dialog', { name: 'Configure model', exact: true });
  await expect(dialog.getByRole('switch', { name: 'Custom pricing', exact: true })).not.toBeChecked();
  await expect(dialog.getByText('Base tier', { exact: true })).toBeVisible();
  await expect(dialog.getByText('Output > 199 tokens', { exact: true })).toBeVisible();
  await expect(dialog.getByText('Input > 31,999 tokens', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const models = await (await page.request.get('/api/models')).json();
  const item = models.find((value: { ResourceID: string }) => value.ResourceID === zhipuID);
  expect(item.PricingInfo.Source).toBe('builtin'); expect(item.Pricing.Tiers).toHaveLength(2);
});

test('assistant budgets are set at hire and edited independently', async ({ page }, info) => {
  await login(page);
  const seed = await seedAssistant(page, `Budget model ${info.project.name}-${Date.now()}`);
  await page.goto('/#/assistants'); await page.reload();
  await page.getByRole('button', { name: 'Hire new assistant', exact: true }).first().click();
  const hire = page.getByRole('dialog', { name: 'Hire new assistant', exact: true });
  const name = `Budget assistant ${info.project.name}-${Date.now()}`;
  await hire.getByRole('textbox', { name: 'Assistant name', exact: true }).fill(name);
  await expect(hire.getByRole('spinbutton', { name: 'Spending limit (USD)', exact: true })).toHaveValue('');
  await hire.getByRole('spinbutton', { name: 'Spending limit (USD)', exact: true }).fill('2');
  await choose(page, 'Reset period', 'Weekly');
  await hire.getByRole('button', { name: 'Hire new assistant', exact: true }).click();
  await expect(hire).not.toBeVisible();
  await expect(page.locator('.chat-header h1')).toHaveText(name);
  const assistant = (await (await page.request.get('/api/assistants')).json()).Items.find((item: { Name: string }) => item.Name === name);
  expect(assistant.Budget).toEqual({ Limit: { Currency: 'USD', Micros: 2_000_000 }, Period: 'weekly' });
  const usage = await (await page.request.get(`/api/usage?assistant_id=${assistant.ID}&current_period=true`)).json();
  expect(usage.Limit.Micros).toBe(2_000_000); expect(usage.PeriodEnd).toBeTruthy();
  await page.reload(); await assistantMenu(page, 'Assistant information');
  const edit = page.getByRole('dialog', { name: 'Assistant information', exact: true });
  await expect(edit.getByRole('spinbutton', { name: 'Spending limit (USD)', exact: true })).toHaveValue('2');
  await expect(edit.getByRole('combobox', { name: 'Reset period', exact: true })).toContainText('Weekly');
  await expect(edit.getByText('Spent this period', { exact: true })).toBeVisible();
  await edit.getByRole('spinbutton', { name: 'Spending limit (USD)', exact: true }).fill('3');
  await choose(page, 'Reset period', 'Daily');
  await edit.getByRole('button', { name: 'Save', exact: true }).click(); await expect(edit).not.toBeVisible();
  expect((await (await page.request.get(`/api/assistants/${assistant.ID}`)).json()).Budget).toEqual({ Limit: { Currency: 'USD', Micros: 3_000_000 }, Period: 'daily' });
  expect((await (await page.request.get(`/api/assistants/${seed.ID}`)).json()).Budget.Limit.Micros).toBe(0);
  await page.locator('.chat-header').getByRole('button', { name: 'Plans', exact: true }).click();
  await page.getByRole('dialog', { name: 'Plans', exact: true }).locator('.plans-header').getByRole('button', { name: 'New plan', exact: true }).click();
  const task = page.getByRole('dialog', { name: 'New plan', exact: true });
  await expect(task.getByRole('spinbutton')).toHaveCount(0);
  await task.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('dialog', { name: 'Plans', exact: true }).locator('.ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click();
  await openUserSettings(page);
  await openSettingsSection(page, 'Preferences');
  await expect(settingsPane(page).getByRole('spinbutton')).toHaveCount(0);
});

test('currency preferences preserve native budgets and prices without assistant budget summaries', async ({ page }, info) => {
  await login(page);
  const previous = await (await page.request.get('/api/settings')).json();
  const name = `Currency ${info.project.name}-${Date.now()}`;
  const seed = await seedAssistant(page, `${name} seed`);
  const restore = async () => {
    const latest = await (await page.request.get('/api/settings')).json();
    await post(page, '/api/settings', { Settings: { ...latest, Currency: previous.Currency, Timezone: previous.Timezone }, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: latest.Version } }, false);
  };
  try {
    await openUserSettings(page);
    await openSettingsSection(page, 'General');
    await choose(page, 'Currency', 'Chinese yuan CNY');
    await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).Currency).toBe('CNY');
    const pref = await (await page.request.get('/api/settings')).json();
    await post(page, '/api/settings', { Settings: { ...pref, Timezone: 'Asia/Shanghai' }, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: pref.Version } }, false);
    await page.goto('/#/assistants'); await page.reload();
    await page.getByRole('button', { name: 'Hire new assistant', exact: true }).first().click();
    const hire = page.getByRole('dialog', { name: 'Hire new assistant', exact: true });
    await expect(hire.getByRole('combobox', { name: 'Currency', exact: true })).toContainText('Chinese yuan CNY');
    await expect(hire).not.toContainText('UTC+08:00 Beijing, Shanghai (Asia/Shanghai)');
    await expect(hire).not.toContainText('at midnight every day');
    await choose(page, 'Reset period', 'Weekly');
    await expect(hire).not.toContainText('at midnight every Monday');
    await expect(hire).not.toContainText('at midnight every day');
    await hire.getByRole('textbox', { name: 'Assistant name', exact: true }).fill(name);
    await hire.getByRole('spinbutton', { name: 'Spending limit (CNY)', exact: true }).fill('7');
    await hire.getByRole('button', { name: 'Hire new assistant', exact: true }).click();
    await expect(hire).not.toBeVisible();
    await expect(page.locator('.chat-header h1')).toHaveText(name);
    const assistant = (await (await page.request.get('/api/assistants')).json()).Items.find((a: { Name: string }) => a.Name === name);
    expect(assistant.Budget).toEqual({ Limit: { Currency: 'CNY', Micros: 7_000_000 }, Period: 'weekly' });
    // New model prices default to the preference; saved values keep that denomination.
    const back = page.getByRole('button', { name: 'Back to assistants', exact: true });
    if (await back.isVisible()) await back.click();
    const toastClose = page.locator('.ui-toasts').getByRole('button', { name: 'Close', exact: true });
    while (await toastClose.count()) await toastClose.first().click();
    await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
    const card = resourceRow(page, `${name} seed`);
    await openProviderModels(card);
    await cardAction(page, card, 'Add model');
    const model = page.getByRole('dialog', { name: 'Add model', exact: true });
    await expect(model.getByRole('combobox', { name: 'Currency', exact: true })).toContainText('Chinese yuan CNY');
    await model.getByRole('textbox', { name: 'Model identifier', exact: true }).fill('native-cny');
    await model.getByRole('spinbutton', { name: 'Input price (CNY / million tokens)', exact: true }).fill('7');
    await model.getByRole('spinbutton', { name: 'Output price (CNY / million tokens)', exact: true }).fill('14');
    await model.getByRole('spinbutton', { name: 'Cached input price (CNY / million tokens)', exact: true }).fill('1');
    await model.getByRole('button', { name: 'Save', exact: true }).click(); await expect(model).not.toBeVisible();
    const models = await (await page.request.get('/api/models')).json();
    const resourceID = models.find((m: { ID: string }) => m.ID === seed.ModelID).ResourceID;
    const nativeModel = models.find((m: { ResourceID: string; Name: string }) => m.ResourceID === resourceID && m.Name === 'native-cny');
    expect(nativeModel.Pricing).toMatchObject({ Currency: 'CNY', InputMicrosPerMillion: 7_000_000, OutputMicrosPerMillion: 14_000_000 });
    await openUserSettings(page);
    await openSettingsSection(page, 'General');
    await choose(page, 'Currency', 'US dollar USD');
    await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).Currency).toBe('USD');
    await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
    await assistantMenu(page, 'Assistant information');
    const edit = page.getByRole('dialog', { name: 'Assistant information', exact: true });
    await expect(edit.getByRole('combobox', { name: 'Currency', exact: true })).toContainText('Chinese yuan CNY');
    await expect(edit.getByRole('spinbutton', { name: 'Spending limit (CNY)', exact: true })).toHaveValue('7');
    await expect(edit.getByText('Saved spending limit', { exact: true })).toHaveCount(0);
    await edit.getByRole('button', { name: 'Save', exact: true }).click(); await expect(edit).not.toBeVisible();
    expect((await (await page.request.get(`/api/assistants/${assistant.ID}`)).json()).Budget).toEqual(assistant.Budget);
    await assistantMenu(page, 'Assistant information');
    await choose(page, 'Currency', 'US dollar USD');
    await edit.getByRole('spinbutton', { name: 'Spending limit (USD)', exact: true }).fill('2');
    await edit.getByRole('button', { name: 'Save', exact: true }).click(); await expect(edit).not.toBeVisible();
    expect((await (await page.request.get(`/api/assistants/${assistant.ID}`)).json()).Budget.Limit).toEqual({ Currency: 'USD', Micros: 2_000_000 });
  } finally { await restore(); }
});

test('developer workbench records hidden executions and only gates entry visibility', async ({ page }, info) => {
  test.setTimeout(90_000);
  await login(page);
  const before = await (await page.request.get('/api/settings')).json();
  const setVisible = async (visible: boolean) => page.evaluate(async visible => {
    const modulePath = '/@id/@molr/sdk'; const { api } = await import(modulePath);
    const current = await api.get('/api/settings');
    await api.mutate('/api/settings', { Settings: { ...current, WorkbenchVisible: visible } }, { idempotencyKey: crypto.randomUUID(), expectedVersion: current.Version });
  }, visible);
  try {
    await setVisible(false);
    await page.goto('/#/account/general');
    await openSettingsSection(page, 'General');
    expect((await page.request.get('/api/debug/runs')).status()).toBe(200);
    expect((await page.request.get('/api/debug/trajectory')).status()).toBe(200);
    await page.route('**/api/status', async route => { const response = await route.fetch(); await route.fulfill({ json: { ...await response.json(), workbench_available: false } }); });
    await page.reload(); await openSettingsSection(page, 'General');
    await expect(page.getByRole('switch', { name: 'Show developer workbench', exact: true })).toBeDisabled();
    const help = page.getByRole('button', { name: 'Show developer workbench information', exact: true });
    const tip = page.getByRole('tooltip').filter({ hasText: 'This Runtime has not enabled debugging.' });
    await expect(tip).not.toBeVisible();
    await help.click();
    await expect(tip).toBeVisible();
    await expect(page.getByRole('switch', { name: 'Show developer workbench', exact: true })).toBeDisabled();
    await help.click();
    await expect(tip).not.toBeVisible();
    await help.click();
    await page.keyboard.press('Escape');
    await expect(tip).not.toBeVisible();
    await page.unrouteAll({ behavior: 'wait' }); await page.reload(); await openSettingsSection(page, 'General');
    // Capture a complete execution before the owner ever shows the workbench.
    const assistant = await seedAssistant(page, `Debug ${info.project.name}-${Date.now()}`);
    await expect.poll(async () => (await (await page.request.get(`/api/debug/runs?assistant_id=${assistant.ID}`)).json()).Items.find((run: { Trigger: string }) => run.Trigger === 'born')?.State, { timeout: 15000 }).toBe('succeeded');
    await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'Debug context source check' } });
    await expect.poll(async () => (await (await page.request.get(`/api/debug/runs?assistant_id=${assistant.ID}`)).json()).Items.find((run: { Trigger: string }) => run.Trigger === 'user_message')?.State, { timeout: 15000 }).toBe('succeeded');
    await expect(page.getByRole('navigation').getByRole('button', { name: 'Workbench', exact: true })).toHaveCount(0);
    await page.getByRole('switch', { name: 'Show developer workbench', exact: true }).click();
    await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).WorkbenchVisible).toBe(true);
    await expect(page.getByRole('navigation').getByRole('button', { name: 'Workbench', exact: true })).toBeVisible();
    await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
    await expect(page.locator('.chat-context-usage')).toHaveCount(0);
    await assistantMenu(page, 'Debug this assistant');
    await expect(page.getByRole('heading', { name: 'Workbench', exact: true })).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Assistant', exact: true })).toContainText(assistant.Name);
    const timeline = page.getByRole('region', { name: 'Execution timeline', exact: true });
    const inspector = page.getByRole('region', { name: 'Step details', exact: true });
    await expect(timeline.locator('.trajectory-record--model')).toHaveCount(6);
    await timeline.locator('.trajectory-record--model .trajectory-record-body').last().click();
    await expect(page.getByRole('switch', { name: 'Follow live execution', exact: true })).not.toBeChecked();
    await inspector.getByRole('tab', { name: 'Context', exact: true }).click();
    await expect(inspector).toContainText('Debug context source check');
    for (const role of ['system', 'assistant', 'tool', 'user']) await expect(inspector.getByRole('progressbar', { name: role, exact: true })).toBeVisible();
    expect(Number(await inspector.getByRole('progressbar', { name: 'user', exact: true }).getAttribute('aria-valuenow'))).toBeGreaterThan(0);
    const contextCodeMessage = inspector.locator('.workbench-message:has(.ui-code-block)').first();
    if (await contextCodeMessage.getAttribute('open') === null) await contextCodeMessage.locator('summary').first().click();
    for (const disclosure of await contextCodeMessage.locator('details:has(.ui-code-block)').all()) {
      if (await disclosure.getAttribute('open') === null) await disclosure.locator('summary').first().click();
    }
    await expect(contextCodeMessage.locator('.ui-code-token--property').first()).toBeVisible();
    await expect(inspector).toContainText('Internal reasoning is not a chat message.');
    await inspector.getByRole('tab', { name: 'Context diff', exact: true }).click();
    await expect(inspector).toContainText('Added / replaced');
    await expect(inspector.locator('.ui-code-block[data-language="json"]').first()).toBeVisible();
    await expect(inspector).toContainText('conversation_publish');
    await inspector.getByRole('tab', { name: 'Model request', exact: true }).click();
    await expect(inspector).toContainText('"messages"'); await expect(inspector).toContainText('"tools"');
    await expect(inspector.locator('.ui-code-token--property').filter({ hasText: '"messages"' }).first()).toBeAttached();
    await inspector.getByRole('tab', { name: 'Model response', exact: true }).click();
    await expect(inspector.getByRole('tab', { name: 'Response content', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(inspector).toContainText('Internal reasoning is not a chat message.');
    await inspector.getByRole('tab', { name: 'Response metadata', exact: true }).click();
    await expect(inspector).toContainText('200');
    await expect(inspector.locator('.ui-code-token--number').first()).toBeAttached();
    await inspector.getByRole('tab', { name: 'Raw data', exact: true }).click();
    await expect(inspector.locator('.ui-code-block')).toHaveAttribute('data-highlighted', 'true');
    // Save events retain their own version and explain that full intermediate snapshots are omitted.
    await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    await page.getByRole('switch', { name: 'Show all events', exact: true }).click();
    await timeline.locator('.trajectory-scroll').evaluate(panel => { panel.scrollTop = 0; });
    const saveRecords = timeline.locator('.trajectory-record--event').filter({ hasText: 'Checkpoint saved' });
    await expect(saveRecords.first()).toBeVisible();
    await saveRecords.first().locator('.trajectory-record-body').click();
    await inspector.getByRole('tab', { name: 'Context', exact: true }).click();
    await expect(inspector.locator('.workbench-checkpoint-metadata dd').first()).toHaveText('1');
    await expect(inspector).toContainText('without duplicating the full context');
    await expect(inspector).toContainText('Event received');
    await expect(inspector).not.toContainText('Internal reasoning is not a chat message.');
    await inspector.getByRole('tab', { name: 'Context diff', exact: true }).click();
    await expect(inspector).toContainText('No context snapshots to compare.');
    await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    await timeline.locator('.trajectory-record--event').filter({ hasText: 'Load checkpoint' }).first().locator('.trajectory-record-body').click();
    await inspector.getByRole('tab', { name: 'Context', exact: true }).click();
    await expect(inspector.locator('.workbench-checkpoint-metadata dd').first()).toHaveText('0');
    await expect(inspector).toContainText('No execution context.');
    await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    await page.getByRole('switch', { name: 'Show all events', exact: true }).click();
    await page.getByRole('button', { name: 'Latest checkpoint', exact: true }).click();
    await expect(inspector.getByRole('heading', { name: 'Latest checkpoint', exact: true })).toBeVisible();
    await expect(inspector.getByRole('progressbar')).toHaveCount(4);
    await expect(inspector).toContainText('Runtime delivered this reply through conversation_publish.');
    await page.reload(); await expect(timeline.locator('.trajectory-record--model')).toHaveCount(6);
    await expect.poll(() => timeline.locator('.trajectory-scroll').evaluate(panel => panel.scrollHeight - panel.clientHeight - panel.scrollTop < 2)).toBe(true);
    if (process.env.MOLR_CAPTURE_WORKBENCH) await page.screenshot({ path: `/tmp/molr-workbench-${info.project.name}.png`, fullPage: true });
    await page.getByRole('button', { name: 'Latest checkpoint', exact: true }).click();
    const checkpointBefore = await (await page.request.get(`/api/debug/assistants/${assistant.ID}/checkpoint`)).json();
    const messagesBefore = await (await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json();
    await inspector.getByRole('button', { name: 'Reset context', exact: true }).click();
    const resetDialog = page.getByRole('dialog', { name: 'Reset context', exact: true });
    await expect(resetDialog).toContainText(assistant.Name);
    await expect(resetDialog).toContainText('Conversations, memories, permissions and budget settings will be kept.');
    await resetDialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    expect((await (await page.request.get(`/api/debug/assistants/${assistant.ID}/checkpoint`)).json()).Data.Sequence).toBe(checkpointBefore.Data.Sequence);
    await inspector.getByRole('button', { name: 'Reset context', exact: true }).click();
    await resetDialog.getByRole('button', { name: 'Confirm', exact: true }).click();
    await expect(resetDialog).not.toBeVisible();
    await expect(inspector).toContainText('No execution context.');
    const checkpointAfter = await (await page.request.get(`/api/debug/assistants/${assistant.ID}/checkpoint`)).json();
    expect(checkpointAfter.Data.Sequence).toBe(checkpointBefore.Data.Sequence + 1);
    expect(checkpointAfter.Data.Context.Inputs).toEqual([]);
    expect(await (await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json()).toEqual(messagesBefore);

    await setVisible(false);
    await expect(page.getByRole('navigation').getByRole('button', { name: 'Workbench', exact: true })).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Workbench', exact: true })).toBeVisible();
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Workbench', exact: true })).toBeVisible();
    expect((await page.request.get(`/api/debug/runs?assistant_id=${assistant.ID}`)).status()).toBe(200);
  } finally { await setVisible(before.WorkbenchVisible ?? false); }
});

test('assistant information unifies settings, real tools, permissions and Skills', async ({ page }, info) => {
  await login(page);
  const assistant = await seedAssistant(page, `Info ${info.project.name}-${Date.now()}`);
  await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Keep this chat draft');
  await page.getByRole('button', { name: `View information for ${assistant.Name}`, exact: true }).click();
  const drawer = page.getByRole('dialog', { name: 'Assistant information', exact: true });
  await expect(drawer).toHaveClass(/ui-drawer/);
  await expect(drawer).toHaveAttribute('data-side', info.project.name === 'mobile' ? 'bottom' : 'right');
  if (info.project.name === 'desktop') await expect(drawer).toHaveCSS('width', '600px');
  await expect(drawer.getByRole('tab')).toHaveText(['Overview', 'Tools', 'Permissions', 'Skills']);
  await expect(drawer.getByRole('combobox', { name: 'Model', exact: true })).toContainText(assistant.Name);
  await expect(drawer.getByText(`Model provider · ${assistant.Name}`, { exact: true })).toBeVisible();
  await drawer.getByRole('textbox', { name: 'Assistant name', exact: true }).fill('Unsaved assistant name');
  await drawer.getByRole('tab', { name: 'Tools', exact: true }).click();
  const tools = drawer.getByRole('tabpanel', { name: 'Tools', exact: true });
  const toolCatalog = await (await page.request.get(`/api/assistants/${assistant.ID}/capabilities`)).json();
  await expect(tools.getByRole('article')).toHaveCount(toolCatalog.Tools.length);
  for (const name of ['List plans', 'Read plan', 'Manage plans', 'Read attachment', 'Manage running tasks', 'Search files', 'Read file']) {
    await expect(tools.getByRole('heading', { name, exact: true })).toBeVisible();
  }
  for (const name of ['Search', 'Update memory', 'Create tasks', 'Read tasks', 'Report task progress', 'Create plan', 'Update plan', 'Delete plan', 'Configure plan', 'Execute plan']) {
    await expect(tools.getByRole('heading', { name, exact: true })).toHaveCount(0);
  }
  await expect(tools.getByRole('heading', { name: 'List permissions', exact: true })).toBeVisible();
  await expect(tools.getByRole('heading', { name: 'List sandbox resources', exact: true })).toHaveCount(0);
  await expect(tools.getByRole('heading', { name: 'List directory', exact: true })).toBeVisible();
  await expect(tools.getByRole('heading', { name: 'Edit file', exact: true })).toBeVisible();
  const shell = tools.getByRole('article').filter({ has: page.getByRole('heading', { name: 'Execute shell', exact: true }) });
  await expect(shell).toContainText('Requirements not met');
  await expect(shell).toContainText(/No valid permission for the required resource operation\.|This device cannot currently provide a secure command environment\./);
  const readStorage = tools.getByRole('article').filter({ has: page.getByRole('heading', { name: 'Read file', exact: true }) });
  await expect(readStorage).toContainText('Your assistant workspace is available. External storage needs separate permission.');
  await readStorage.locator('.assistant-tool-disclosure > summary').click();
  await expect(readStorage.getByText('file_read', { exact: true })).toBeVisible();
  await readStorage.locator('.assistant-tool-schema > summary').click();
  await expect(readStorage.locator('pre')).toContainText('properties');
  await tools.getByRole('button', { name: 'Configure permissions', exact: true }).click();
  await expect(drawer.getByRole('tab', { name: 'Permissions', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(drawer.getByText('View models · Call models', { exact: true })).toBeVisible();
  await drawer.getByRole('tab', { name: 'Skills', exact: true }).click();
  const capabilities = await (await page.request.get(`/api/assistants/${assistant.ID}/capabilities`)).json();
  await expect(drawer.getByText(capabilities.Skills.Supported ? 'No Skills assigned' : 'This Runtime does not support Skills yet', { exact: true })).toBeVisible();
  await drawer.getByRole('tab', { name: 'Overview', exact: true }).click();
  await expect(drawer.getByRole('textbox', { name: 'Assistant name', exact: true })).toHaveValue('Unsaved assistant name');
  await drawer.screenshot({ path: `/tmp/molr-assistant-info-${info.project.name}.png` });
  await drawer.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(drawer).not.toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toHaveValue('Keep this chat draft');
  expect((await (await page.request.get(`/api/assistants/${assistant.ID}`)).json()).Name).toBe(assistant.Name);
});

test('chat panels dock by workspace width and preserve state across layouts', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'This test resizes across desktop and mobile layouts.');
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
  await page.setViewportSize({ width: 1800, height: 1100 });
  await login(page);
  const assistant = await seedAssistant(page, `Docked ${Date.now()}`);
  await page.goto(`/#/assistants/${assistant.ID}`);
  const composer = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  await composer.fill('Keep composing while the panel is open');
  await page.getByRole('button', { name: `View information for ${assistant.Name}`, exact: true }).click();
  let panel = page.getByRole('complementary', { name: 'Assistant information', exact: true });
  await expect(panel).toBeVisible();
  await expect(page.locator('dialog:modal')).toHaveCount(0);
  await expect(page.locator('.chat-side-panel')).toHaveCSS('flex-basis', '600px');
  const [chatBounds, panelBounds] = await readLayout(page.locator('.chat-panel'), panel);
  expect(chatBounds!.width).toBeGreaterThanOrEqual(480);
  expect(chatBounds!.x + chatBounds!.width).toBeLessThanOrEqual(panelBounds!.x + 1);
  await panel.getByRole('textbox', { name: 'Assistant name', exact: true }).fill('Unsaved across resize');
  await composer.fill('Chat remains interactive');
  await expect(panel).toBeVisible();
  await panel.locator(':scope > .ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).focus();
  await page.keyboard.press('Shift+Tab');
  expect(await panel.evaluate(element => element.contains(document.activeElement))).toBe(false);
  await panel.getByRole('tab', { name: 'Tools', exact: true }).click();
  await page.screenshot({ path: '/tmp/molr-chat-sidebar-wide.png', fullPage: true });

  await page.setViewportSize({ width: 1200, height: 1000 });
  panel = page.getByRole('dialog', { name: 'Assistant information', exact: true });
  await expect(panel).toBeVisible();
  await expect(panel).toHaveAttribute('data-side', 'right');
  await expect(panel.getByRole('tab', { name: 'Tools', exact: true })).toHaveAttribute('aria-selected', 'true');
  expect(await panel.evaluate(element => element.matches(':modal'))).toBe(true);
  await panel.getByRole('tab', { name: 'Overview', exact: true }).click();
  await expect(panel.getByRole('textbox', { name: 'Assistant name', exact: true })).toHaveValue('Unsaved across resize');

  await page.setViewportSize({ width: 390, height: 844 });
  await expect(panel).toHaveAttribute('data-side', 'bottom');
  await expect(panel).toHaveAttribute('data-contained', 'true');
  await expect(panel.getByRole('textbox', { name: 'Assistant name', exact: true })).toHaveValue('Unsaved across resize');
  await page.setViewportSize({ width: 1800, height: 1100 });
  panel = page.getByRole('complementary', { name: 'Assistant information', exact: true });
  await expect(panel).toBeVisible();
  await expect(page.locator('dialog:modal')).toHaveCount(0);
  await expect(panel.getByRole('textbox', { name: 'Assistant name', exact: true })).toHaveValue('Unsaved across resize');
  await composer.fill('No leftover modal blocking');
  await panel.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(panel).not.toBeVisible();

  const plan = await post(page, '/api/todos', { Title: 'Plan to edit across layouts', AssistantID: assistant.ID });
  await page.locator('.chat-header').getByRole('button', { name: 'Plans', exact: true }).click();
  const tasksPanel = page.getByRole('complementary', { name: 'Plans', exact: true });
  await expect(tasksPanel).toBeVisible();
  await composer.fill('Tasks do not block chat');
  await tasksPanel.locator(`[data-plan-id="${plan.ID}"]`).getByRole('button', { name: 'Plan actions', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Edit', exact: true }).click();
  const editor = page.getByRole('dialog', { name: 'Edit', exact: true });
  await expect(editor).toBeVisible();
  await editor.getByRole('textbox', { name: 'What needs doing?', exact: true }).fill('Nested draft');
  await page.setViewportSize({ width: 1200, height: 1000 });
  await editor.getByRole('textbox', { name: 'What needs doing?', exact: true }).click();
  await expect(editor.getByRole('textbox', { name: 'What needs doing?', exact: true })).toHaveValue('Nested draft');
  await page.setViewportSize({ width: 390, height: 844 });
  await editor.getByRole('textbox', { name: 'What needs doing?', exact: true }).click();
  await expect(editor.getByRole('textbox', { name: 'What needs doing?', exact: true })).toHaveValue('Nested draft');
  await page.setViewportSize({ width: 1800, height: 1100 });
  await expect.poll(() => editor.evaluate(element => element.matches(':modal'))).toBe(true);
  await editor.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(tasksPanel).toBeVisible();
  await tasksPanel.locator(':scope > .ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click();
  await expect(tasksPanel).not.toBeVisible();
  await expect(composer).toHaveValue('Tasks do not block chat');
  expect(errors).toEqual([]);
});

test('chat sidebar animates layout both ways, reverses smoothly and respects reduced motion', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'Docked sidebar requires a wide workspace.');
  await page.setViewportSize({ width: 1800, height: 1100 });
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await login(page);
  const assistant = await seedAssistant(page, `Motion ${Date.now()}`);
  await page.goto(`/#/assistants/${assistant.ID}`);
  const trigger = page.getByRole('button', { name: `View information for ${assistant.Name}`, exact: true });
  const slot = page.locator('.chat-side-panel');
  const panel = page.getByRole('complementary', { name: 'Assistant information', exact: true });
  const pause = async () => {
    await page.waitForFunction(() => document.querySelector('.chat-side-panel')?.getAnimations().some(animation => animation.id === 'molr-chat-panel-size'));
    return slot.evaluate(element => {
      const animations = [...element.getAnimations(), ...Array.from(element.querySelectorAll('.ui-drawer')).flatMap(drawer => drawer.getAnimations())];
      for (const animation of animations) { animation.pause(); animation.currentTime = Number(animation.effect!.getTiming().duration) / 2; }
      return element.getBoundingClientRect().width;
    });
  };
  const finish = () => slot.evaluate(element => {
    for (const animation of [...element.getAnimations(), ...Array.from(element.querySelectorAll('.ui-drawer')).flatMap(drawer => drawer.getAnimations())]) animation.finish();
  });
  await trigger.click();
  const growing = await pause();
  expect(growing).toBeGreaterThan(0); expect(growing).toBeLessThan(600);
  await finish(); await expect(slot).toHaveCSS('flex-basis', '600px');
  await panel.locator(':scope > .ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click();
  const shrinking = await pause();
  expect(shrinking).toBeGreaterThan(0); expect(shrinking).toBeLessThan(600);
  await expect(slot).toHaveAttribute('data-state', 'closing');
  await expect(slot).toHaveAttribute('inert', '');
  // Reopen before the closing transition completes; do not remount a duplicate.
  await trigger.click();
  await expect(slot).toHaveAttribute('data-state', 'open');
  const start = await slot.evaluate(element => {
    const animation = element.getAnimations().find(animation => animation.id === 'molr-chat-panel-size');
    return parseFloat(String((animation?.effect as KeyframeEffect).getKeyframes()[0].flexBasis));
  });
  expect(Math.abs(start - shrinking)).toBeLessThan(2);
  await finish(); await expect(slot).toHaveCSS('flex-basis', '600px');
  await expect(panel.getByRole('textbox', { name: 'Assistant name', exact: true })).toHaveCount(1);
  await panel.locator(':scope > .ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click();
  await pause();
  const composer = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  await composer.fill('Keep focus while the sidebar closes');
  await finish(); await expect(slot).toHaveCount(0); await expect(composer).toBeFocused();
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await trigger.click(); await expect(panel).toBeVisible();
  await expect(slot).toHaveCSS('flex-basis', '600px');
  expect(await slot.evaluate(element => element.getAnimations().length)).toBe(0);
  await panel.locator(':scope > .ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click();
  await expect(slot).toHaveCount(0);
});

test('trajectory spans runs, pages older history and preserves inspection while live updates arrive', async ({ page }, info) => {
  test.setTimeout(120_000);
  await login(page);
  const before = await (await page.request.get('/api/settings')).json();
  const enable = async (value: boolean) => {
    const latest = await (await page.request.get('/api/settings')).json();
    await post(page, '/api/settings', { Settings: { ...latest, WorkbenchVisible: value }, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: latest.Version } }, false);
  };
  try {
    await enable(true);
    const assistant = await seedAssistant(page, `Trajectory ${info.project.name}-${Date.now()}`);
    const send = async (number: number) => {
      await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: `Trajectory message ${number}` } });
      await expect.poll(async () => {
        const runs = (await (await page.request.get(`/api/debug/runs?assistant_id=${assistant.ID}`)).json()).Items.filter((run: { Trigger: string }) => run.Trigger === 'user_message');
        return runs.length === number && runs.every((run: { State: string }) => run.State === 'succeeded');
      }, { timeout: 20000 }).toBe(true);
    };
    for (let i = 1; i <= 5; i++) await send(i);
    const payloadRequests: string[] = [];
    page.on('request', request => { if (/\/api\/debug\/runs\/[^/]+\/steps\//.test(request.url())) payloadRequests.push(request.url()); });
    await page.goto(`/#/workbench/${assistant.ID}`); await page.reload();
    const timeline = page.getByRole('region', { name: 'Execution timeline', exact: true });
    const scroll = timeline.locator('.trajectory-scroll');
    const follow = page.getByRole('switch', { name: 'Follow live execution', exact: true });
    await expect(page.getByRole('button', { name: 'Load earlier trajectory', exact: true })).toBeVisible();
    await expect.poll(() => scroll.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop < 2)).toBe(true);
    expect(payloadRequests).toHaveLength(0);
    const readingArea = await scroll.boundingBox();
    expect(readingArea!.height).toBeGreaterThan(info.project.name === 'mobile' ? 350 : 500);
    expect(readingArea!.y).toBeLessThan(380);
    await scroll.hover(); await page.mouse.wheel(0, -400);
    await expect(follow).not.toBeChecked();
    const anchor = await scroll.evaluate(el => {
      const top = el.getBoundingClientRect().top;
      const row = [...el.querySelectorAll<HTMLElement>('[data-record-id]')].find(row => row.getBoundingClientRect().top >= top);
      return { id: row!.dataset.recordId!, offset: row!.getBoundingClientRect().top - top };
    });
    // The number of debug steps per run can grow; exhaust the actual cursor
    // instead of assuming all five runs fit in exactly two pages.
    let olderCursor: string;
    do {
      const [response] = await Promise.all([
        page.waitForResponse(response => response.url().includes('/api/debug/trajectory?') && new URL(response.url()).searchParams.has('cursor')),
        page.getByRole('button', { name: 'Load earlier trajectory', exact: true }).click(),
      ]);
      olderCursor = (await response.json()).NextCursor;
    } while (olderCursor);
    await expect(page.getByRole('button', { name: 'Load earlier trajectory', exact: true })).toHaveCount(0);
    await expect.poll(() => scroll.evaluate((el, anchor) => {
      const row = [...el.querySelectorAll<HTMLElement>('[data-record-id]')].find(row => row.dataset.recordId === anchor.id);
      return row ? Math.abs(row.getBoundingClientRect().top - el.getBoundingClientRect().top - anchor.offset) : 999;
    }, anchor)).toBeLessThan(3);
    expect(await timeline.getByRole('listitem').count()).toBeLessThan(30);
    const search = page.getByRole('searchbox', { name: 'Search loaded trajectory', exact: true });
    await search.fill('Trajectory message 5');
    await expect(timeline.locator('.trajectory-trigger-copy')).toHaveText(['Trajectory message 5']);
    await scroll.evaluate(el => { el.scrollTop = 0; });
    const inspector = page.getByRole('region', { name: 'Step details', exact: true });
    const axis = page.getByRole('region', { name: 'Time overview', exact: true });
    const [axisBounds, trackBounds] = await readLayout(axis, axis.locator('.trajectory-time-track'));
    const expectStableAxis = async () => {
      await expect(axis).toBeVisible();
      const [currentAxis, currentTrack] = await readLayout(axis, axis.locator('.trajectory-time-track'));
      expect(currentAxis).toEqual(axisBounds);
      expect(currentTrack).toEqual(trackBounds);
    };
    await timeline.locator('.trajectory-run-bar').getByRole('button', { name: 'Inspect full details', exact: true }).click();
    await expect(inspector).toContainText('Trajectory message 5');
    await expect(inspector).toContainText('Execution snapshot');
    await expectStableAxis();
    const [currentAxis, inspectorBounds] = await readLayout(axis, inspector);
    expect(inspectorBounds.y).toBeGreaterThanOrEqual(currentAxis.y + currentAxis.height);
    await axis.getByRole('button', { name: 'Event axis', exact: true }).click();
    await expect(axis).toHaveAttribute('data-axis-mode', 'event');
    await axis.getByRole('button', { name: 'Duration axis', exact: true }).click();
    await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    await expectStableAxis();
    await timeline.locator('.trajectory-record--model').first().getByRole('button', { name: 'Inspect full details', exact: true }).click();
    await expect(inspector).toContainText('Internal reasoning is not a chat message.');
    await expect(inspector).toContainText('conversation_read');
    await expectStableAxis();
    await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    await timeline.locator('.trajectory-record--tool').first().getByRole('button', { name: 'Inspect full details', exact: true }).click();
    await expect(inspector).toContainText('conversation_id');
    // conversation_read returns the most recent five messages in this run.
    await expect(inspector).toContainText('Trajectory message 5');
    await expect(inspector.getByRole('heading', { name: 'Input', exact: true })).toBeVisible();
    await expect(inspector.locator('.ui-code-token--property').filter({ hasText: '"conversation_id"' }).first()).toBeVisible();
    await expect(inspector.getByRole('heading', { name: 'Output', exact: true })).toBeVisible();
    await expectStableAxis();
    await axis.locator('.trajectory-time-bar--model').last().click();
    await expect(inspector).toContainText('Internal reasoning is not a chat message.');
    await expectStableAxis();
    await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    await expectStableAxis();

    await page.getByRole('searchbox', { name: 'Search loaded trajectory', exact: true }).fill('conversation_publish');
    await expect(page.getByRole('region', { name: 'Time overview' }).locator('.trajectory-time-bar--model')).toHaveCount(18);
    await timeline.locator('.trajectory-record--tool .trajectory-record-body').first().click();
    await expect(inspector).toContainText('conversation_id');
    await expect(inspector).toContainText('Runtime delivered this reply');
    await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    await page.getByRole('searchbox', { name: 'Search loaded trajectory', exact: true }).fill('');
    await scroll.evaluate(el => { el.scrollTop = 0; });
    await expect(follow).not.toBeChecked();
    const topBefore = await scroll.evaluate(el => el.scrollTop);
    await send(6);
    await expect(page.getByRole('region', { name: 'Time overview' }).locator('.trajectory-time-bar--model')).toHaveCount(21);
    await expect.poll(() => scroll.evaluate(el => el.scrollTop)).toBe(topBefore);
    await follow.click();
    await expect.poll(() => scroll.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop < 2)).toBe(true);
    await page.locator('.trajectory-range-controls > summary').click();
    await page.getByRole('slider', { name: 'Time range start', exact: true }).focus();
    await page.keyboard.press('End');
    await expect(timeline.locator('.trajectory-record--model')).toHaveCount(1);
    await expect(timeline.locator('.trajectory-record--tool')).toHaveCount(0);
    await page.getByRole('button', { name: 'Reset time range', exact: true }).click();
    await page.locator('.trajectory-range-controls > summary').click();
    await page.getByRole('region', { name: 'Time overview' }).locator('.trajectory-time-bar--model').last().click();
    await expect(inspector).toContainText('Internal reasoning is not a chat message.');
    await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    await page.getByRole('button', { name: 'Zoom in timeline', exact: true }).click();
    await page.getByRole('button', { name: 'Reset time range', exact: true }).click();
    const overview = page.getByRole('region', { name: 'Time overview', exact: true });
    await expect(overview.locator('.trajectory-time-bar--trigger')).toHaveCount(7);
    await overview.locator('.trajectory-time-bar--trigger').last().click();
    await expect(inspector.getByRole('heading', { name: 'Trigger and configuration snapshot', exact: true })).toBeVisible();
    await expect(inspector).toContainText('Trajectory message 6');
    await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    await overview.getByRole('button', { name: 'Event axis', exact: true }).click();
    await expect(overview).toHaveAttribute('data-axis-mode', 'event');
    await expect(overview.getByRole('button', { name: 'Event axis', exact: true })).toBeFocused();
    const widths = await overview.locator('.trajectory-time-bar').evaluateAll(nodes => nodes.map(node => node.getBoundingClientRect().width));
    expect(Math.max(...widths) - Math.min(...widths)).toBeLessThan(1);
    await expect(overview.locator('.trajectory-time-bar')).toHaveCount(42);
    await page.locator('.trajectory-range-controls > summary').click();
    const rangeControl = overview.getByRole('group', { name: 'Event range', exact: true });
    await expect(rangeControl.locator('.ui-range-slider__rail')).toHaveCount(1);
    const firstEvent = rangeControl.getByRole('slider', { name: 'First event', exact: true });
    const lastEvent = rangeControl.getByRole('slider', { name: 'Last event', exact: true });
    await lastEvent.focus(); await page.keyboard.press('Home');
    await expect(timeline.locator('.trajectory-run')).toHaveCount(1);
    await expect(timeline.locator('.trajectory-record')).toHaveCount(0);
    await lastEvent.focus(); await page.keyboard.press('End');
    await firstEvent.focus(); await page.keyboard.press('End');
    await expect(timeline.locator('.trajectory-record--model')).toHaveCount(1);
    const selectedLast = await lastEvent.getAttribute('aria-valuenow');
    await send(7);
    await expect(overview.locator('.trajectory-time-bar--trigger')).toHaveCount(8);
    await expect(lastEvent).toHaveAttribute('aria-valuenow', selectedLast!);
    await expect(timeline.locator('.trajectory-record--model')).toHaveCount(1);
    await overview.getByRole('button', { name: 'Duration axis', exact: true }).click();
    await expect(overview).toHaveAttribute('data-axis-mode', 'time');
    await page.getByRole('switch', { name: 'Show all events', exact: true }).click();
    await page.getByRole('searchbox', { name: 'Search loaded trajectory', exact: true }).fill('Actual model request');
    await expect(timeline.locator('.trajectory-record--event').first()).toContainText('Actual model request');
    if (process.env.MOLR_CAPTURE_WORKBENCH) {
      await page.getByRole('searchbox', { name: 'Search loaded trajectory', exact: true }).fill('');
      await page.getByRole('switch', { name: 'Show all events', exact: true }).click();
      await follow.click();
      await page.screenshot({ path: `/tmp/molr-trajectory-${info.project.name}.png`, fullPage: true });
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  } finally { await enable(before.WorkbenchVisible ?? false); }
});


test('trajectory groups multiple tools under one model and only parents have fold controls', async ({ page }, info) => {
  await login(page);
  const before = await (await page.request.get('/api/settings')).json();
  const enable = async (value: boolean) => {
    const latest = await (await page.request.get('/api/settings')).json();
    await post(page, '/api/settings', { Settings: { ...latest, WorkbenchVisible: value }, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: latest.Version } }, false);
  };
  try {
    await enable(true);
    const assistant = await seedAssistant(page, `Multi-tool trajectory ${info.project.name}-${Date.now()}`);
    await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).State, { timeout: 30_000 }).toBe('idle');
    await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'Exercise three tool calls from one model response.' } });
    const userRun = async () => (await (await page.request.get(`/api/debug/runs?assistant_id=${assistant.ID}`)).json()).Items.find((run: { Trigger: string }) => run.Trigger === 'user_message');
    await expect.poll(async () => (await userRun())?.State, { timeout: 30_000 }).toBe('succeeded');
    const runID = (await userRun()).ID;
    await page.goto(`/#/workbench/${assistant.ID}`); await page.reload();
    const timeline = page.getByRole('region', { name: 'Execution timeline', exact: true });
    const rows = timeline.locator(`[data-record-id^="${runID}:"]`);
    const models = rows.locator('.trajectory-record--model');
    const tools = rows.locator('.trajectory-record--tool');
    await expect(models).toHaveCount(2);
    await expect(tools).toHaveCount(3);
    await expect(models.first().locator('.trajectory-fold')).toHaveCount(1);
    await expect(models.last().locator('.trajectory-fold')).toHaveCount(0);
    await expect(tools.nth(0).locator('.trajectory-preview')).toHaveText('Read 1 chat message');
    // The born reply and user message both precede this second read.
    await expect(tools.nth(1).locator('.trajectory-preview')).toHaveText('Read 2 chat messages');
    await expect(tools.nth(2).locator('.trajectory-preview')).toHaveText('Reply after multiple tool calls.');
    await expect(tools.nth(2).locator('.trajectory-preview')).toHaveAttribute('title', 'Reply after multiple tool calls.');
    const summarySearch = page.getByRole('searchbox', { name: 'Search loaded trajectory', exact: true });
    await summarySearch.fill('Read 1 chat message');
    await expect(tools).toHaveCount(1);
    await summarySearch.fill('');
    await expect(tools).toHaveCount(3);
    expect(await rows.locator('.trajectory-record').evaluateAll(rows => rows.map(row => row.classList.contains('trajectory-record--model') ? 'model' : 'tool'))).toEqual(['model', 'tool', 'tool', 'tool', 'model']);
    await models.first().locator('.trajectory-fold').click();
    await expect(tools).toHaveCount(0);
    await expect(models).toHaveCount(2);
    await expect(models.first().locator('.trajectory-fold')).toHaveAttribute('aria-expanded', 'false');
    await models.first().locator('.trajectory-fold').click();
    await expect(tools).toHaveCount(3);
    const inspector = page.getByRole('region', { name: 'Step details', exact: true });
    for (let index = 0; index < 3; index++) {
      await tools.nth(index).locator('.trajectory-record-body').click();
      await inspector.getByRole('tab', { name: 'Overview', exact: true }).click();
      await expect(inspector).toContainText(index < 2 ? `"limit": ${index + 1}` : 'Reply after multiple tool calls.');
      await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    }
    await page.getByRole('button', { name: 'Event axis', exact: true }).click();
    const overview = page.getByRole('region', { name: 'Time overview', exact: true });
    await expect(overview.locator(`.trajectory-time-bar--tool[data-axis-id^="${runID}:"]`)).toHaveCount(3);
    await overview.locator(`.trajectory-time-bar--tool[data-axis-id^="${runID}:"]`).nth(1).click();
    await inspector.getByRole('tab', { name: 'Overview', exact: true }).click();
    await expect(inspector).toContainText('"limit": 2');
  } finally { await enable(before.WorkbenchVisible ?? false); }
});

test('clipboard images upload to Runtime, persist as thumbnails and open a multi-image gallery', async ({ page }, info) => {
  await login(page);
  const name = `Image gallery ${info.project.name}-${Date.now()}`;
  const assistant = await seedAssistant(page, name);
  await page.reload();
  await page.getByRole('button', { name, exact: true }).click();
  const input = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  await input.evaluate(element => {
    const clipboardData = new DataTransfer();
    for (const [name, color] of [['landscape.svg', 'seagreen'], ['portrait.svg', 'coral']]) clipboardData.items.add(new File([`<svg xmlns="http://www.w3.org/2000/svg" width="240" height="160"><rect width="240" height="160" fill="${color}"/></svg>`], name, { type: 'image/svg+xml' }));
    element.dispatchEvent(new ClipboardEvent('paste', { clipboardData, bubbles: true, cancelable: true }));
  });
  const draft = page.locator('.composer-attachments');
  await expect(draft.locator('.ui-image-gallery__thumbnail img')).toHaveCount(2);
  for (const image of await draft.locator('img').all()) await expect(image).toHaveJSProperty('naturalWidth', 240);
  await expect(input).toHaveValue('');
  await page.locator('.chat-input input[type=file]').setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('gallery attachment') });
  await expect(draft).toContainText('notes.txt');
  await expect(input).toBeEnabled();
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const message = page.locator('.chat-history .ui-chat-message--user').last();
  await expect(message.locator('.ui-image-gallery__thumbnail')).toHaveCount(2);
  const trigger = message.getByRole('button', { name: 'Preview image portrait.svg', exact: true });
  await trigger.click();
  const gallery = page.getByRole('dialog', { name: 'Image preview', exact: true });
  await expect(gallery.getByRole('status')).toHaveText('2 / 2');
  await expect(gallery.locator('.ui-image-gallery__stage img')).toHaveJSProperty('naturalWidth', 240);
  await gallery.getByRole('button', { name: 'Previous image', exact: true }).click();
  await expect(gallery.getByRole('status')).toHaveText('1 / 2');
  await page.keyboard.press('ArrowRight');
  await expect(gallery.getByRole('status')).toHaveText('2 / 2');
  await gallery.screenshot({ path: `/tmp/molr-runtime-gallery-${info.project.name}.png` });
  await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused();
  const download = page.waitForEvent('download');
  await message.getByRole('link', { name: 'notes.txt', exact: true }).click();
  expect((await download).suggestedFilename()).toBe('notes.txt');
  const saved = await (await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json();
  expect(saved.Items.find((row: any) => row.Sender === 'user').Content.AttachmentIDs).toHaveLength(3);
  await page.reload();
  await expect(message.locator('.ui-image-gallery__thumbnail img')).toHaveCount(2);
  await expect(message.locator('.ui-image-gallery__thumbnail img').first()).toHaveJSProperty('naturalWidth', 240);
  await message.screenshot({ path: `/tmp/molr-runtime-images-${info.project.name}.png` });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('real chat renders Markdown and LaTeX and copies stored source across reload and archive', async ({ page, context }, info) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await login(page);
  const assistant = await seedAssistant(page, `Rich text ${info.project.name}-${Date.now()}`);
  await page.goto(`/#/assistants/${assistant.ID}`);
  const raw = '## My original request\n\n**中文 and English** with $x^2$.\n\n```text\n' + 'wide_code_'.repeat(40) + '\n```\n\n[unsafe](javascript:alert(1))\n\n<script>window.richTextExecuted = true</script>\n';
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill(raw);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const user = page.locator('.chat-history .ui-chat-message--user').last();
  const latestReply = page.locator('.chat-history .ui-chat-message--assistant').last();
  await expect(user.getByRole('heading', { name: 'My original request', exact: true })).toBeVisible();
  await expect(user.locator('script, a[href^="javascript:"]')).toHaveCount(0);
  expect(await page.evaluate(() => Reflect.get(window, 'richTextExecuted'))).toBeUndefined();
  await expect(latestReply.getByRole('heading', { name: 'Runtime rich text reply', exact: true })).toBeVisible({ timeout: 20_000 });
  // The initial greeting and request reply can arrive separately; keep one stable target.
  const replyID = await latestReply.evaluate(element => element.closest<HTMLElement>('[data-message-id]')!.dataset.messageId!);
  const reply = page.locator(`[data-message-id="${replyID}"] .ui-chat-message--assistant`);
  await expect(reply.locator('table')).toHaveCount(1);
  await expect(reply.locator('.katex')).toHaveCount(2);
  await expect(reply.locator('.katex-display')).toHaveCount(1);
  await expect(reply.locator('pre code')).toHaveText('total = 6 / 4 + 0.5\n');
  const stored = (await (await page.request.get(`/api/conversations/${assistant.ConversationID}/messages?order=desc&limit=50`)).json()).Items;
  expect(stored.find((message: { Sender: string }) => message.Sender === 'user').Content.Text).toBe(raw);
  const replySource = stored.find((message: { ID: string }) => message.ID === replyID).Content.Text;
  await user.getByRole('button', { name: 'Copy original text', exact: true }).click();
  await expect(user.getByRole('button', { name: 'Copied', exact: true })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(raw);
  await reply.getByRole('button', { name: 'Copy original text', exact: true }).click();
  await expect(reply.getByRole('button', { name: 'Copied', exact: true })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(replySource);
  const feedbackSaved = page.waitForResponse(response => response.url().endsWith('/api/conversations/feedback') && response.request().method() === 'POST' && response.status() === 200);
  await reply.getByRole('button', { name: 'Like', exact: true }).click();
  await expect(reply.getByRole('button', { name: 'Like', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await feedbackSaved;
  await page.reload();
  await expect(reply.locator('.katex-display')).toBeVisible();
  await expect(reply.getByRole('button', { name: 'Like', exact: true })).toHaveAttribute('aria-pressed', 'true');
  // LAN HTTP has no Clipboard API; exercise the same fallback on real history.
  await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined }));
  await reply.getByRole('button', { name: 'Copy original text', exact: true }).click();
  await expect(reply.getByRole('button', { name: 'Copied', exact: true })).toBeVisible();
  await page.evaluate(() => Reflect.deleteProperty(navigator, 'clipboard'));
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(replySource);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await page.locator('.chat-history').evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
  await page.screenshot({ path: info.outputPath('rich-text.png') });
  await assistantMenu(page, 'Dismiss assistant');
  const dismiss = page.getByRole('dialog', { name: 'Dismiss assistant', exact: true });
  await dismiss.getByRole('button', { name: 'Confirm', exact: true }).click();
  await expect(dismiss).not.toBeVisible();
  // The dismissal toast overlaps the bottom navigation on mobile.
  for (const close of await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).all()) await close.click();
  await openUserSettings(page);
  await openSettingsSection(page, 'Former assistants');
  const former = settingsPane(page).locator('.settings-row-item').filter({ hasText: assistant.Name });
  await former.getByRole('button', { name: 'Assistant conversation', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toBeDisabled();
  await reply.getByRole('button', { name: 'Copy original text', exact: true }).click();
  await expect(reply.getByRole('button', { name: 'Copied', exact: true })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(replySource);
});

test('chat refresh appends sequence deltas across pages without refetching loaded history', async ({ page }, info) => {
  test.setTimeout(120_000);
  await login(page);
  const name = `Incremental ${info.project.name}-${Date.now()}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: modelOrigin, AllowedOrigins: [modelOrigin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 0 } }] } });
  const assistant = await post(page, '/api/assistants', { Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' }, Name: name, ModelID: (await (await page.request.get('/api/models')).json()).find((model: { ResourceID: string }) => model.ResourceID === resource.ID).ID });
  const path = `/api/conversations/${assistant.ConversationID}/messages`;
  const send = (n: number) => post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: `Delta ${n}` } });
  for (let n = 1; n <= 65; n++) await send(n);
  await page.goto(`/#/assistants/${assistant.ID}`);
  const log = page.getByRole('log');
  await expect(log.getByText('Delta 65', { exact: true })).toBeVisible();
  const requests: URL[] = [];
  page.on('request', request => { const url = new URL(request.url()); if (url.pathname === path && url.searchParams.get('limit') === '50') requests.push(url); });
  await page.getByRole('button', { name: 'Load earlier messages', exact: true }).click();
  await expect(log.getByText('Delta 1', { exact: true })).toBeVisible();
  expect(requests.filter(url => url.searchParams.get('order') === 'desc')).toHaveLength(1);
  expect(requests.find(url => url.searchParams.get('order') === 'desc')!.searchParams.get('cursor')).toBeTruthy();
  requests.length = 0;

  // Hold a real, already-fetched response while SSE keeps invalidating it.
  // It must complete, then drain all newer pages without gaps or duplicates.
  let release!: () => void, started!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const held = new Promise<void>(resolve => { started = resolve; });
  let once = false;
  await page.route(`**${path}?*`, async route => {
    if (once || !new URL(route.request().url()).searchParams.has('after_sequence')) return route.continue();
    once = true;
    const response = await route.fetch(); started();
    await gate;
    await route.fulfill({ response });
  });
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  await held;
  try { for (let n = 66; n <= 130; n++) await send(n); } finally { release(); }
  await expect(log.getByText('Delta 130', { exact: true })).toHaveCount(1);
  await expect(log.locator('.ui-chat-message--user')).toHaveCount(130);
  expect(requests.every(url => url.searchParams.has('after_sequence') || url.searchParams.has('message_id'))).toBe(true);
  expect(requests.some(url => url.searchParams.get('after_sequence') === '65' && !!url.searchParams.get('cursor'))).toBe(true);
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  await expect.poll(() => requests.some(url => url.searchParams.get('after_sequence') === '130')).toBe(true);
  await expect(log.getByText('Delta 1', { exact: true })).toBeVisible();
  const unchanged = await (await page.request.get(`${path}?after_sequence=130`)).json();
  expect(unchanged.Items ?? []).toEqual([]);
  for (const invalid of ['-1', 'abc', '', '18446744073709551616']) {
    expect((await page.request.get(`${path}?after_sequence=${invalid}`)).status()).toBe(400);
  }
});

test('sandbox access grants can be created edited and revoked from the resource', async ({ page }, info) => {
  test.setTimeout(90000);
  const closeToasts = async () => { for (const close of await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).all()) await close.click(); };
  const directory = await mkdtemp(join(tmpdir(), 'molr-sandbox-access-'));
  try {
    await login(page);
    const name = `Sandbox access ${info.project.name} ${Date.now()}`;
    const assistant = await seedAssistant(page, name);
    const resource = await post(page, '/api/resources', { Name: name, Kind: 'storage', Provider: 'local-directory', Binding: { RootPath: directory } });
    await page.goto(`/#/resources/${resource.ID}`);
    const card = page.locator('.sandbox-resource').filter({ has: page.getByRole('heading', { name, exact: true }) });
    await card.getByRole('button', { name: 'Grant assistant access', exact: true }).click();
    const access = page.getByRole('dialog', { name: `Manage access · ${name}`, exact: true });
    await access.getByRole('button', { name: 'Grant assistant access', exact: true }).click();
    const grant = page.getByRole('dialog', { name: 'Grant assistant access', exact: true });
    await grant.getByRole('combobox', { name: 'Assistant', exact: true }).click();
    const search = page.getByRole('searchbox');
    if (await search.isVisible()) await search.fill(name);
    await page.getByRole('option', { name, exact: true }).click();
    await grant.getByRole('textbox', { name: 'Allowed path prefixes (one per line)', exact: true }).fill('/read');
    await grant.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(grant).not.toBeVisible();
    await expect(access).toContainText('Authorized assistants · 1');
    await expect(access).toContainText('/read');
    const grants = () => page.request.get(`/api/grants?resource_id=${resource.ID}`).then(r => r.json());
    await expect.poll(async () => (await grants()).Items.filter((g: any) => !g.RevokedAt).length).toBe(1);
    expect((await grants()).Items[0]).toMatchObject({ AssistantID: assistant.ID, Scope: { PathPrefixes: ['/read'] } });
    await closeToasts();
    await access.getByRole('button', { name: 'Edit', exact: true }).click();
    const edit = page.getByRole('dialog', { name: 'Edit permission', exact: true });
    await edit.getByRole('textbox', { name: 'Allowed path prefixes (one per line)', exact: true }).fill('/write');
    await edit.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(edit).not.toBeVisible();
    await expect(access).toContainText('/write');
    await closeToasts();
    await access.getByRole('button', { name: 'Revoke permission', exact: true }).click();
    const revoke = page.getByRole('dialog', { name: 'Revoke permission', exact: true });
    await revoke.getByRole('button', { name: 'Confirm', exact: true }).click();
    await expect(revoke).not.toBeVisible();
    await expect(access).toContainText('No assistants authorized');
    await expect.poll(async () => (await grants()).Items.filter((g: any) => !g.RevokedAt).length).toBe(0);
    await page.keyboard.press('Escape');
    const compute = (await (await page.request.get('/api/resources?kind=compute')).json()).Items.find((r: any) => r.State === 'available');
    await page.goto(`/#/resources/${compute.ID}`);
    const computeCard = page.locator(`[data-resource-id="${compute.ID}"]`);
    await computeCard.getByRole('button', { name: /Authorized assistants|Grant assistant access/ }).click();
    const computeAccess = page.getByRole('dialog', { name: `Manage access · ${compute.Name}`, exact: true });
    await computeAccess.getByRole('button', { name: 'Grant assistant access', exact: true }).click();
    await grant.getByRole('combobox', { name: 'Assistant', exact: true }).click();
    if (await search.isVisible()) await search.fill(name);
    await page.getByRole('option', { name, exact: true }).click();
    await expect(grant.getByRole('textbox', { name: 'Allowed path prefixes (one per line)', exact: true })).toHaveCount(0);
    await grant.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(grant).not.toBeVisible();
    const computeGrants = (await (await page.request.get(`/api/grants?resource_id=${compute.ID}`)).json()).Items;
    expect(computeGrants.find((g: any) => g.AssistantID === assistant.ID && !g.RevokedAt)).toMatchObject({ Actions: ['execute'], Scope: { ReadOnly: false } });
    const capabilities = await (await page.request.get(`/api/assistants/${assistant.ID}/capabilities`)).json();
    expect(capabilities.Tools.find((tool: any) => tool.Name === 'shell_execute').ResourceIDs).toContain(compute.ID);

  } finally { await rm(directory, { recursive: true, force: true }); }
});

test('sandbox selection persists and connection changes invalidate a successful test', async ({ page }) => {
  await login(page);
  await page.goto('/#/resources');
  await resourceCategory(page, 'Sandboxes').click();
  await resourceCategory(page, 'Network').click();
  await page.reload();
  await expect(resourceCategory(page, 'Sandboxes')).toHaveAttribute('aria-selected', 'true');
  await expect(resourceCategory(page, 'Network')).toHaveAttribute('aria-selected', 'true');
  await page.getByRole('navigation').getByRole('button', { name: 'Assistants', exact: true }).click();
  await page.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  await expect(resourceCategory(page, 'Network')).toHaveAttribute('aria-selected', 'true');
  await page.getByRole('button', { name: 'Connect sandbox', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect sandbox', exact: true });
  const save = dialog.getByRole('button', { name: 'Connect sandbox', exact: true });
  await expect(save).toBeDisabled();
  await dialog.getByLabel('Name', { exact: true }).fill('Test sandbox');
  await dialog.getByLabel('Service URL', { exact: true }).fill('https://sandbox.invalid');
  await dialog.getByLabel('Connection code', { exact: true }).fill('bad-pin');
  await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(dialog.getByLabel('Connection code', { exact: true })).toHaveAttribute('aria-invalid', 'true');
  await expect(save).toBeDisabled();
  const info = (await (await page.request.get('/api/sandboxes')).json())[0].Info;
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/sandboxes/test', async route => { await gate; await route.fulfill({ json: { Connected: true, Info: info, CheckedAt: new Date().toISOString() } }); });
  await dialog.getByLabel('Connection code', { exact: true }).fill('a'.repeat(64));
  await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
  await dialog.getByLabel('Connection code', { exact: true }).fill('y'.repeat(40));
  release();
  await expect(dialog.getByRole('button', { name: 'Test connection', exact: true })).toBeEnabled();
  await expect(save).toBeDisabled(); // The in-flight response checked the old token.
  await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(save).toBeEnabled();
  await dialog.locator('.ui-alert--positive').getByRole('button', { name: 'Close', exact: true }).click();
  await expect(dialog.locator('.ui-alert--positive')).toHaveCount(0);
  await expect(save).toBeEnabled(); // Dismissing feedback does not invalidate the tested credentials.
  await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(dialog.locator('.ui-alert--positive')).toBeVisible();
  await dialog.getByLabel('Connection code', { exact: true }).fill('z'.repeat(40));
  await expect(save).toBeDisabled();
});

for (const [preset, name, endpoint] of [
  ['vllm', 'vLLM', 'http://127.0.0.1:8000/v1'],
  ['sglang', 'SGLang', 'http://127.0.0.1:30000/v1'],
]) test(`${name} preset connects, discovers models and preserves connection when edited`, async ({ page }, info) => {
  test.setTimeout(120_000);
  await login(page);
  await page.goto('/#/resources');
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
  await dialog.getByRole('radio', { name: 'Local', exact: true }).check();
  await dialog.getByRole('searchbox').fill(name.toLowerCase());
  await dialog.getByRole('button', { name, exact: true }).click();
  const form = dialog.getByRole('form', { name: 'Provider configuration' });
  await expect(form).toContainText(`Connect to ${name} on the Runtime device`);
  await expect(form.getByRole('combobox', { name: 'Authentication' })).toContainText('No authentication');
  await expect(form.getByLabel('API key', { exact: true })).toHaveCount(0);
  await form.getByText('More configuration', { exact: true }).click();
  await expect(form.getByRole('textbox', { name: 'Service URL' })).toHaveValue(endpoint);
  const resourceName = `${name} ${info.project.name} ${Date.now()}`;
  await form.getByRole('textbox', { name: 'Name', exact: true }).fill(resourceName);
  const useKey = preset === 'sglang';
  const url = `${modelOrigin}${useKey ? '/private' : ''}/v1`;
  await form.getByRole('textbox', { name: 'Service URL' }).fill(url);
  if (useKey) {
    await choose(page, 'Authentication', 'API key');
    await form.getByLabel('API key', { exact: true }).fill('test-provider-secret');
  }
  await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(dialog.getByText('Connected · 2 models detected', { exact: true })).toBeVisible();
  await dialog.locator('.ui-alert--positive').getByRole('button', { name: 'Close', exact: true }).click();
  await expect(dialog.locator('.ui-alert--positive')).toHaveCount(0);
  const saved = page.waitForResponse(response => response.url().endsWith('/api/resources/provider') && response.request().method() === 'POST');
  await dialog.getByRole('button', { name: 'Connect provider', exact: true }).click();
  const response = await saved;
  expect(response.ok(), await response.text()).toBe(true);
  const resource = await response.json();
  await expect(dialog).not.toBeVisible();
  const card = resourceRow(page, resourceName);
  await openProviderModels(card);
  await expect(page.locator('.provider-models-page').first()).toContainText('discovered-one');
  const connection = await (await page.request.get(`/api/resources/${resource.ID}/connection`)).json();
  expect(connection.PricingProvider).toBe(preset);
  expect(Boolean(connection.CredentialID)).toBe(useKey);
  await page.reload();
  await cardAction(page, card, 'Edit provider');
  const editor = page.getByRole('dialog', { name: 'Edit provider', exact: true });
  await expect(editor.getByRole('textbox', { name: 'Service URL' })).toHaveValue(url);
  await expect(editor).toContainText(`Connect to ${name} on the Runtime device`);
  await editor.getByRole('textbox', { name: 'Name', exact: true }).fill(`${resourceName} edited`);
  await editor.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(editor).not.toBeVisible();
  await expect(resourceRow(page, `${resourceName} edited`)).toBeVisible();
  const current = await (await page.request.get(`/api/resources/${resource.ID}`)).json();
  await post(page, '/api/resources/configure-model', { ID: resource.ID, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: current.Version }, Model: { Name: 'discovered-one', SupportsTools: true, ContextLimit: 65536, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } } }, false);
  const models = await (await page.request.get('/api/models')).json();
  const model = models.find((model: { ResourceID: string; Name: string }) => model.ResourceID === resource.ID && model.Name === 'discovered-one');
  const assistant = await post(page, '/api/assistants', { Name: resourceName, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Check this self-hosted provider.');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.getByRole('log', { name: 'Assistant conversation' }).getByText('Runtime delivered this reply through conversation_publish.', { exact: true }).last()).toBeVisible({ timeout: 60_000 });
});

test('provider network rejection explains test and save without clearing the draft', async ({ page }) => {
  await login(page);
  await page.goto('/#/resources');
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
  await dialog.getByRole('button', { name: 'vLLM', exact: true }).click();
  await dialog.getByText('More configuration', { exact: true }).click();
  const endpoint = dialog.getByRole('textbox', { name: 'Service URL' });
  await endpoint.fill('http://192.168.50.20:8000/v1');
  for (const [action, path] of [['Test connection', 'test-connection'], ['Connect provider', 'provider']]) {
    const response = page.waitForResponse(response => response.url().endsWith(`/api/resources/${path}`) && response.request().method() === 'POST');
    await dialog.getByRole('button', { name: action, exact: true }).click();
    expect(await (await response).json()).toMatchObject({ code: 'invalid_argument', reason: 'network_private_address_denied' });
    await expect(dialog.getByText(/Runtime does not allow this private or loopback IP/)).toBeVisible();
    await expect(endpoint).toHaveValue('http://192.168.50.20:8000/v1');
    await dialog.locator('.ui-toast').getByRole('button', { name: 'Close', exact: true }).click();
  }
  await endpoint.fill(`${modelOrigin}/v1`);
  await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(dialog.getByText('Connected · 2 models detected', { exact: true })).toBeVisible();
});


test('OpenRouter preset discovers metadata and preserves streamed reasoning through tool calls', async ({ page }, info) => {
  test.setTimeout(120_000);
  await login(page);
  await page.goto('/#/resources');
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
  await dialog.getByRole('radio', { name: 'Cloud', exact: true }).check();
  await dialog.getByRole('searchbox').fill('openrouter');
  await dialog.getByRole('button', { name: 'OpenRouter', exact: true }).click();
  await dialog.getByText('More configuration', { exact: true }).click();
  await expect(dialog.getByRole('textbox', { name: 'Service URL' })).toHaveValue('https://openrouter.ai/api/v1');
  const name = `OpenRouter ${info.project.name} ${Date.now()}`;
  const endpoint = `${modelOrigin}/openrouter/api/v1`;
  await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
  await dialog.getByRole('textbox', { name: 'Service URL' }).fill(endpoint);
  await dialog.getByLabel('API key', { exact: true }).fill('test-router-key');
  await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(dialog.getByText('Connected · 1 models detected', { exact: true })).toBeVisible();
  const saved = page.waitForResponse(response => response.url().endsWith('/api/resources/provider') && response.request().method() === 'POST');
  await dialog.getByRole('button', { name: 'Connect provider', exact: true }).click();
  const resource = await (await saved).json();
  await expect(dialog).not.toBeVisible();
  const card = resourceRow(page, name);
  await openProviderModels(card);
  await expect(page.locator('.provider-models-page').first()).toContainText('fixture/router');
  const models = await (await page.request.get('/api/models')).json();
  const model = models.find((item: { ResourceID: string }) => item.ResourceID === resource.ID);
  expect(model).toMatchObject({ SupportsTools: true, SupportsImages: true, EffectiveContextLimit: 131072, EffectiveOutputLimit: 8192, PricingInfo: { Source: 'provider' } });
  await cardAction(page, card, 'Edit provider');
  const editor = page.getByRole('dialog', { name: 'Edit provider', exact: true });
  await expect(editor.getByRole('textbox', { name: 'Service URL' })).toHaveValue(endpoint);
  await editor.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(editor).not.toBeVisible();
  const connection = await (await page.request.get(`/api/resources/${resource.ID}/connection`)).json();
  expect(connection.PricingProvider).toBe('openrouter');
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  const dismiss = page.locator('.ui-toasts').getByRole('button', { name: 'Close', exact: true });
  while (await dismiss.count()) await dismiss.first().click();
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Check OpenRouter tool continuation.');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const log = page.getByRole('log', { name: 'Assistant conversation' });
  await expect(log.getByText('Runtime delivered this reply through conversation_publish.', { exact: true }).last()).toBeVisible({ timeout: 60_000 });
  await expect(log).not.toContainText('Private continuation.');
});
