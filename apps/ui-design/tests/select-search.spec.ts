import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.goto('/#components');
  await page.getByRole('tablist', { name: 'Component categories', exact: true }).getByRole('tab', { name: 'Inputs & actions', exact: true }).click();
});

test('searchable single select filters labels, values and descriptions, then restores focus', async ({ page }) => {
  const trigger = page.getByRole('combobox', { name: 'Choose a resource', exact: true });
  await trigger.click();
  const search = page.getByRole('combobox', { name: 'Search Choose a resource', exact: true });
  const list = page.getByRole('listbox', { name: 'Choose a resource', exact: true });
  await expect(search).toBeFocused();
  await search.fill('RESEARCH');
  await expect(list.getByRole('option')).toHaveCount(1);
  await expect(list.getByRole('option')).toContainText('Exploration notes');
  await search.press('Enter');
  await expect(trigger).toContainText('Exploration notes');
  await expect(trigger).toBeFocused();
  await trigger.click();
  await expect(search).toHaveValue('');
  await expect(list.getByRole('option')).toHaveCount(8);
  await search.fill('document');
  await expect(list.getByRole('option')).toHaveCount(3);
  await search.fill('brand guide');
  await expect(list.getByRole('option')).toHaveCount(1);
  await search.fill('no-such-resource');
  await expect(page.getByRole('status').filter({ hasText: 'No matching options' })).toBeVisible();
  await search.press('Enter');
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
  await expect(search).not.toHaveAttribute('aria-activedescendant');
  await search.fill('icons');
  await expect(list.getByRole('option')).toHaveAttribute('aria-disabled', 'true');
  await search.press('Enter');
  await expect(trigger).toContainText('Exploration notes');
  await search.fill('guide');
  await search.dispatchEvent('keydown', { key: 'Enter', code: 'Enter', isComposing: true });
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
  await search.press('Escape');
  await expect(trigger).toBeFocused();
  await expect(trigger).toContainText('Exploration notes');
  await trigger.click();
  await search.press('Tab');
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByRole('combobox', { name: 'Choose multiple resources', exact: true })).toBeFocused();
});

test('searchable multiple select keeps hidden selections and supports keyboard navigation', async ({ page }) => {
  const trigger = page.getByRole('combobox', { name: 'Choose multiple resources', exact: true });
  await trigger.click();
  const search = page.getByRole('combobox', { name: 'Search Choose multiple resources', exact: true });
  const list = page.getByRole('listbox', { name: 'Choose multiple resources', exact: true });
  await search.fill('guide');
  await list.getByRole('option').click();
  await expect(search).toBeFocused();
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
  await search.fill('logo');
  await search.press('Enter');
  await expect(trigger).toContainText('Brand guide, Logo source');
  await search.fill('');
  await search.press('ArrowDown');
  await search.press('ArrowDown');
  await search.press('Enter');
  await expect(trigger).toContainText('Exploration notes');
  await search.fill('guide');
  await expect(list.getByRole('option')).toHaveAttribute('aria-selected', 'true');
  await search.press('Enter');
  await expect(trigger).not.toContainText('Brand guide');
  await page.getByRole('heading', { name: 'Searchable selects', exact: true }).click();
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
});

test('switch sizes keep keyboard toggling and thumb alignment', async ({ page }) => {
  const example = page.locator('.component-example').filter({ has: page.getByRole('heading', { name: 'Switch sizes', exact: true }) });
  for (const name of ['Small', 'Default', 'Large']) {
    const control = example.getByRole('switch', { name, exact: true });
    const track = control.locator('..').locator('.switch-control');
    await expect(control).not.toBeChecked();
    await control.focus();
    await control.press('Space');
    await expect(control).toBeChecked();
    await expect.poll(async () => track.evaluate(element => {
      const box = element.getBoundingClientRect(), thumb = element.firstElementChild!.getBoundingClientRect();
      const padding = parseFloat(getComputedStyle(element).paddingRight);
      return Math.abs(box.right - thumb.right - padding) < 0.5 && thumb.left >= box.left;
    })).toBe(true);
    await control.press('Space');
    await expect(control).not.toBeChecked();
    await expect.poll(async () => track.evaluate(element => {
      const box = element.getBoundingClientRect(), thumb = element.firstElementChild!.getBoundingClientRect();
      return Math.abs(thumb.left - box.left - parseFloat(getComputedStyle(element).paddingLeft)) < 0.5;
    })).toBe(true);
  }
});
