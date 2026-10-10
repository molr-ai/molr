import { test, expect } from '@playwright/test';
import { chooseOption } from './select-helpers';
import { selectAssistant } from './interface-helpers';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => { if (window === window.top) localStorage.setItem('molr-ui-design-locale', 'en'); });
  await page.goto('/#interfaces');
});

test('previews mount on first visit, suspend subscriptions and retain page state', async ({ page }) => {
  await page.addInitScript(() => {
    const native = window.matchMedia.bind(window);
    (window as any).compactListeners = 0;
    window.matchMedia = query => {
      const media = native(query);
      if (query !== '(max-width: 1000px)') return media;
      const add = media.addEventListener.bind(media), remove = media.removeEventListener.bind(media);
      media.addEventListener = ((type: string, listener: EventListener) => { if (type === 'change') (window as any).compactListeners++; add(type, listener); }) as typeof media.addEventListener;
      media.removeEventListener = ((type: string, listener: EventListener) => { if (type === 'change') (window as any).compactListeners--; remove(type, listener); }) as typeof media.removeEventListener;
      return media;
    };
  });
  await page.reload();
  await expect.poll(() => page.evaluate(() => (window as any).compactListeners)).toBe(1);
  const showcase = page.locator('#interfaces');
  const shell = page.locator('.interface-preview.ip-shell:visible');
  await expect(showcase.locator('.interface-preview.ip-shell')).toHaveCount(1);
  await expect(showcase.locator('.ip-terminal-input')).toHaveCount(0);
  await shell.getByRole('navigation').getByRole('button', { name: 'Settings', exact: true }).click();
  await shell.getByRole('textbox', { name: 'What are your preferences?' }).fill('Keep this unsaved preference.');
  await showcase.getByRole('tab', { name: 'Mobile', exact: true }).click();
  await expect(showcase.locator('.interface-preview.ip-shell')).toHaveCount(2);
  await expect(showcase.locator('.ip-terminal-input')).toHaveCount(0);
  await selectAssistant(shell);
  await shell.getByRole('textbox', { name: 'Message your assistant' }).fill('Shared conversation draft');
  await showcase.getByRole('tab', { name: 'CLI', exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as any).compactListeners)).toBe(0);
  const command = showcase.getByRole('textbox', { name: 'Terminal command' });
  await command.fill('help'); await command.press('Enter');
  await expect(showcase.getByRole('log')).toContainText('assistant list');
  await command.fill('say Update while previews are hidden'); await command.press('Enter');
  await expect(showcase.getByRole('log')).toContainText('Message added');
  await expect(showcase.locator('.ip-shell--mobile .message-content').filter({ hasText: 'Update while previews are hidden' })).toHaveCount(0);
  await showcase.getByRole('tab', { name: 'Web', exact: true }).click();
  await expect(shell.getByRole('textbox', { name: 'What are your preferences?' })).toHaveValue('Keep this unsaved preference.');
  await showcase.getByRole('tab', { name: 'Mobile', exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as any).compactListeners)).toBe(1);
  await expect(shell.getByRole('log', { name: 'Assistant conversation' }).getByText('Update while previews are hidden', { exact: true })).toBeVisible();
  // Sending through CLI consumes the shared draft; activation uses fresh data.
  await expect(shell.getByRole('textbox', { name: 'Message your assistant' })).toHaveValue('');
  await showcase.getByRole('tab', { name: 'CLI', exact: true }).click();
  await expect(showcase.getByRole('log')).toContainText('assistant list');
});

test('catalogs switch brand, product, hidden previews and parameterized copy together', async ({ page }) => {
  const showcase = page.locator('#interfaces');
  await showcase.getByRole('tab', { name: 'Mobile', exact: true }).click();
  await showcase.getByRole('tab', { name: 'Web', exact: true }).click();
  await chooseOption(page.getByRole('combobox', { name: 'Language', exact: true }), '简体中文');
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
  await expect(showcase.getByRole('heading', { name: '一个 Molr，四种入口' })).toBeVisible();
  await showcase.getByRole('tab', { name: 'Mobile', exact: true }).click();
  const shell = page.locator('.interface-preview.ip-shell:visible');
  await shell.getByRole('list', { name: '助理列表' }).getByRole('button', { name: 'Molr', exact: true }).click();
  await shell.locator('.conversation-header').getByRole('button', { name: '更多', exact: true }).click();
  await page.getByRole('menuitem', { name: '克隆助理', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '克隆助理', exact: true });
  await expect(dialog).toContainText('确认克隆「Molr」？');
  await dialog.getByRole('button', { name: '取消', exact: true }).click();
  await chooseOption(page.getByRole('combobox', { name: '语言', exact: true }), 'English');
  await expect(shell.getByRole('textbox', { name: 'Message your assistant' })).toBeVisible();
  await shell.locator('.conversation-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Clone assistant', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Clone assistant', exact: true })).toContainText('Clone “Molr”?');
});

test('named viewport breakpoints keep CSS and component behavior aligned at their edges', async ({ page }) => {
  const shell = page.locator('.interface-preview.ip-shell:visible');
  for (const width of [1001, 1000, 701, 700, 701, 1001]) {
    await page.setViewportSize({ width, height: 1000 });
    const workspace = shell.locator('.conversation-workspace');
    if (width <= 1000) await expect(workspace).toHaveClass(/conversation-workspace--compact/);
    else await expect(workspace).not.toHaveClass(/conversation-workspace--compact/);
    await expect(shell.getByRole('navigation', { name: 'Workspace navigation' })).toHaveCSS('flex-direction', width <= 700 ? 'row' : 'column');
    await shell.locator('.conversation-header').getByRole('button', { name: 'Plans', exact: true }).click();
    const drawer = page.getByRole('dialog', { name: 'Plans', exact: true });
    await expect(drawer).toHaveAttribute('data-side', width <= 700 ? 'bottom' : 'right');
    await drawer.getByRole('button', { name: 'Close plans', exact: true }).click();
    await expect(drawer).not.toBeVisible();
  }
});

test('switching away from a contained overlay closes it without reopening on return', async ({ page }) => {
  const showcase = page.locator('#interfaces');
  await showcase.getByRole('tab', { name: 'Mobile', exact: true }).click();
  const shell = page.locator('.interface-preview.ip-shell:visible');
  await shell.locator('.mobile-assistant-toolbar').getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await showcase.getByRole('tab', { name: 'Web', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await showcase.getByRole('tab', { name: 'Mobile', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(shell.locator(':scope > .ui-modal-scope-content')).not.toHaveAttribute('inert');
  await expect(shell.getByRole('list', { name: 'Assistant list' })).toBeVisible();
});
