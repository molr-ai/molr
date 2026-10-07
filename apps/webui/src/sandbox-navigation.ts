import { useState } from 'react';
import { useSession } from './state';

// Remember only navigation IDs, scoped to this browser tab, Runtime origin and
// owner. No resource names, credentials or API responses are stored here.
export function useSandboxPreference<T extends string>(name: string, fallback: T, validate: (value: string) => boolean) {
  const { principal } = useSession();
  const key = `molr-sandbox-nav:${principal?.OwnerID ?? 'anonymous'}:${name}`;
  function read(): T { try { const value = sessionStorage.getItem(key); return value && validate(value) ? value as T : fallback; } catch { return fallback; } }
  const [state, setState] = useState(() => ({ key, value: read() }));
  const value = state.key === key ? state.value : read();
  function set(value: T) { if (!validate(value)) return; setState({ key, value }); try { sessionStorage.setItem(key, value); } catch { /* in-memory navigation still works */ } }
  return [value, set] as const;
}
