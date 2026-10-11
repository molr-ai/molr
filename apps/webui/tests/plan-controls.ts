import type { Locator, Page } from '@playwright/test';

export async function selectPlanRange(scope: Page | Locator, name: string | RegExp) {
  await scope.getByRole('button', { name: 'Event range', exact: true }).click();
  await scope.getByRole('menu', { name: 'Event range', exact: true }).getByRole('menuitemradio', { name }).click();
}
