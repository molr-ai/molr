import { expect, test } from '@playwright/test';

test.beforeEach(async ({ page }) => { await page.goto('/tests/fixtures/'); });

for (const alreadyOpen of [false, true]) {
  for (const background of ['blur', 'hidden'] as const) {
    test(`${background} clears ${alreadyOpen ? 'visible' : 'pending'} tooltips without reopening on restored focus`, async ({ page }) => {
      const trigger = page.getByRole('button', { name: 'Ref help 0', exact: true });
      const tip = page.getByRole('tooltip', { name: 'Ref tooltip', exact: true });
      await trigger.focus();
      if (alreadyOpen) await expect(tip).toBeVisible();
      await page.evaluate(background => {
        if (background === 'hidden') {
          Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
          document.dispatchEvent(new Event('visibilitychange'));
        } else window.dispatchEvent(new Event('blur'));
        document.activeElement?.dispatchEvent(new FocusEvent('focusout', { bubbles: true }));
      }, background);
      await expect(tip).not.toBeVisible();
      await page.waitForTimeout(500);
      await expect(tip).not.toBeVisible();
      await page.evaluate(background => {
        if (background === 'hidden') {
          delete (document as unknown as { visibilityState?: string }).visibilityState;
          document.dispatchEvent(new Event('visibilitychange'));
        }
        window.dispatchEvent(new Event('focus'));
        document.activeElement?.dispatchEvent(new FocusEvent('focusin', { bubbles: true }));
      }, background);
      await expect(trigger).toBeFocused();
      await page.waitForTimeout(500);
      await expect(tip).not.toBeVisible();

      // A real Tab still reveals the next button's description.
      await page.keyboard.press('Tab');
      await expect(page.getByRole('button', { name: 'Legacy ref help', exact: true })).toBeFocused();
      await expect(page.getByRole('tooltip', { name: 'Legacy ref tooltip', exact: true })).toBeVisible();
      await page.keyboard.press('Shift+Tab');
      await expect(tip).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(tip).not.toBeVisible();
    });
  }
}

test('pointer movement resumes hover after window restoration, without a focus event', async ({ page }) => {
  const trigger = page.getByRole('button', { name: 'Ref help 0', exact: true });
  const tip = page.getByRole('tooltip', { name: 'Ref tooltip', exact: true });
  await trigger.hover();
  await expect(tip).toBeVisible();
  await page.evaluate(() => { window.dispatchEvent(new Event('blur')); window.dispatchEvent(new Event('focus')); });
  // Restoring a window may synthesize pointer entry without actual movement.
  await trigger.dispatchEvent('pointerover', { pointerType: 'mouse' });
  await page.waitForTimeout(500);
  await expect(tip).not.toBeVisible();
  const bounds = (await trigger.boundingBox())!;
  await page.mouse.move(bounds.x + bounds.width / 2 + 2, bounds.y + bounds.height / 2);
  await expect(tip).toBeVisible();
  await page.mouse.move(0, 0);
  await expect(tip).not.toBeVisible();
});

for (const closeWith of ['Escape', 'close button', 'backdrop', 'drawer close', 'drawer unmount'] as const) {
  test(`${closeWith} restores trigger focus without reopening its tooltip`, async ({ page }) => {
    const drawer = closeWith.startsWith('drawer');
    const trigger = page.getByRole('button', { name: drawer ? 'Open motion drawer' : 'Open modal', exact: true });
    const tip = page.getByRole('tooltip', { name: drawer ? 'Open drawer details' : 'Open modal details', exact: true });
    await trigger.hover();
    await expect(tip).toBeVisible();
    await trigger.click();
    const dialog = page.getByRole('dialog', { name: drawer ? 'Motion drawer' : 'Initial focus', exact: true });
    await expect(dialog).toBeVisible();
    if (closeWith === 'Escape') await page.keyboard.press('Escape');
    else if (closeWith === 'backdrop') await page.mouse.click(1, 1);
    else await dialog.getByRole('button', { name: closeWith === 'drawer unmount' ? 'Unmount drawer' : 'Close', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(trigger).toBeFocused();
    await page.waitForTimeout(500);
    await expect(tip).not.toBeVisible();
    // Stay inside the document even when the trigger is its last tab stop.
    await page.keyboard.press('Shift+Tab');
    await page.keyboard.press('Tab');
    await expect(trigger).toBeFocused();
    await expect(tip).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(tip).not.toBeVisible();
    await page.mouse.move(0, 0);
    await trigger.hover();
    await expect(tip).toBeVisible();
  });
}
