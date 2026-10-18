import { useEffect, useState } from 'react';
import { readDesktopCatalog, type DesktopCatalog } from './desktop-releases';

export function useDesktopRelease(source: string) {
  const [catalog, setCatalog] = useState<DesktopCatalog>();
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const abort = new AbortController(); setError(false); setCatalog(undefined);
    fetch(`${source}/desktop/catalog.json`, { signal: abort.signal, cache: 'no-cache' }).then(async response => {
      if (response.status === 404) return { schema: 1, channels: { stable: null, preview: null, nightly: null } };
      if (!response.ok) throw new Error('Unavailable');
      return response.json();
    }).then(value => setCatalog(readDesktopCatalog(value))).catch(() => { if (!abort.signal.aborted) setError(true); });
    return () => abort.abort();
  }, [source, attempt]);
  return { release: catalog?.channels.stable, loading: !catalog && !error, error, retry: () => setAttempt(x => x + 1) };
}
