import { useCallback, useEffect, useRef, useState } from 'react';
import { poll } from '../../../polling';
import { api, APIError } from '@molr/sdk';
import type { DebugCleanupEvent, DebugRun, TrajectoryEntry, TrajectoryPage } from '@molr/sdk';
import { useTopicVersion } from '../../../state';
import { mergeEntries, mergeRuns } from './trajectory';

type Snapshot = { entries: TrajectoryEntry[]; runs: DebugRun[]; older: string; head: string; runsCursor: string; resetAt: string | null; cleanup: DebugCleanupEvent | null; loading: boolean; loadingOlder: boolean; error: unknown };
const initial = (): Snapshot => ({ entries: [], runs: [], older: '', head: '', runsCursor: '', resetAt: null, cleanup: null, loading: true, loadingOlder: false, error: null });
// One request queue owns the loaded window. Live backfill cannot race an older
// page, and a reconnect drains every new page rather than replacing the tail.
export function useTrajectory(assistantID: string) {
  const revision = useTopicVersion('debug');
  const [snapshot, setSnapshot] = useState<Snapshot>(initial);
  const refresh = useRef<() => void>(() => {}), older = useRef<() => void>(() => {});
  useEffect(() => {
    const controller = new AbortController(); let current = initial(); let queue = Promise.resolve(); let olderQueued = false;
    setSnapshot(current);
    const commit = (patch: Partial<Snapshot>) => { if (controller.signal.aborted) return; current = { ...current, ...patch }; setSnapshot(current); };
    const request = (cursor = '', after = '') => {
      const query = new URLSearchParams({ assistant_id: assistantID, limit: '100' });
      if (current.runsCursor) query.set('runs_cursor', current.runsCursor);
      if (cursor) query.set('cursor', cursor); if (after) query.set('after', after);
      return api.get<TrajectoryPage>(`/api/debug/trajectory?${query}`, controller.signal);
    };
    const receive = (page: TrajectoryPage) => {
      const runs = mergeRuns(current.runs, page.Runs, page.RunsReset);
      const entries = !page.RunsReset && !page.Items.length ? current.entries : mergeEntries(current.entries, page.Items, runs);
      commit({ entries, runs, runsCursor: page.RunsCursor, resetAt: page.ContextResetAt, cleanup: page.Cleanup, error: null });
    };
    const enqueue = (job: () => Promise<void>) => {
      const result = queue.then(async () => { if (controller.signal.aborted) return; try { await job(); } catch (error) {
        if (controller.signal.aborted) return;
        const denied = error instanceof APIError && [401, 403].includes(error.status);
        commit({ ...(denied ? initial() : {}), error, loading: false, loadingOlder: false });
        throw error;
      } });
      queue = result.catch(() => {});
      return result;
    };
    const loop = poll(async () => {
      await enqueue(async () => {
        if (!current.head) {
          const page = await request(); receive(page); commit({ older: page.NextCursor, head: page.HeadCursor, loading: false }); return;
        }
        let after = current.head;
        do {
          const page = await request('', after); if (controller.signal.aborted) return;
          receive(page); commit({ head: page.HeadCursor, loading: false }); after = page.NextCursor;
        } while (after);
      });
      return 30_000;
    }, false);
    refresh.current = () => loop.request(true);
    older.current = () => {
      if (olderQueued || !current.older) return; olderQueued = true;
      void enqueue(async () => {
        commit({ loadingOlder: true });
        try { const page = await request(current.older); receive(page); commit({ older: page.NextCursor }); }
        finally { olderQueued = false; commit({ loadingOlder: false }); }
      }).catch(() => {});
    };
    return () => { loop.stop(); controller.abort(); refresh.current = () => {}; older.current = () => {}; };
  }, [assistantID]);
  useEffect(() => { refresh.current(); }, [revision, assistantID]);
  return { ...snapshot, reload: useCallback(() => refresh.current(), []), loadOlder: useCallback(() => older.current(), []) };
}
