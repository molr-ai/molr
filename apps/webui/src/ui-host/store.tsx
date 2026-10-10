import { useMemo, createContext, useContext, useRef, useState } from 'react';
import type { ReactNode, SetStateAction } from 'react';
import { api, APIError } from '@molr/sdk';
import type { Settings } from '@molr/sdk';
import { defaultConfiguration, packageKey } from '@molr/ui-host';
import type { UIConfiguration, InstalledUI, UISelection } from '@molr/ui-host';
import { pluginAllowed } from '@molr/sdk/plugins';
import type { PluginInstallation } from '@molr/sdk/plugins';
import { isChatDraft } from '@molr/sdk/ui';
import type { ChatDraft } from '@molr/sdk/ui';
import { useQuery, useSession } from '../state';
import { officialPackage } from '../interfaces/official/manifest';
import { testPackage } from '../interfaces/test/manifest';

export const builtins: InstalledUI[] = [officialPackage, testPackage];
type Store = {
  pluginError: unknown; plugins: PluginInstallation[]; packages: InstalledUI[]; reloadPlugins: () => void;
  configuration: UIConfiguration; error: unknown; busy: boolean; loading: boolean;
  select: (selection: UISelection) => Promise<void>; reset: () => Promise<boolean>;
  drafts: Record<string, ChatDraft>; setDrafts: (value: SetStateAction<Record<string, ChatDraft>>) => void;
};
const Context = createContext<Store | null>(null);
export function UIHostProvider({ children }: { children: ReactNode }) {
  const { principal } = useSession(), query = useQuery<Settings>('/api/settings');
  const pluginQuery = useQuery<PluginInstallation[]>('/api/plugins');
  const plugins = pluginQuery.data ?? [];
  const packages = useMemo<InstalledUI[]>(() => [...builtins, ...plugins.flatMap(p => (p.Manifest.contributes.themes ?? []).filter(t => pluginAllowed(p,t.capabilities)).map(t => ({ runtime: 'frame' as const, plugin: { ID: p.ID, Digest: p.Digest, Kind: 'theme' as const, Contribution: t.id, Page: '' }, manifest: { id: p.ID + '.' + t.id.replaceAll('_', '-'), name: t.name, version: p.Manifest.version, apiVersion: 1 as const, pages: t.pages, permissions: [] } })))], [pluginQuery.data]);
  const [saved, setSaved] = useState<Settings | null>(null), [error, setError] = useState<unknown>(null), [busy, setBusy] = useState(false);
  const [recovering, setRecovering] = useState(false), writing = useRef(false);
  const current = saved && (!query.data || saved.Version > query.data.Version) ? saved : query.data;
  const selected = packages.find(item => packageKey(item.manifest) === current?.UITheme);
  const configuration: UIConfiguration = !recovering && selected ? { schema: 2, selected: { id: selected.manifest.id, version: selected.manifest.version } } : defaultConfiguration();
  const draftKey = `molr-ui-packages:v1:${principal!.OwnerID}:drafts`;
  const [drafts, updateDrafts] = useState<Record<string, ChatDraft>>(() => {
    try { const value = JSON.parse(sessionStorage.getItem(draftKey) || '{}'); return value && typeof value === 'object' && !Array.isArray(value) ? Object.fromEntries(Object.entries(value).filter((entry): entry is [string, ChatDraft] => /^[A-Za-z0-9_-]{1,128}$/.test(entry[0]) && isChatDraft(entry[1]))) : {}; } catch { return {}; }
  });
  async function select(selection: UISelection) {
    if (writing.current) return;
    if (!packages.some(item => packageKey(item.manifest) === packageKey(selection))) throw new APIError('invalid_argument');
    writing.current = true; setBusy(true); setError(null);
    try {
      const latest = await api.get<Settings>('/api/settings');
      const updated = await api.mutate<Settings>('/api/settings', { Settings: { ...latest, UITheme: packageKey(selection) } }, latest.Version);
      setSaved(updated); setRecovering(false);
    } catch (error) { setError(error); throw error; }
    finally { writing.current = false; setBusy(false); }
  }
  const store: Store = {
    pluginError: pluginQuery.error, plugins, packages, reloadPlugins: pluginQuery.reload,
    configuration, error: error || query.error, busy, loading: (!current && query.loading) || (!pluginQuery.data && pluginQuery.loading),
    setDrafts: value => updateDrafts(previous => {
      const next = typeof value === 'function' ? value(previous) : value;
      try { sessionStorage.setItem(draftKey, JSON.stringify(next)); } catch { /* Keep drafts in memory during page switches. */ }
      return next;
    }),
    drafts, select,
    reset: async () => {
      setRecovering(true);
      if (writing.current) return false;
      try { await select(defaultConfiguration().selected); return true; }
      catch { return false; } // Keep the current-session fallback when saving is unavailable.
    },
  };
  return <Context.Provider value={store}>{children}</Context.Provider>;
}
export function useUIHost() { const value = useContext(Context); if (!value) throw new Error('Missing UI host'); return value; }
