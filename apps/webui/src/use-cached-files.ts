import { useCallback, useMemo, useSyncExternalStore } from 'react';
import { useData } from './state';

export function useCachedFiles(ids: string[], preview = true) {
  const { files: cache } = useData();
  const key = JSON.stringify(ids);
  const stableIDs = useMemo(() => JSON.parse(key) as string[], [key]);
  const subscribe = useCallback((listener: () => void) => {
    const releases = [...new Set(stableIDs)].map(id => cache.subscribe(id, listener, preview));
    return () => releases.forEach(release => release());
  }, [cache, stableIDs, preview]);
  const snapshot = useCallback(() => cache.version(stableIDs), [cache, stableIDs]);
  useSyncExternalStore(subscribe, snapshot);
  const states = stableIDs.map(id => cache.snapshot(id));
  const reload = useCallback(() => cache.retry(stableIDs), [cache, stableIDs]);
  return {
    files: states.flatMap(state => state.file ? [state.file] : []),
    attachments: states.flatMap(state => state.file ? [{
      id: state.file.ID, name: state.file.Name, size: state.file.Size, type: state.file.MediaType,
      url: `/api/files/${encodeURIComponent(state.file.ID)}/content`,
      previewUrl: state.previewUrl ?? (state.error ? 'data:image/png;base64,' : undefined),
    }] : []),
    loading: states.some(state => state.loading && !state.file),
    error: states.find(state => state.error)?.error,
    reload,
  };
}
