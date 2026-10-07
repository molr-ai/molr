import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('molr-ui-design-locale', 'en'));
  await page.goto('/?components=chat#components');
});

const sample = '.chat-rich-text__preview';

test('renders Markdown and math and copies the exact source with keyboard access', async ({ page, context }, info) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  const source = page.getByRole('textbox', { name: 'Edit original text' });
  const raw = await source.inputValue();
  const preview = page.locator(sample);
  await expect(preview.getByRole('heading', { name: 'A relaxed walking plan' })).toBeVisible();
  await expect(preview.locator('table')).toHaveCount(1);
  await expect(preview.locator('pre code')).toContainText('distance = 6');
  await expect(preview.locator('.katex')).toHaveCount(3);
  await expect(preview.locator('.katex-display')).toHaveCount(1);
  const copy = preview.getByRole('button', { name: 'Copy original text' });
  await copy.focus();
  await page.keyboard.press('Enter');
  await expect(preview.getByRole('button', { name: 'Copied', exact: true })).toBeFocused();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(raw);
  await expect(preview.locator('.ui-chat-bubble button')).toHaveCount(0);
  await page.getByRole('radio', { name: 'Markdown', exact: true }).check();
  await expect(preview.locator('blockquote')).toBeVisible();
  await expect(preview.locator('input[type=checkbox]')).toHaveCount(2);
  await expect(preview.locator('del')).toHaveText('fill every hour');
  await page.getByRole('radio', { name: 'LaTeX', exact: true }).check();
  await expect(preview.locator('.katex-display')).toHaveCount(3);
  await expect(preview.locator('.katex-error')).toHaveCount(0);
  await page.getByRole('group', { name: 'Message role', exact: true }).getByRole('radio', { name: 'User', exact: true }).check();
  await expect(preview.locator('.ui-chat-message--user')).toBeVisible();
  await expect(preview.getByRole('button', { name: 'Copy original text' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await preview.screenshot({ path: `/tmp/molr-rich-text-${info.project.name}.png` });
});

test('HTTP clipboard fallback copies original whitespace and restores focus', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  const raw = '  **Original**\n\n$E=mc^2$\n```js\nconst value = "中文";\n```\n';
  await page.getByRole('textbox', { name: 'Edit original text' }).fill(raw);
  await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined }));
  const copy = page.locator(sample).getByRole('button', { name: 'Copy original text' });
  await copy.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator(sample).getByRole('button', { name: 'Copied', exact: true })).toBeFocused();
  await page.evaluate(() => Reflect.deleteProperty(navigator, 'clipboard'));
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(raw);
});

test('copy failures are visible and retryable; empty messages have no copy action', async ({ page }) => {
  await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: () => Promise.reject(new Error('denied')) } }));
  const preview = page.locator(sample);
  await preview.getByRole('button', { name: 'Copy original text' }).click();
  await expect(preview.getByRole('status')).toContainText('Copy failed');
  await expect(preview.getByRole('button', { name: 'Copy original text' })).toBeEnabled();
  await page.getByRole('textbox', { name: 'Edit original text' }).fill('');
  await expect(preview.getByRole('button', { name: 'Copy original text' })).toHaveCount(0);
});

test('untrusted content and incomplete formulas remain safe; wide content stays inside the message', async ({ page }) => {
  const preview = page.locator(sample);
  const source = page.getByRole('textbox', { name: 'Edit original text' });
  await source.fill('<script>window.markdownExecuted = true</script>\n\n[unsafe](javascript:alert(1))\n\n<img src=x onerror="window.markdownExecuted = true">\n\n$$\n\\frac{1}{\n$$\n\nIncomplete $x^2');
  await expect(preview.locator('script, img[onerror], a[href^="javascript:"]')).toHaveCount(0);
  expect(await page.evaluate(() => Reflect.get(window, 'markdownExecuted'))).toBeUndefined();
  await expect(preview).toContainText('Incomplete $x^2');
  await expect(preview.locator('.katex-error')).toBeVisible();
  await source.fill('```text\n' + 'long_code_'.repeat(70) + '\n```\n\n| A | B | C | D | E |\n| --- | --- | --- | --- | --- |\n| one | two | three | four | five |\n\n$$\n' + 'a + '.repeat(40) + 'b\n$$');
  await expect(preview.locator('.katex-display')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
