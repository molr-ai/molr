import { test, expect } from '@playwright/test';
import { assistantList, selectAssistant } from './interface-helpers';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('molr-ui-design-locale', 'en'));
  await page.goto('/#interfaces');
});

for (const version of ['Web', 'Desktop', 'Mobile']) {
  test(`${version} previews cloning and dismissal without modifying the assistant`, async ({ page }, info) => {
    await page.locator('#interfaces').getByRole('tab', { name: version, exact: true }).click();
    const shell = page.locator('.interface-preview.ip-shell:visible');
    await selectAssistant(shell);
    const input = shell.getByRole('textbox', { name: 'Message your assistant' });
    await input.fill('Keep this draft');
    const before = await page.evaluate(() => localStorage.getItem('molr-assistant-prototype-v1'));
    const more = shell.locator('.conversation-header').getByRole('button', { name: 'More', exact: true });

    await more.click();
    await page.getByRole('menuitem', { name: 'Clone assistant', exact: true }).click();
    const clone = page.getByRole('dialog', { name: 'Clone assistant', exact: true });
    await expect(clone).toContainText('Molr');
    await expect(clone).toContainText('memories, message history, and permissions');
    await clone.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(more).toBeFocused();
    await more.click();
    await page.getByRole('menuitem', { name: 'Clone assistant', exact: true }).click();
    await clone.screenshot({ path: `/tmp/molr-clone-${version}-${info.project.name}.png` });
    await clone.getByRole('button', { name: 'Confirm clone', exact: true }).click();
    await expect(clone).not.toBeVisible();
    await expect(page.getByRole('status').filter({ hasText: 'Interaction preview' }).last()).toContainText('No assistants or conversations were changed');

    await more.click();
    await page.getByRole('menuitem', { name: 'Dismiss assistant', exact: true }).click();
    const dismiss = page.getByRole('dialog', { name: 'Dismiss assistant', exact: true });
    await expect(dismiss).toContainText('Molr');
    await expect(dismiss).toContainText('will be archived');
    await expect(dismiss).toContainText('no longer be able to send new messages');
    await expect(dismiss).toContainText('Settings → Former assistants');
    await expect(dismiss).toContainText('rehire');
    await page.keyboard.press('Escape');
    await expect(dismiss).not.toBeVisible();
    await expect(more).toBeFocused();
    await more.click();
    await page.getByRole('menuitem', { name: 'Dismiss assistant', exact: true }).click();
    await dismiss.screenshot({ path: `/tmp/molr-dismiss-${version}-${info.project.name}.png` });
    await dismiss.getByRole('button', { name: 'Confirm dismissal', exact: true }).click();
    await expect(dismiss).not.toBeVisible();
    await expect(input).toBeEnabled();
    await expect(input).toHaveValue('Keep this draft');
    expect(await page.evaluate(() => localStorage.getItem('molr-assistant-prototype-v1'))).toBe(before);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  });
}

test('former assistants can be searched, their archives viewed, and rehiring previewed', async ({ page }, info) => {
  if (info.project.name === 'mobile') await page.locator('#interfaces').getByRole('tab', { name: 'Mobile', exact: true }).click();
  const shell = page.locator('.interface-preview.ip-shell:visible');
  await shell.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: 'Settings', exact: true }).click();
  await shell.getByRole('tab', { name: 'Former assistants', exact: true }).click();
  const panel = shell.getByRole('tabpanel', { name: 'Former assistants', exact: true });
  const before = await page.evaluate(() => localStorage.getItem('molr-assistant-prototype-v1'));
  const search = panel.getByRole('searchbox', { name: 'Search former assistants and conversations' });
  await search.fill('not-found');
  await expect(panel.getByText('No matching former assistants', { exact: true })).toBeVisible();
  await panel.getByRole('status').getByRole('button', { name: 'Clear search', exact: true }).click();
  await search.fill('article');
  await expect(panel.getByRole('list', { name: 'Former assistants' }).getByText('Reading assistant', { exact: true })).toBeVisible();
  await expect(panel.getByText('Sample archive · 2 messages', { exact: true })).toBeVisible();
  await panel.screenshot({ path: `/tmp/molr-former-assistants-${info.project.name}.png` });
  const archiveButton = panel.getByRole('button', { name: 'View archive', exact: true });
  await archiveButton.click();
  const archive = page.getByRole('dialog', { name: 'Archived conversation', exact: true });
  await expect(archive).toContainText('new messages cannot be sent');
  await expect(archive.getByRole('region', { name: 'Archived messages' }).getByRole('article')).toHaveCount(2);
  await expect(archive.getByRole('textbox')).toHaveCount(0);
  await archive.screenshot({ path: `/tmp/molr-archived-conversation-${info.project.name}.png` });
  await page.keyboard.press('Escape');
  await expect(archiveButton).toBeFocused();

  const rehireButton = panel.getByRole('button', { name: 'Rehire', exact: true });
  await rehireButton.click();
  const rehire = page.getByRole('dialog', { name: 'Rehire assistant', exact: true });
  await expect(rehire).toContainText('Reading assistant');
  await expect(rehire).toContainText('send new messages again');
  await rehire.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(rehireButton).toBeFocused();
  await rehireButton.click();
  await rehire.getByRole('button', { name: 'Confirm rehire', exact: true }).click();
  await expect(rehire).not.toBeVisible();
  await expect(panel.getByRole('listitem')).toHaveCount(1);
  expect(await page.evaluate(() => localStorage.getItem('molr-assistant-prototype-v1'))).toBe(before);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('assistant list is flat with pinned entries first and no group action', async ({ page }, info) => {
  if (info.project.name === 'mobile') await page.locator('#interfaces').getByRole('tab', { name: 'Mobile', exact: true }).click();
  const shell = page.locator('.interface-preview.ip-shell:visible');
  const list = await assistantList(shell);
  await expect(list.getByRole('button').first()).toHaveAccessibleName('Molr');
  await expect(list.getByRole('button', { name: 'Molr', exact: true }).getByRole('img', { name: 'Pinned', exact: true })).toBeVisible();
  await expect(list.getByRole('list')).toHaveCount(0);
  await selectAssistant(shell, '旅行助理');
  const before = await page.evaluate(() => localStorage.getItem('molr-assistant-prototype-v1'));
  await shell.locator('.conversation-header').getByRole('button', { name: 'More', exact: true }).click();
  await expect(page.getByRole('menuitem', { name: 'Conversation group', exact: true })).toHaveCount(0);
  await page.getByRole('menuitem', { name: 'Pin conversation', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Pin conversation · Preview' })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem('molr-assistant-prototype-v1'))).toBe(before);
  await assistantList(shell);
  await expect(list.getByRole('button', { name: '旅行助理', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
