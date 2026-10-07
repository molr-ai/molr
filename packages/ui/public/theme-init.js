(() => {
  let preference = 'system';
  try {
    const stored = localStorage.getItem('molr-theme');
    if (stored === 'light' || stored === 'dark') preference = stored;
  } catch {}
  const theme = preference === 'system' ? (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light') : preference;
  document.documentElement.dataset.theme = theme;
  const style = getComputedStyle(document.documentElement);
  const background = style.getPropertyValue('--color-page-background').trim() || style.getPropertyValue('--color-background').trim();
  document.documentElement.style.colorScheme = theme;
  if (background) {
    document.documentElement.style.backgroundColor = background;
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', background);
  }
})();
