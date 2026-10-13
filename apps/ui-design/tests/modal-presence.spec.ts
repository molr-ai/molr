import { expect, test, type Locator } from '@playwright/test';

async function finish(dialog: Locator) {
  await dialog.evaluate(element => {
    for (const animation of element.getAnimations({ subtree: true })) animation.finish();
    const backdrop = element.parentElement?.querySelector('.ui-modal-backdrop');
    for (const animation of backdrop?.getAnimations() ?? []) animation.finish();
  });
}
async function pauseExit(dialog: Locator, button: string) {
  await finish(dialog);
  await dialog.evaluate(async (element, button) => {
    const changed = new Promise<void>(resolve => {
      const observer = new MutationObserver(() => {
        if (!element.isConnected || element.getAttribute('data-state') === 'closed') { observer.disconnect(); resolve(); }
      });
      observer.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['data-state'] });
    });
    [...element.querySelectorAll('button')].find(item => item.textContent === button)!.click();
    await changed;
    const slide = element.getAnimations().find(animation => animation.id === 'molr-drawer-slide');
    if (!element.isConnected || !slide) throw new Error('Conditional modal disappeared before exit');
    slide.pause(); slide.currentTime = 110;
  }, button);
}

test.beforeEach(async ({ page }) => { await page.goto('/tests/fixtures/modal-presence.html'); });

for (const mode of ['right', 'bottom', 'contained']) test(`${mode} keeps conditional contents and effects until exit ends`, async ({ page }) => {
  const trigger = page.getByRole('button', { name: `Open retained ${mode}`, exact: true });
  const dialog = page.getByRole('dialog', { name: 'Retained details', exact: true });
  await trigger.click();
  await dialog.getByLabel('Retained draft').fill('Unsaved draft');
  await pauseExit(dialog, 'Cancel retained form');
  await expect(dialog).toHaveAttribute('open');
  await expect(dialog.getByLabel('Retained draft')).toHaveValue('Unsaved draft');
  await expect(page.getByLabel('Live modal effects')).toHaveText('1');
  expect(await dialog.evaluate(element => {
    const transform = new DOMMatrixReadOnly(getComputedStyle(element).transform);
    return Math.max(transform.m41, transform.m42);
  })).toBeGreaterThan(0);
  // Retained stale callbacks cannot submit or start another action during exit.
  await dialog.getByRole('button', { name: 'Retained action' }).evaluate((button: HTMLButtonElement) => button.click());
  await expect(page.getByLabel('Presence actions')).toHaveText('0');
  await finish(dialog);
  await expect(dialog).toHaveCount(0);
  await expect(page.getByLabel('Live modal effects')).toHaveText('0');
  await expect(trigger).toBeFocused();
  await trigger.click();
  await expect(dialog.getByLabel('Retained draft')).toHaveValue('');
});

test('nested presence closes independently and parent closure waits for all layers', async ({ page }) => {
  const trigger = page.getByRole('button', { name: 'Open retained contained', exact: true });
  const parent = page.getByRole('dialog', { name: 'Retained details', exact: true, includeHidden: true });
  const child = page.getByRole('dialog', { name: 'Retained child', exact: true });
  await trigger.click(); await finish(parent);
  await parent.getByRole('button', { name: 'Open retained child', exact: true }).click(); await finish(child);
  await page.keyboard.press('Escape');
  await expect(child).toHaveCount(0);
  await expect(parent).toBeVisible();
  await expect(parent.getByRole('button', { name: 'Open retained child', exact: true })).toBeFocused();
  await parent.getByRole('button', { name: 'Open retained child', exact: true }).click();
  await pauseExit(child, 'Close retained family');
  await finish(parent);
  // The child is still paused; finishing the parent cannot remove its subtree.
  await expect(child).toHaveAttribute('open');
  await finish(child);
  await expect(parent).toHaveCount(0); await expect(child).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await expect(page.locator('[inert]')).toHaveCount(0);
});

test('rapid reopening preserves draft and hard owner unmount cleans up', async ({ page }) => {
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
  await page.clock.install();
  const trigger = page.getByRole('button', { name: 'Open retained bottom', exact: true });
  const dialog = page.getByRole('dialog', { name: 'Retained details', exact: true });
  await trigger.click(); await dialog.getByLabel('Retained draft').fill('Keep during reversal');
  await pauseExit(dialog, 'Reverse retained exit');
  await page.clock.runFor(70);
  await expect(dialog).toHaveAttribute('data-state', 'open');
  await expect(dialog.getByLabel('Retained draft')).toHaveValue('Keep during reversal');
  await finish(dialog);
  await dialog.getByRole('button', { name: 'Unmount retained owner' }).click();
  await expect(dialog).toHaveCount(0); await expect(trigger).toBeFocused();
  await expect(page.getByLabel('Live modal effects')).toHaveText('0');
  await expect(page.locator('html')).not.toHaveCSS('overflow', 'hidden');
  expect(errors).toEqual([]);
});

for (const preference of ['system', 'account']) test(`${preference} reduced motion immediately cleans up conditional dialogs`, async ({ page }) => {
  if (preference === 'system') await page.emulateMedia({ reducedMotion: 'reduce' });
  else await page.getByRole('button', { name: 'Toggle presence reduced motion' }).click();
  const trigger = page.getByRole('button', { name: 'Open retained contained', exact: true });
  const dialog = page.getByRole('dialog', { name: 'Retained details', exact: true });
  await trigger.click();
  expect(await dialog.evaluate(element => element.getAnimations().length)).toBe(0);
  await dialog.getByRole('button', { name: 'Cancel retained form' }).click();
  await expect(dialog).toHaveCount(0); await expect(trigger).toBeFocused();
  await expect(page.getByLabel('Live modal effects')).toHaveText('0');
});


test('controlled drawers keep selected content until exit then release it', async ({ page }) => {
  const dialog = page.locator('.controlled-selection');
  await page.getByRole('button', { name: 'Open controlled selection' }).click();
  await pauseExit(dialog, 'Clear controlled selection');
  await expect(dialog.getByRole('heading', { name: 'Selected record', exact: true })).toBeVisible();
  await expect(dialog.locator('.ui-dialog__body')).toHaveText('Selected record contents');
  await expect(page.getByLabel('Completed modal exits')).toHaveText('0');
  await finish(dialog);
  await expect(dialog).not.toBeVisible();
  await expect(dialog.locator('.ui-dialog__body')).toHaveCount(0);
  await expect(page.getByLabel('Completed modal exits')).toHaveText('1');
});


test('a late load cannot replace the closing modal', async ({ page }) => {
  await page.clock.install();
  const trigger = page.getByRole('button', { name: 'Open loading modal' });
  await trigger.click();
  const dialog = page.getByRole('dialog', { name: 'Loading modal', exact: true });
  await pauseExit(dialog, 'Resolve during exit');
  await page.clock.runFor(40);
  await expect(dialog).toHaveAttribute('open');
  await expect(page.getByRole('dialog', { name: 'Loaded modal' })).toHaveCount(0);
  await finish(dialog);
  await expect(dialog).toHaveCount(0); await expect(trigger).toBeFocused();
});
