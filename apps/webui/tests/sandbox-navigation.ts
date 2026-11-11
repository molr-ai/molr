import { expect, type Page } from '@playwright/test';

export async function openSandboxDetails(page: Page) {
  await page.locator('.sandbox-list').waitFor({ state: 'attached' });
  if (!(await page.locator('.sandbox-detail').isVisible())) await page.locator('.sandbox-item').first().click();
  await expect(page.locator('.sandbox-detail')).toBeVisible();
}

export async function openSandboxList(page: Page) {
  if (!(await page.locator('.sandbox-sidebar').isVisible())) await page.locator('.sandbox-heading').getByRole('button', { name: 'Back', exact: true }).click();
  await expect(page.locator('.sandbox-sidebar')).toBeVisible();
}
