import { test, expect } from '@playwright/test';

const gallery = '/tests/fixtures/full-access-cards.html?lang=en';
test.beforeEach(async ({ page }) => {
  await page.goto(gallery);
  await expect(page.locator('.permission-request-card')).toHaveCount(16);
  await expect(page.locator('#any-arguments .full-access-program__path')).toHaveText('/usr/bin/ffmpeg');
});

test('scopes use distinct titles and exact arguments stay in order', async ({ page }) => {
  await expect(page.locator('#unrestricted .permission-request-card')).toHaveAccessibleName('Unrestricted host access');
  await expect(page.locator('#any-arguments .permission-request-card')).toHaveAccessibleName('Run specified programs');
  await expect(page.locator('#any-arguments .full-access-program')).toContainText('Any arguments');
  await expect(page.locator('#any-arguments .full-access-arguments')).toHaveCount(0);
  await expect(page.locator('#exact-arguments .full-access-arguments code')).toHaveText(['"-i"', '"/work/source recording.mp4"', '"-c:v"', '"libx264"', '"/work/output.mp4"']);
  await expect(page.locator('#multiple-programs .full-access-program')).toHaveCount(2);
});

test('no arguments, an empty argument and whitespace remain distinguishable', async ({ page }) => {
  await expect(page.locator('#no-arguments .full-access-program')).toContainText('No arguments');
  await expect(page.locator('#no-arguments .full-access-arguments')).toHaveCount(0);
  const values = await page.locator('#special-arguments .full-access-arguments code').allTextContents();
  expect(values).toEqual(['"%s\\n"', '""', '"  keep spaces  "', '"first line\\nsecond line"']);
  await expect(page.locator('#special-arguments .full-access-arguments__empty')).toHaveText('Empty argument');
});

test('approval displays only the granted scope and rejection has no approval action', async ({ page }) => {
  const narrowed = page.locator('#approved-narrowed .permission-request-card');
  await expect(narrowed).toHaveAccessibleName('Run specified programs');
  await expect(narrowed.locator('.full-access-arguments code')).toHaveText(['"-version"']);
  await expect(narrowed.locator('.full-access-summary--unrestricted')).toHaveCount(0);
  await expect(page.locator('#approved-permanent')).toContainText('No expiry');
  await expect(page.locator('#rejected')).toContainText('Rejected');
  await expect(page.locator('#rejected').getByRole('button', { name: 'Review permission', exact: true })).toHaveCount(0);
});

test('missing scope stays explicit until a reviewer selects it', async ({ page }) => {
  const card = page.locator('#missing-scope');
  await expect(card).toContainText('Access scope not declared');
  await card.getByRole('button', { name: 'Review permission', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Review permission', exact: true });
  const approve = dialog.getByRole('button', { name: 'Approve permission', exact: true });
  await expect(approve).toBeDisabled();
  await expect(dialog.getByRole('radio', { name: 'Unrestricted', exact: true })).not.toBeChecked();
  await dialog.getByRole('radio', { name: 'Specified programs', exact: true }).click();
  await dialog.getByLabel('Program path 1', { exact: true }).fill('/usr/bin/uptime');
  await approve.click();
  await expect(dialog).toBeHidden();
  await expect(card).toContainText('Approved');
  await expect(card.locator('.full-access-program__path')).toHaveText('/usr/bin/uptime');
  await expect(card.locator('.full-access-program')).toContainText('No arguments');
});

test('grouped scopes retain type labels', async ({ page }) => {
  await expect(page.locator('#grouped .permission-scope')).toHaveCount(2);
  await expect(page.locator('#grouped .permission-scope--full-access .permission-scope__location')).toContainText('Full access');

});

test('long paths and argument values fit every palette and display mode', async ({ page }) => {
  for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
    for (const theme of ['light', 'dark']) {
      await page.evaluate(({ palette, theme }) => {
        document.documentElement.dataset.palette = palette;
        document.documentElement.dataset.theme = theme;
      }, { palette, theme });
      const overflow = await page.evaluate(() => {
        const pageOverflow = document.documentElement.scrollWidth > window.innerWidth;
        const blocks = [...document.querySelectorAll<HTMLElement>('.permission-request-card, .full-access-program, .full-access-arguments code')];
        return pageOverflow || blocks.some(node => node.scrollWidth > node.clientWidth + 1);
      });
      expect(overflow, `${palette}/${theme}`).toBe(false);
    }
  }
});

test('one-use approval preserves the option and used records stay visible', async ({ page }) => {
 const pending=page.locator('#once-pending');
 await expect(pending.locator('.permission-use')).toContainText('Use once');
 await pending.getByRole('button',{name:'Review permission',exact:true}).click();
 const dialog=page.getByRole('dialog',{name:'Review permission',exact:true});
 await expect(dialog.getByRole('switch',{name:'Use once',exact:true})).toBeChecked();
 await dialog.getByRole('button',{name:'Approve permission',exact:true}).click();
 await expect(dialog).toBeHidden();
 await expect(pending).toContainText('Approved');
 await expect(pending.locator('.permission-use')).toContainText('Use once');
 const used=page.locator('#once-used');
 await expect(used.locator('.permission-use')).toContainText('Use once (used)');
 await used.getByText('Use record',{exact:true}).click();
 await expect(used.locator('.permission-use__record code')).toHaveText('preview-used-operation');
});
