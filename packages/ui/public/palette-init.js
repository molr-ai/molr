// Opt-in bootstrap. The palette stylesheet precedes this script to avoid a flash on reload.
(() => {
  let palette = 'forest';
  try {
    const stored = localStorage.getItem(document.currentScript.dataset.storageKey);
    if (['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome'].includes(stored)) palette = stored;
  } catch {}
  const root = document.documentElement;
  root.dataset.palette = palette;
  const background = getComputedStyle(root).getPropertyValue('--color-page-background').trim();
  if (background) {
    root.style.backgroundColor = background;
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', background);
  }
})();
