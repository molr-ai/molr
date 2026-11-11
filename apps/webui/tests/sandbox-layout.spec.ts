import { test, expect } from '@playwright/test';

test('sandbox uses a sidebar on tablet and list/detail navigation on phones', async ({ page }, info) => {
  await page.goto('/#/resources');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('tab', { name: 'Sandboxes', exact: true }).click();
  const sidebar = page.locator('.sandbox-sidebar'), detail = page.locator('.sandbox-detail'), entry = page.locator('.sandbox-item').first();
  await expect(sidebar).toBeVisible();
  await expect(page.locator('.sandbox-mobile-switch')).toHaveCount(0);
  if (info.project.name === 'mobile') {
    await expect(detail).toBeHidden();
    const workspace = (await page.locator('.sandbox-workspace').boundingBox())!, listPane = (await sidebar.boundingBox())!;
    expect(Math.abs(workspace.y + workspace.height - listPane.y - listPane.height)).toBeLessThanOrEqual(1);
    await entry.click();
    await expect(detail).toBeVisible();
    await expect(sidebar).toBeHidden();
    await expect(page).toHaveURL(/sandbox=local/);
    await page.goBack();
    await expect(sidebar).toBeVisible();
    await expect(entry).toBeFocused();
    await page.goForward();
    await expect(detail).toBeVisible();
    await page.reload();
    await expect(detail).toBeVisible();
    await detail.getByRole('button', { name: 'Back', exact: true }).click();
    await expect(sidebar).toBeVisible();
    await entry.click();
  } else {
    await page.setViewportSize({ width: 900, height: 900 });
    await expect(sidebar).toBeVisible();
    await expect(detail).toBeVisible();
    const left = (await sidebar.boundingBox())!, right = (await detail.boundingBox())!;
    expect(right.x).toBeGreaterThanOrEqual(left.x + left.width);
  }
  const tabs = detail.locator(':scope > .ui-tabs > .ui-tabs__list');
  const section = detail.locator('.sandbox-section').first();
  await expect(section).toBeVisible();
  expect((await section.boundingBox())!.y - ((await tabs.boundingBox())!.y + (await tabs.boundingBox())!.height)).toBeLessThanOrEqual(22);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
