import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { updates as updateClient, type UpdateStatus } from '@molr/sdk';
import { AvatarNotice, Badge } from '@molr/ui';
import { useCopy, useConnection, useQuery, useSession } from '../state';
import { api } from '@molr/sdk';
import { buildInfo } from '../build-info';
import type { AppUpdateStatus } from '@molr/ui-host/app';
import { useAppInfo } from '../app-mode';
import { compareBuilds } from './build';
import './updates.css';

function useUpdateState() {
 const app = useAppInfo();
 const [desktop, setDesktop] = useState<AppUpdateStatus>();
 const [desktopError, setDesktopError] = useState(false);
 const refreshDesktop = useCallback(() => {
  const bridge = window.molrApp?.updates;
  if (bridge) void bridge.status().then(value => { setDesktop(value); setDesktopError(false); }).catch(() => setDesktopError(true));
 }, []);
 useEffect(() => {
  const bridge = window.molrApp?.updates;
  if (!bridge) return;
  const unsubscribe = bridge.onChanged(value => { setDesktop(value); setDesktopError(false); });
  refreshDesktop();
  const timer = window.setInterval(refreshDesktop, 300_000);
  const visible = () => { if (document.visibilityState === 'visible') refreshDesktop(); };
  document.addEventListener('visibilitychange', visible);
  return () => { unsubscribe(); clearInterval(timer); document.removeEventListener('visibilitychange', visible); };
 }, [refreshDesktop]);
 const checkDesktop = useCallback(async () => {
  try { const value = await window.molrApp?.updates.check(); setDesktop(value); setDesktopError(false); }
  catch { setDesktopError(true); }
 }, []);
 const query = useQuery<UpdateStatus>('/api/updates', false, 300_000);
 const { connection } = useConnection();
 const reloadRef = useRef(query.reload); reloadRef.current = query.reload;
 const reload = useCallback(() => { refreshDesktop(); return reloadRef.current(); }, [refreshDesktop]);
 const [checking, setChecking] = useState(false), [checkError, setCheckError] = useState(false);
 const pending = useRef<Promise<void> | null>(null);
 useEffect(() => { if (connection === 'recovered') reload(); }, [connection, reload]);
 const check = useCallback((automatic = false) => {
  if (pending.current) return pending.current;
  setChecking(true); setCheckError(false);
  pending.current = updateClient(api).check(automatic).then(() => { reload(); }).catch(() => { setCheckError(true); reload(); }).finally(() => { setChecking(false); pending.current = null; });
  return pending.current;
 }, [reload]);
 const managed = app?.connection.kind === 'managed-local';
 const localPending = managed && !!desktop?.local?.pending && query.data?.current.version !== desktop.local.targetVersion;
 const webuiConsistency = compareBuilds(buildInfo, query.data?.current);
 const desktopConsistency = window.molrApp ? (app ? compareBuilds(app.build, query.data?.current) : 'unknown') : undefined;
 const components = [webuiConsistency, ...(desktopConsistency ? [desktopConsistency] : [])];
 const consistency = (['version_mismatch', 'commit_mismatch', 'unknown', 'consistent'] as const).find(value => components.includes(value))!;
 const differs = (value: string | undefined) => value === 'version_mismatch' || value === 'commit_mismatch';
 const mismatch = differs(consistency);
 const available = desktop?.comparison === 'available' || localPending || (!managed && query.data?.comparison === 'available');
 return { ...query, app, desktop, desktopError, checkDesktop, managed, localPending, mismatch, available, webuiConsistency, desktopConsistency, reload, unreachable: !!query.error || connection === 'disconnected' || connection === 'failed', checking, checkError, check, consistency };
}
const UpdateContext = createContext<ReturnType<typeof useUpdateState> | null>(null);
export function UpdateProvider({ children }: { children: ReactNode }) { const state = useUpdateState(); return <UpdateContext.Provider value={state}>{children}</UpdateContext.Provider>; }
export function useUpdates() { return useContext(UpdateContext)!; }
export function useUpdateNotice() {
 const updates = useUpdates(), { principal } = useSession(), { locale } = useCopy();
 const mismatch = updates.mismatch;
 const available = updates.desktop?.comparison === 'available' || updates.localPending || (!updates.managed && principal?.User?.Administrator && updates.data?.comparison === 'available');
 if (!mismatch && !available) return undefined;
 return updates.localPending ? locale === 'zh' ? '本地服务更新待完成' : 'Local service update pending' : mismatch ? locale === 'zh' ? '版本不一致' : 'Version mismatch' : locale === 'zh' ? '有更新' : 'Update available';
}
export function UpdateBadge({ compact = false }: { compact?: boolean }) {
 const label = useUpdateNotice();
 if (!label) return null;
 if (compact) return <AvatarNotice label={label} className="update-badge" />;
 return <span className="update-badge" title={label} aria-label={label}><Badge tone="warning" size="sm" dot={false}>{label}</Badge></span>;
}
