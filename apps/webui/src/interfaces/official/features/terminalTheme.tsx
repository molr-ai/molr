import { useLayoutEffect, useSyncExternalStore } from 'react';
import type { RefObject } from 'react';
import { IconButton, Select } from '@molr/ui';
import { SunMoon } from 'lucide-react';

const key = 'molr.terminal-theme', event = 'molr:terminal-theme';
type Theme = 'app' | 'light' | 'dark';
function snapshot(): Theme {
  try { const value = localStorage.getItem(key); return value === 'light' || value === 'dark' ? value : 'app'; } catch { return 'app'; }
}
function subscribe(update: () => void) {
  window.addEventListener('storage', update); window.addEventListener(event, update);
  return () => { window.removeEventListener('storage', update); window.removeEventListener(event, update); };
}
function usePreference() { return useSyncExternalStore(subscribe, snapshot, () => 'app' as Theme); }

// Scope the full terminal window, including chrome, without changing the app theme.
export function useTerminalTheme(ref: RefObject<HTMLDivElement | null>, window: boolean) {
  const preference = usePreference();
  useLayoutEffect(() => {
    const target = window ? ref.current?.closest<HTMLElement>('.terminal-drawer') : ref.current?.closest<HTMLElement>('.terminal-session');
    if (!target || !window && target.closest('.terminal-drawer')) return;
    const root = document.documentElement;
    const apply = () => {
      target.dataset.uiTheme = '';
      target.dataset.theme = preference === 'app' ? root.dataset.theme ?? 'light' : preference;
      target.dataset.palette = root.dataset.palette ?? 'forest';
    };
    apply(); const observer = new MutationObserver(apply);
    observer.observe(root, { attributes: true, attributeFilter: ['data-theme', 'data-palette'] });
    return () => { observer.disconnect(); delete target.dataset.uiTheme; delete target.dataset.theme; delete target.dataset.palette; };
  }, [preference, ref, window]);
}
export function TerminalThemePicker({ zh }: { zh: boolean }) {
  const preference = usePreference(), label = zh ? '终端配色' : 'Terminal appearance';
  return <Select<Theme> className="terminal-theme-picker" label={label} hideLabel size="sm" align="end" value={preference} onValueChange={value => {
    try { localStorage.setItem(key, value); } catch { return; }
    window.dispatchEvent(new Event(event));
  }} trigger={<IconButton label={label}><SunMoon size={16} aria-hidden="true"/></IconButton>} options={[
    { value: 'app', label: zh ? '跟随应用' : 'Follow app' },
    { value: 'light', label: zh ? '浅色' : 'Light' },
    { value: 'dark', label: zh ? '深色' : 'Dark' },
  ]}/>;
}
