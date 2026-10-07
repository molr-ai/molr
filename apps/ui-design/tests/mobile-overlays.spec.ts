import { test, expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { selectAssistant } from './interface-helpers';

async function preview(page: Page) {
  await page.goto('/#interfaces');
  await page.locator('#interfaces').getByRole('tab', { name: 'Mobile', exact: true }).click();
  const shell = page.locator('.interface-preview.ip-shell--mobile:visible');
  await shell.scrollIntoViewIfNeeded();
  return shell;
}

async function expectContained(shell: Locator, sheet: Locator) {
  await expect(sheet).toHaveAttribute('data-contained', 'true');
  await expect(sheet).toHaveAttribute('data-side', 'bottom');
  await expect(sheet).toHaveCSS('transform', 'none');
  const frame = (await shell.boundingBox())!, panel = (await sheet.boundingBox())!;
  expect(panel.x).toBeGreaterThanOrEqual(frame.x);
  expect(panel.x + panel.width).toBeLessThanOrEqual(frame.x + frame.width + 1);
  expect(panel.y).toBeGreaterThanOrEqual(frame.y);
  expect(panel.y + panel.height).toBeCloseTo(frame.y + frame.height - 1, 0);
  expect(await sheet.evaluate(el => el.matches(':modal'))).toBe(false);
  const layer = sheet.locator('..');
  await expect(layer).toHaveCSS('overflow', 'hidden');
  const mask = (await layer.locator(':scope > .ui-modal-backdrop').boundingBox())!;
  expect(mask.x).toBeCloseTo(frame.x + 1, 0);
  expect(mask.y).toBeCloseTo(frame.y + 1, 0);
  expect(mask.width).toBeCloseTo(frame.width - 2, 0);
  expect(mask.height).toBeCloseTo(frame.height - 2, 0);
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('molr-ui-design-locale', 'en'));
});

