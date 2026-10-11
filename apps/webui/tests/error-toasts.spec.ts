import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
 await page.clock.install();
 await page.goto('/tests/fixtures/error-retry.html');
});

test('service failures share one Toast and remain dismissed through background retries and remounts', async ({ page }) => {
 const toast = page.locator('.ui-toast--danger');
 await page.getByRole('button', { name: 'Service unavailable', exact: true }).click();
 await page.getByRole('button', { name: 'Toggle second source', exact: true }).click();
 await expect(toast).toHaveCount(1);
 await expect(toast).toContainText('The service is unavailable');
 await toast.getByRole('button', { name: 'Close', exact: true }).click();
 await page.getByRole('button', { name: 'Background retry', exact: true }).click();
 await page.getByRole('button', { name: 'Toggle notice', exact: true }).click();
 await page.getByRole('button', { name: 'Toggle notice', exact: true }).click();
 await expect(toast).toHaveCount(0);
 await page.getByRole('button', { name: 'Retry operation', exact: true }).click();
 await expect(toast).toHaveCount(1);
 await page.getByRole('button', { name: 'Clear error', exact: true }).click();
 await expect(toast).toHaveCount(0);
});

test('ordinary errors keep separate sources and leave no Toast after their source unmounts', async ({ page }) => {
 const toast = page.locator('.ui-toast--danger');
 await page.getByRole('button', { name: 'Operation failed', exact: true }).click();
 await page.getByRole('button', { name: 'Toggle second source', exact: true }).click();
 await expect(toast).toHaveCount(2);
 await toast.first().getByRole('button', { name: 'Close', exact: true }).click();
 await expect(toast).toHaveCount(1);
 await page.getByRole('button', { name: 'Toggle notice', exact: true }).click();
 await expect(toast).toHaveCount(0);
});

test('errors without retry expire once and background refreshes do not reopen them', async ({ page }) => {
 const toast = page.locator('.ui-toast--danger');
 await page.getByRole('button', { name: 'Toggle retry action', exact: true }).click();
 await page.getByRole('button', { name: 'Operation failed', exact: true }).click();
 await expect(toast).toBeVisible();
 await page.clock.runFor(5100);
 await expect(toast).toHaveCount(0);
 await page.getByRole('button', { name: 'Background retry', exact: true }).click();
 await expect(toast).toHaveCount(0);
 await page.getByRole('button', { name: 'Clear error', exact: true }).click();
 await page.getByRole('button', { name: 'Operation failed', exact: true }).click();
 await expect(toast).toBeVisible();
});
