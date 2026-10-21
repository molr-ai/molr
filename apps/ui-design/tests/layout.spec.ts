import { test, expect } from '@playwright/test';
import { readLayout } from '../../../scripts/test-support/layout.ts';

test('related layout rectangles retain their offsets during scrolling and expose real misalignment', async ({ page }) => {
  await page.setContent(`<style>
    body { margin: 0; height: 6000px; }
    main { position: absolute; top: 2000px; display: flex; gap: 20px; }
    button { box-sizing: border-box; width: 120px; height: 64px; }
  </style><main><button>Download</button><button>Docs</button></main>`);
  const download = page.getByRole('button', { name: 'Download', exact: true });
  const docs = page.getByRole('button', { name: 'Docs', exact: true });
  await page.evaluate(() => {
    let alternate = false;
    const scroll = () => {
      window.scrollTo(0, (alternate = !alternate) ? 1800 : 1900);
      requestAnimationFrame(scroll);
    };
    scroll();
  });
  for (let i = 0; i < 30; i++) {
    const [left, right] = await readLayout(download, docs);
    expect(left.y).toBe(right.y);
    expect(right.x - (left.x + left.width)).toBe(20);
    expect(left.height).toBe(64);
  }
  await docs.evaluate(element => { element.style.transform = 'translateY(1px)'; });
  const [left, right] = await readLayout(download, docs);
  expect(right.y - left.y).toBe(1);
});
