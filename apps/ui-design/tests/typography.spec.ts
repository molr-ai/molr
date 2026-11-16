import { test, expect } from './fixtures';

test('three type scales remain independent of all color palettes and display modes', async ({ page }, info) => {
  await page.addInitScript(() => localStorage.setItem('molr-ui-design-locale', 'en'));
  await page.goto('/#colors');
  const group = page.getByRole('group', { name: 'Font size', exact: true });
  for (const [value, label, expected] of [['small', 'Small', [12, 13, 14, 18, 22]], ['standard', 'Standard', [12, 14, 16, 20, 24]], ['large', 'Large', [14, 16, 18, 24, 28]]] as const) {
    await group.getByRole('radio', { name: label, exact: true }).check();
    await expect(page.locator('html')).toHaveAttribute('data-font-size', value);
    const combinations = await page.evaluate(() => {
      const probe = document.createElement('div'); probe.dataset.uiTheme = ''; document.body.append(probe);
      const output: number[][] = [];
      for (const theme of ['light', 'dark']) for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
        probe.dataset.theme = theme; probe.dataset.palette = palette;
        output.push(['xs', 'sm', 'base', 'lg', 'xl'].map(size => { probe.style.fontSize = `var(--text-${size})`; return parseFloat(getComputedStyle(probe).fontSize); }));
      }
      probe.remove(); return output;
    });
    for (const sizes of combinations) expect(sizes).toEqual([...expected]);
  }
  await page.screenshot({ path: info.outputPath('typography-large.png') });
});
