import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api, APIError } from '@molr/sdk';
import type { WorkspaceListing } from '@molr/sdk';
import { poll, retryDelay } from '../../../../polling';
import { shareJSON } from '../../../../share-json';
import { workspaceReader } from './read';

export function useWorkspaceDirectory(assistantID: string, directory: string, busy: boolean, refreshInterval: number | false = 5000) {
  const [listing, setListing] = useState<WorkspaceListing>();
  const [loading, setLoading] = useState(true), [error, setError] = useState<unknown>();
  const current = useRef({ listing, busy });
  current.current = { listing, busy };
  const reader = useMemo(workspaceReader, []);
  const polling = useRef<ReturnType<typeof poll> | undefined>(undefined), halted = useRef(false);
  const read = useCallback((more: boolean, replace: boolean, signal?: AbortSignal) => reader.run(async signal => {
    const old = more ? current.current.listing : undefined;
    try {
      const next = await api.workspace.list(assistantID, directory, old?.NextOffset ?? 0, old?.Version ?? '', signal);
      if (signal.aborted) return;
      setListing(previous => shareJSON(previous, old ? { ...next, Entries: [...old.Entries, ...next.Entries] } : next) as WorkspaceListing);
      setError(undefined);
    } catch (error) {
      if (!signal.aborted) {
        setError(error);
        if (error instanceof APIError && error.code === 'conflict') setListing(undefined);
        // A removed directory may be recreated without any UI action.
        if (error instanceof APIError && error.code === 'not_found') return;
      }
      throw error;
    } finally {
      if (!signal.aborted) setLoading(false);
    }
  }, replace, signal), [assistantID, directory, reader]);
  useEffect(() => {
    setLoading(true); setListing(undefined); current.current.listing = undefined;
    const loop = poll(async signal => {
      const { listing, busy } = current.current;
      // Keep loaded pages and scroll until the user explicitly refreshes them.
      try {
        if (!busy && (!listing || !listing.Truncated && listing.Entries.length <= 100)) await read(false, false, signal);
      } catch (error) {
        halted.current = retryDelay(error, 1) === false;
        throw error;
      }
      return refreshInterval;
    });
    polling.current = loop; halted.current = false;
    const focus = () => loop.request();
    if (refreshInterval !== false) window.addEventListener('focus', focus);
    return () => { loop.stop(); polling.current = undefined; reader.cancel(); window.removeEventListener('focus', focus); };
  }, [read, reader, refreshInterval]);
  const loadList = useCallback((more = false) => read(more, true).then(() => {
    if (halted.current) { halted.current = false; polling.current?.request(true); }
  }).catch(() => {}), [read]);
  return { listing, loading, error, loadList };
}
