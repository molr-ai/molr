import { useSyncExternalStore } from 'react';
import values from '../breakpoints.json';

/** Shared width thresholds; chatPanel measures the available chat workspace. */
export const breakpoints = Object.freeze(values);
export type Breakpoint = keyof typeof breakpoints;

function createBreakpoint(width: number) {
  const query = `(max-width: ${width}px)`;
  let media: MediaQueryList | undefined;
  const listeners = new Set<() => void>();
  const read = () => typeof window !== 'undefined' && (media ??= window.matchMedia(query)).matches;
  const notify = () => { for (const listener of listeners) listener(); };
  return {
    query,
    read,
    subscribe(listener: () => void) {
      if (typeof window === 'undefined') return () => {};
      if (!listeners.size) {
        media = window.matchMedia(query);
        media.addEventListener('change', notify);
      }
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
        if (!listeners.size) { media?.removeEventListener('change', notify); media = undefined; }
      };
    },
  };
}
const stores = Object.fromEntries(Object.entries(breakpoints).map(([name, width]) => [name, createBreakpoint(width)])) as Record<Breakpoint, ReturnType<typeof createBreakpoint>>;
const serverSnapshot = () => false;

export function useBreakpoint(name: Breakpoint) {
  const store = stores[name];
  return useSyncExternalStore(store.subscribe, store.read, serverSnapshot);
}
