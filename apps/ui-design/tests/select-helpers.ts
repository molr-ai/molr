import type { Locator } from '@playwright/test';

export async function chooseOption(control: Locator, name: string | RegExp) {
  await control.click();
  await control.page().getByRole('listbox').getByRole('option', { name, exact: typeof name === 'string' }).click();
}
