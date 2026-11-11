import { test, expect } from '@playwright/test';

type TransitionWindow = typeof window & {
  themeAnimation: Promise<{ theme: string | undefined; clip: string; name: string; radius: string; diagonal: number }>;
};

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('molr-theme', 'light'));
  await page.goto('/tests/fixtures/platform-review.html');
});

test('dark expands from bottom left and contracts toward bottom right', async ({ page }) => {
  for (const [button, theme, pseudo, origin] of [
    ['Dark B', 'dark', 'new', '0px'], ['Light A', 'light', 'old', '100%'],
  ]) {
    await page.evaluate(({ pseudo }) => {
      const start = document.startViewTransition.bind(document);
      document.startViewTransition = (...args) => {
        document.startViewTransition = start;
        const transition = start(...args);
        // Capture in the browser at readiness: Playwright round trips can outlast
        // the 550ms animation on a loaded CI runner.
        (window as TransitionWindow).themeAnimation = transition.ready.then(() => {
          const root = document.documentElement;
          const style = getComputedStyle(root, `::view-transition-${pseudo}(root)`);
          return { theme: root.dataset.themeTransition, clip: style.clipPath, name: style.animationName, radius: root.style.getPropertyValue('--theme-transition-radius'), diagonal: Math.hypot(innerWidth, innerHeight) };
        });
        return transition;
      };
    }, { pseudo });
    await page.getByRole('button', { name: button, exact: true }).click();
    const animation = await page.evaluate(() => (window as TransitionWindow).themeAnimation);
    expect(animation.theme).toBe(theme);
    expect(animation.name).toBe(theme === 'dark' ? 'theme-dark-expand' : 'theme-dark-contract');
    expect(animation.clip).toContain(`at ${origin}`);
    expect(parseFloat(animation.radius)).toBeGreaterThanOrEqual(animation.diagonal);
    await expect(page.getByRole('status', { name: 'Theme C' })).toHaveText(`${theme}:${theme}`);
    await expect(page.locator('html')).not.toHaveAttribute('data-theme-transition');
  }
});

for (const mode of ['system', 'account', 'unsupported']) test(`${mode} skips animation`, async ({ page }) => {
  if (mode === 'system') await page.emulateMedia({ reducedMotion: 'reduce' });
  else await page.evaluate(mode => {
    if (mode === 'account') document.documentElement.dataset.reducedMotion = 'true';
    else Object.defineProperty(document, 'startViewTransition', { value: undefined });
  }, mode);
  await page.getByRole('button', { name: 'Dark B', exact: true }).click();
  await expect(page.getByRole('status', { name: 'Theme C' })).toHaveText('dark:dark');
  await expect(page.locator('html')).not.toHaveAttribute('data-theme-transition');
});

test('rapid changes retain the final preference and release snapshots', async ({ page }) => {
  await page.evaluate(() => {
    for (const name of ['Dark B', 'Light A', 'Dark B']) (Array.from(document.querySelectorAll('button')).find(b => b.textContent === name)!).click();
  });
  await expect(page.getByRole('status', { name: 'Theme C' })).toHaveText('dark:dark');
  await expect(page.locator('html')).not.toHaveAttribute('data-theme-transition');
  expect(await page.evaluate(() => localStorage.getItem('molr-theme'))).toBe('dark');
});
