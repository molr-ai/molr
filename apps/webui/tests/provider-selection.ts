import type { Locator } from '@playwright/test';

export async function selectProvider(dialog: Locator, name: string, query = name) {
  const picker = dialog.getByRole('combobox', { name: 'Model providers', exact: true });
  if (await picker.count()) {
    await picker.click();
    await dialog.getByPlaceholder('Search providers', { exact: true }).fill(query);
    await dialog.getByRole('option', { name, exact: true }).click();
  } else {
    await dialog.getByRole('searchbox', { name: 'Search providers', exact: true }).fill(query);
    await dialog.getByRole('button', { name, exact: true }).click();
  }
}
