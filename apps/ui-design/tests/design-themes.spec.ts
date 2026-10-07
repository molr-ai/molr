import { test, expect } from '@playwright/test';
import { chooseOption } from './select-helpers';

const themes = [['forest', 'Forest'], ['ocean', 'Ocean'], ['iris', 'Iris'], ['sand', 'Sand'], ['rose', 'Rose'], ['monochrome', 'Monochrome']] as const;
test('all palettes update real surfaces in both appearances and survive reload', async ({ page }, info) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/#colors');
  const backgrounds = new Set<string>();
  for (const mode of ['Light', 'Dark']) {
    await chooseOption(page.getByRole('combobox', { name: 'Appearance', exact: true }), new RegExp(`^${mode} `));
    for (const [id, name] of themes) {
      const card = page.getByRole('group', { name: 'Color palettes', exact: true }).getByRole('button', { name, exact: true });
      await card.click();
      await expect(card).toHaveAttribute('aria-pressed', 'true');
      await expect(page.locator('html')).toHaveAttribute('data-palette', id);
      const surface = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
      backgrounds.add(surface);
      await expect(page.locator('.color-card__swatch').nth(2)).toHaveCSS('background-color', surface);
      const paletteValues = await page.evaluate(() => {
        const style = getComputedStyle(document.documentElement);
        return ['primary', 'accent', 'background', 'surface', 'text-muted', 'border'].map(token => style.getPropertyValue(`--color-${token}`).trim().toUpperCase());
      });
      await expect(page.locator('.color-card__hex')).toHaveText(paletteValues);
      for (const value of await page.locator('.color-card__hex').allTextContents()) expect(value).toMatch(/^#[\dA-F]{6}$/);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }
  }
  expect(backgrounds.size).toBe(12);
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-palette', 'monochrome');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByRole('combobox', { name: 'Theme colors', exact: true })).toContainText('Monochrome');
  await chooseOption(page.getByRole('combobox', { name: 'Theme colors', exact: true }), /^Ocean /);
  await expect(page.getByRole('group', { name: 'Color palettes' }).getByRole('button', { name: 'Ocean', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await page.setViewportSize({ width: 320, height: 720 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: `.cache/acceptance/themes-mobile-${info.project.name}.png` });
  expect(errors).toEqual([]);
});

test('keyboard, system appearance, cross-tab changes and Chinese labels work', async ({ page, context }) => {
  await page.goto('/#colors');
  const group = page.getByRole('group', { name: 'Color palettes' });
  await group.getByRole('button', { name: 'Iris', exact: true }).focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('html')).toHaveAttribute('data-palette', 'iris');
  await chooseOption(page.getByRole('combobox', { name: 'Appearance', exact: true }), /^System /);
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.locator('html')).toHaveAttribute('data-palette', 'iris');
  const other = await context.newPage();
  await other.goto('/');
  await other.evaluate(() => localStorage.setItem('molr-ui-design-palette', 'sand'));
  await expect(page.locator('html')).toHaveAttribute('data-palette', 'sand');
  await other.evaluate(() => localStorage.setItem('molr-ui-design-palette', 'unknown'));
  await expect(page.locator('html')).toHaveAttribute('data-palette', 'forest');
  await other.close();
  await chooseOption(page.getByRole('combobox', { name: 'Language', exact: true }), '简体中文');
  await expect(page.getByRole('group', { name: '主题配色' }).getByRole('button', { name: '玫瑰', exact: true })).toBeVisible();
});

test('themes remain switchable when browser storage is blocked', async ({ page }) => {
  await page.addInitScript(() => {
    Storage.prototype.getItem = () => { throw new Error('Storage blocked'); };
    Storage.prototype.setItem = () => { throw new Error('Storage blocked'); };
  });
  await page.goto('/#colors');
  await page.getByRole('group', { name: 'Color palettes' }).getByRole('button', { name: 'Rose', exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('data-palette', 'rose');
  await expect(page.getByRole('combobox', { name: 'Theme colors', exact: true })).toContainText('Rose');
});
