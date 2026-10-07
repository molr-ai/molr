import type { Locator } from '@playwright/test';

export async function assistantList(ui: Locator) {
  const back = ui.getByRole('button', { name: 'Back to assistants', exact: true });
  if (await back.isVisible()) await back.click();
  const sidebar = ui.getByRole('complementary', { name: 'Assistant sidebar' });
  if (!await sidebar.isVisible()) await ui.getByRole('button', { name: 'Assistant list', exact: true }).click();
  return ui.getByRole('list', { name: 'Assistant list' });
}

export async function selectAssistant(ui: Locator, name = 'Molr') {
  const back = ui.getByRole('button', { name: 'Back to assistants', exact: true });
  if (await back.isVisible()) await back.click();
  await ui.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: 'Assistant', exact: true }).click();
  const list = await assistantList(ui);
  await list.getByRole('button', { name, exact: true }).click();
}
