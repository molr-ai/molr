import { useSyncExternalStore } from 'react';

export type ThemePreference = 'system' | 'light' | 'dark';
export type Theme = Exclude<ThemePreference, 'system'>;
type Snapshot = { preference: ThemePreference; theme: Theme };

const storageKey = 'molr-theme';
const systemQuery = '(prefers-color-scheme: dark)';
const serverSnapshot: Snapshot = { preference: 'system', theme: 'light' };
let snapshot = serverSnapshot;
let currentPreference: ThemePreference | undefined;
let media: MediaQueryList | undefined;
const listeners = new Set<() => void>();

function readPreference(): ThemePreference {
  try {
    const stored = window.localStorage.getItem(storageKey);
    return stored === 'light' || stored === 'dark' ? stored : 'system';
  } catch {
    return currentPreference ?? 'system';
  }
}
function getSnapshot(): Snapshot {
  if (typeof window === 'undefined') return serverSnapshot;
  currentPreference ??= readPreference();
  media ??= window.matchMedia(systemQuery);
  const theme = currentPreference === 'system' ? media.matches ? 'dark' : 'light' : currentPreference;
  if (snapshot.preference !== currentPreference || snapshot.theme !== theme) snapshot = { preference: currentPreference, theme };
  return snapshot;
}
function applyTheme(theme: Theme) {
  const root = document.documentElement;
  if (root.dataset.theme !== theme) root.dataset.theme = theme;
  // Remove the pre-render bootstrap styling only when it is actually present.
  for (const property of ['background-color', 'color-scheme']) if (root.style.getPropertyValue(property)) root.style.removeProperty(property);
  const meta = document.querySelector('meta[name="theme-color"]');
  const style = getComputedStyle(root);
  const color = style.getPropertyValue('--color-page-background').trim() || style.getPropertyValue('--color-background').trim();
  if (meta && color && meta.getAttribute('content') !== color) meta.setAttribute('content', color);
}
function synchronize() {
  const previous = snapshot;
  const next = getSnapshot();
  applyTheme(next.theme);
  if (previous !== next) for (const listener of listeners) listener();
}
function synchronizeStorage(event: StorageEvent) {
  if (event.key !== storageKey && event.key !== null) return;
  currentPreference = readPreference();
  synchronize();
}
function subscribe(listener: () => void) {
  listeners.add(listener);
  if (listeners.size === 1) {
    currentPreference = readPreference();
    media ??= window.matchMedia(systemQuery);
    media.addEventListener('change', synchronize);
    window.addEventListener('storage', synchronizeStorage);
    synchronize();
  }
  return () => {
    listeners.delete(listener);
    if (!listeners.size) {
      media?.removeEventListener('change', synchronize);
      window.removeEventListener('storage', synchronizeStorage);
      media = undefined;
    }
  };
}
function changePreference(next: ThemePreference) {
  if (typeof window === 'undefined') return;
  currentPreference = next;
  try { window.localStorage.setItem(storageKey, next); } catch { /* In-memory changes still notify every consumer. */ }
  synchronize();
}

/** One browser subscription and one DOM writer, shared by every hook consumer. */
export function useTheme() {
  const state = useSyncExternalStore(subscribe, getSnapshot, () => serverSnapshot);
  return { ...state, changePreference };
}
