import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { updates as updateClient, type UpdateStatus } from '@molr/sdk';
import { Badge } from '@molr/ui';
import { useCopy, useData, useQuery, useSession } from '../state';
import { api } from '../api/client';
import { buildInfo } from '../build-info';
import { compareBuilds } from './build';
import './updates.css';

function useUpdateState() {
 const query = useQuery<UpdateStatus>('/api/updates', false, 300_000);
 const { connection } = useData();
 const reloadRef = useRef(query.reload); reloadRef.current = query.reload;
 const reload = useCallback(() => reloadRef.current(), []);
 const [checking, setChecking] = useState(false), [checkError, setCheckError] = useState(false), [observedAt, setObservedAt] = useState<string>();
 const pending = useRef<Promise<void> | null>(null);
 useEffect(() => { if (query.data && !query.error) setObservedAt(new Date().toISOString()); }, [query.data, query.error]);
 useEffect(() => { if (connection === 'recovered') reload(); }, [connection, reload]);
 const check = useCallback((automatic = false) => {
  if (pending.current) return pending.current;
  setChecking(true); setCheckError(false);
  pending.current = updateClient(api).check(automatic).then(() => { reload(); }).catch(() => { setCheckError(true); reload(); }).finally(() => { setChecking(false); pending.current = null; });
  return pending.current;
 }, [reload]);
 return { ...query, reload, unreachable: !!query.error || connection === 'disconnected' || connection === 'failed', observedAt, checking, checkError, check, consistency: compareBuilds(buildInfo, query.data?.current) };
}
const UpdateContext = createContext<ReturnType<typeof useUpdateState> | null>(null);
export function UpdateProvider({ children }: { children: ReactNode }) { const state = useUpdateState(); return <UpdateContext.Provider value={state}>{children}</UpdateContext.Provider>; }
export function useUpdates() { return useContext(UpdateContext)!; }
export function UpdateBadge({ compact = false }: { compact?: boolean }) {
 const updates = useUpdates(), { principal } = useSession(), { locale } = useCopy();
 const mismatch = updates.consistency === 'version_mismatch' || updates.consistency === 'commit_mismatch';
 const available = principal?.User?.Administrator && updates.data?.comparison === 'available';
 if (!mismatch && !available) return null;
 const label = mismatch ? locale === 'zh' ? '版本不一致' : 'Version mismatch' : locale === 'zh' ? '有更新' : 'Update available';
 return <span className={compact ? 'update-badge update-badge--compact' : 'update-badge'} title={label} aria-label={label}><Badge tone="warning" size="sm" dot={false}>{compact ? '!' : label}</Badge></span>;
}
