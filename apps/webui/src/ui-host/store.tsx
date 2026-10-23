import { useMemo, createContext, useContext, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { api, APIError } from '@molr/sdk';
import type { Settings } from '@molr/sdk';
import { defaultConfiguration, packageKey } from '@molr/ui-host';
import type { UIConfiguration, InstalledUI, UISelection } from '@molr/ui-host';
import { pluginAllowed } from '@molr/sdk/plugins';
import type { PluginInstallation } from '@molr/sdk/plugins';
import { useQuery } from '../state';
import { officialPackage } from '../interfaces/official/manifest';

export const builtins: InstalledUI[] = [officialPackage];
type Store = {
  pluginError: unknown; plugins: PluginInstallation[]; packages: InstalledUI[]; reloadPlugins: () => void;
  configuration: UIConfiguration; error: unknown; busy: boolean; loading: boolean;
  select: (selection: UISelection) => Promise<void>; reset: () => Promise<boolean>;
};
const Context = createContext<Store | null>(null);
export function UIHostProvider({ children }: { children: ReactNode }) {
  const query = useQuery<Settings>('/api/settings');
  const pluginsVisible = query.data?.PluginsVisible === true;
  const pluginQuery = useQuery<PluginInstallation[]>(pluginsVisible ? '/api/plugins' : null);
  const plugins = pluginQuery.data ?? [];
  const packages = useMemo<InstalledUI[]>(() => [...builtins, ...(pluginsVisible ? plugins : []).flatMap(p => (p.Manifest.contributes.themes ?? []).filter(t => pluginAllowed(p,t.capabilities)).map(t => ({ runtime: 'plugin' as const, plugin: { ID: p.ID, Digest: p.Digest, Kind: 'theme' as const, Contribution: t.id, Page: '' }, manifest: { id: p.ID + '.' + t.id.replaceAll('_', '-'), name: t.name, version: p.Manifest.version, pages: t.pages } })))], [pluginQuery.data, pluginsVisible]);
  const [saved, setSaved] = useState<Settings | null>(null), [error, setError] = useState<unknown>(null), [busy, setBusy] = useState(false);
  const [recovering, setRecovering] = useState(false), writing = useRef(false);
  const current = saved && (!query.data || saved.Version > query.data.Version) ? saved : query.data;
  const selected = packages.find(item => packageKey(item.manifest) === current?.UITheme);
  const configuration = useMemo<UIConfiguration>(() => !recovering && selected ? { selected: { id: selected.manifest.id, version: selected.manifest.version } } : defaultConfiguration(), [recovering, selected]);
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
    select,
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
