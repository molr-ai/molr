import { expect, test, type Route } from '@playwright/test';
import { openSandboxDetails } from './sandbox-navigation';

test.afterEach(async ({ page }) => {
  const csrf = (await page.context().cookies()).find(cookie => cookie.name.includes('csrf'))?.value;
  if (csrf) await page.request.post('/api/auth/logout', { headers: { Origin: new URL(page.url()).origin, 'X-CSRF-Token': csrf }, data: {} });
});

test('refresh finishes its current revolution and resumes without resetting its angle', async ({ page }) => {
  await page.goto('/#/resources');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('tab', { name: /^(Sandboxes|沙盒)$/ }).click();
  await openSandboxDetails(page);
  const button = page.getByRole('button', { name: /^(Check again|重新检查)$/ });
  const icon = button.locator('svg');
  await expect(button).toBeEnabled();

  // Keep interception installed while refresh also starts unrelated list reads.
  // Removing/reinstalling one-shot routes can strand those reads in Chromium.
  let respond: ((route: Route) => Promise<void>) | undefined;
  let releasePending = () => {};
  await page.route('**/api/resources/check-sandbox', route => {
    const handle = respond;
    respond = undefined;
    if (!handle) throw new Error('Unexpected sandbox check request');
    return handle(route);
  });
  async function start(fail = false) {
    let release!: () => void, arrived!: () => void;
    const gate = new Promise<void>(resolve => { release = resolve; });
    const started = new Promise<void>(resolve => { arrived = resolve; });
    releasePending = release;
    respond = async route => {
      arrived();
      await gate;
      await route.fulfill(fail ? { status: 503, json: { error: { code: 'unavailable', message: 'Refresh failed' } } } : { json: {} });
    };
    await button.click();
    await started;
    await expect(button).toBeDisabled();
    await expect(button).toHaveAttribute('aria-busy', 'true');
    await expect(icon).toHaveCount(1);
    await expect(button.locator('.ui-spinner')).toHaveCount(0);
    return release;
  }

  try {
    for (const [elapsed, fail] of [[250, false], [1050, false], [250, true]] as const) {
      const release = await start(fail);
      const animation = await icon.evaluateHandle(element => element.getAnimations()[0]);
      await animation.evaluate((value, time) => { value.pause(); value.currentTime = time; }, elapsed);
      const angle = await icon.evaluate(element => getComputedStyle(element).transform);
      release();
      await expect(button).toBeEnabled();
      // Completing the request must not remove the animation or reset a partial turn.
      expect(await icon.evaluate(element => getComputedStyle(element).transform)).toBe(angle);
      expect(await icon.evaluate(element => element.getAnimations().length)).toBe(1);
      const stoppedAt = await animation.evaluate(async value => { value.play(); await value.finished; return value.currentTime; });
      expect(stoppedAt).toBe(Math.ceil(elapsed / 800) * 800);
      await expect.poll(() => icon.evaluate(element => element.getAnimations().length)).toBe(0);
      await animation.dispose();
    }

    const release = await start();
    const animation = await icon.evaluateHandle(element => element.getAnimations()[0]);
    await animation.evaluate(value => { value.pause(); value.currentTime = 350; });
    release();
    await expect(button).toBeEnabled();
    const angle = await icon.evaluate(element => getComputedStyle(element).transform);
    const releaseNext = await start();
    expect(await icon.evaluate(element => getComputedStyle(element).transform)).toBe(angle);
    expect(await animation.evaluate(value => value.effect?.getTiming().iterations)).toBe(Infinity);
    releaseNext();
    await expect(button).toBeEnabled();
    expect(await animation.evaluate(async value => { value.play(); await value.finished; return value.currentTime; })).toBe(800);
    await expect.poll(() => icon.evaluate(element => element.getAnimations().length)).toBe(0);
    await animation.dispose();

    await page.emulateMedia({ reducedMotion: 'reduce' });
    const releaseReduced = await start();
    expect(await icon.evaluate(element => element.getAnimations().length)).toBe(0);
    releaseReduced();
    await expect(button).toBeEnabled();
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    expect(await icon.evaluate(element => element.getAnimations().length)).toBe(0);
  } finally {
    releasePending();
    await page.unrouteAll({ behavior: 'wait' });
  }
});
