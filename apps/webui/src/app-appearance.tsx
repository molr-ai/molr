import { useEffect, useState } from 'react';
import type { Settings } from '@molr/sdk';
import { useQuery } from './state';
import { useErrorToast } from './error-toasts';

/** Mounted only inside the authenticated account's data scope. */
export function AppAppearanceSync() {
  const { data } = useQuery<Settings>('/api/settings');
  const [error, setError] = useState<unknown>(null);
  const sync = window.molrApp?.setAppearance;
  useErrorToast(error);
  useEffect(() => {
    if (!sync || !data) return;
    let current = true;
    setError(null);
    void sync({ theme: data.Theme, palette: data.Palette || 'forest' }).catch(error => { if (current) setError(error); });
    return () => { current = false; };
  }, [sync, data?.Theme, data?.Palette]);
  return null;
}
