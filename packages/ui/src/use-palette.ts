import { useCallback, useEffect, useLayoutEffect, useState } from 'react';
import type { Theme } from './use-theme';
import { palettes, paletteBrandColors, paletteAssetBase } from './palettes';
import type { Palette } from './palettes';

function readPalette(storageKey: string): Palette {
  try {
    const stored = localStorage.getItem(storageKey);
    return palettes.find(item => item.id === stored)?.id ?? 'forest';
  } catch { return 'forest'; }
}

export function usePalettePreference(storageKey: string, theme: Theme, brandBase = '/brand') {
  const [palette, setPalette] = useState(() => readPalette(storageKey));
  useLayoutEffect(() => {
    const root = document.documentElement;
    root.dataset.palette = palette;
    const colors = paletteBrandColors[palette];
    root.style.setProperty('--color-logo-ink', colors.ink);
    root.style.setProperty('--color-logo-paper', colors.paper);
    document.querySelector('link[rel="icon"]')?.setAttribute('href', `${paletteAssetBase(palette, brandBase)}/molr-icon.svg`);
    // The shared appearance hook also updates this metadata on light/dark changes.
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', getComputedStyle(root).getPropertyValue('--color-background').trim());
  }, [palette, theme, brandBase]);
  useEffect(() => {
    const synchronize = (event: StorageEvent) => {
      if (event.key === storageKey || event.key === null) setPalette(readPalette(storageKey));
    };
    window.addEventListener('storage', synchronize);
    return () => window.removeEventListener('storage', synchronize);
  }, [storageKey]);
  const changePalette = useCallback((next: Palette) => {
    setPalette(next);
    try { localStorage.setItem(storageKey, next); } catch { /* Keep switching available without storage. */ }
  }, [storageKey]);
  return { palette, changePalette };
}