test('mobile search stays inside the preview with a local backdrop and multi-select menu', async ({ page }, info) => {
  const shell = await preview(page);
  await expect(shell.locator('.mobile-assistant-toolbar')).toHaveCSS('margin-bottom', '8px');
  const trigger = shell.getByRole('button', { name: 'Search', exact: true });
  await expect(trigger).toHaveCSS('width', '44px');
  await expect(trigger).toHaveCSS('height', '44px');
  const more = shell.locator('.mobile-assistant-toolbar').getByRole('button', { name: 'More', exact: true });
  await expect(more).toHaveCSS('width', '44px');
  await expect(more).toHaveCSS('height', '44px');
  await shell.screenshot({ path: `/tmp/molr-mobile-toolbar-${info.project.name}.png` });
  await trigger.click();
  const search = shell.getByRole('dialog', { name: 'Search', exact: true });
  await expectContained(shell, search);
  await expect(page.locator('html')).not.toHaveCSS('overflow', 'hidden');
  await expect(shell.locator(':scope > .ui-modal-scope-content')).toHaveAttribute('inert', '');
  await expect(search.locator(':scope > .ui-dialog__header > button')).toHaveCount(0);
  await expect(search.locator('.ui-dialog__footer')).toHaveCount(0);
  await expect(search.locator('.search-field__actions')).toHaveCSS('right', '12px');
  await search.getByRole('searchbox').fill('travel');
  const filter = search.getByRole('combobox', { name: 'Filter search types' });
  await filter.click();
  const menu = search.getByRole('listbox', { name: 'Filter search types' });
  await expect(menu).toHaveAttribute('aria-multiselectable', 'true');
  const resources = menu.getByRole('option', { name: 'Resources', exact: true });
  await resources.click();
  await menu.getByRole('option', { name: 'Chats', exact: true }).click();
  await expect(resources).toHaveAttribute('aria-selected', 'true');
  await expect(menu.getByRole('option', { name: 'Chats', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(menu.getByRole('checkbox')).toHaveCount(0);
  await expect(menu.getByRole('option').nth(1)).toHaveCSS('margin-top', '4px');
  await menu.getByRole('option', { name: 'Assistants', exact: true }).hover();
  const selectedBackground = await resources.evaluate(el => getComputedStyle(el).backgroundColor);
  await expect(menu.getByRole('option', { name: 'Assistants', exact: true })).toHaveCSS('background-color', selectedBackground);
  const frame = (await shell.boundingBox())!, box = (await menu.boundingBox())!;
  expect(box.x).toBeGreaterThanOrEqual(frame.x);
  expect(box.x + box.width).toBeLessThanOrEqual(frame.x + frame.width);
  expect(box.y).toBeGreaterThanOrEqual(frame.y);
  expect(box.y + box.height).toBeLessThanOrEqual(frame.y + frame.height);
  await shell.screenshot({ path: `/tmp/molr-contained-search-${info.project.name}.png` });
  await page.keyboard.press('Escape');
  await expect(menu).not.toBeVisible();
  await expect(search).toBeVisible();
  await search.locator('..').locator(':scope > .ui-modal-backdrop').click({ position: { x: 10, y: 10 } });
  await expect(search).not.toBeVisible();
  await expect(trigger).toBeFocused();
  await expect(shell.locator(':scope > .ui-modal-scope-content')).not.toHaveAttribute('inert');
});

test('mobile backdrop stays transparent when the exit animation is removed and reopens normally', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const shell = await preview(page);
  const trigger = shell.getByRole('button', { name: 'Search', exact: true });
  await trigger.click();
  const search = shell.getByRole('dialog', { name: 'Search', exact: true });
  await expectContained(shell, search);
  const backdrop = shell.locator('.workspace-search').locator('..').locator(':scope > .ui-modal-backdrop');
  await expect(backdrop).toHaveCSS('opacity', '1');
  // Observe the frame between animation cleanup and React hiding the layer.
  await backdrop.evaluate(element => {
    const cancel = Animation.prototype.cancel;
    const opacities: number[] = [];
    Animation.prototype.cancel = function () {
      const effect = this.effect as KeyframeEffect | null;
      const exitFinished = this.playState === 'finished' && Number(effect?.getKeyframes().at(-1)?.opacity) === 0;
      cancel.call(this);
      if (exitFinished && this.id === 'molr-drawer-backdrop' && effect?.target === element) {
        opacities.push(element.getClientRects().length ? Number(getComputedStyle(element).opacity) : 0);
        element.setAttribute('data-exit-opacities', JSON.stringify(opacities));
      }
    };
  });
  await page.keyboard.press('Escape');
  await expect(search).not.toBeVisible();
  const opacities = JSON.parse((await backdrop.getAttribute('data-exit-opacities'))!);
  expect(opacities.length).toBeGreaterThan(0);
  expect(opacities.every((opacity: number) => opacity === 0)).toBe(true);
  await expect(trigger).toBeFocused();

  for (const reducedMotion of ['no-preference', 'reduce'] as const) {
    await page.emulateMedia({ reducedMotion });
    await trigger.click();
    await expectContained(shell, search);
    await expect(backdrop).toHaveCSS('opacity', '1');
    await page.keyboard.press('Escape');
    await expect(search).not.toBeVisible();
    await expect(trigger).toBeFocused();
  }
});

test('mobile confirmations, information and task sheets leave the rest of the page usable', async ({ page }, info) => {
  const shell = await preview(page);
  await selectAssistant(shell);
  const more = shell.locator('.conversation-header').getByRole('button', { name: 'More', exact: true });
  await more.click();
  await page.getByRole('menuitem', { name: 'Rename assistant', exact: true }).click();
  const rename = shell.getByRole('dialog', { name: 'Rename assistant', exact: true });
  await expectContained(shell, rename);
  await expect(rename.getByRole('textbox', { name: 'Assistant name' })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(rename).not.toBeVisible();
  await expect(more).toBeFocused();
  await shell.getByRole('button', { name: 'Message information', exact: true }).first().click();
  const information = shell.getByRole('dialog', { name: 'Message information', exact: true });
  await expectContained(shell, information);
  await page.keyboard.press('Escape');
  await expect(information).not.toBeVisible();
  await shell.locator('.conversation-header').getByRole('button', { name: 'Plans', exact: true }).click();
  const tasks = shell.getByRole('dialog', { name: 'Plans', exact: true });
  await expectContained(shell, tasks);
  await shell.screenshot({ path: `/tmp/molr-contained-tasks-${info.project.name}.png` });
  await page.locator('#interfaces').getByRole('tab', { name: 'Web', exact: true }).click();
  await expect(tasks).not.toBeVisible();
  await page.locator('#interfaces').getByRole('tab', { name: 'Mobile', exact: true }).click();
  await expect(shell.locator(':scope > .ui-modal-scope-content')).not.toHaveAttribute('inert');
  await expect(shell.getByRole('textbox', { name: 'Message your assistant' })).toBeEnabled();
});
