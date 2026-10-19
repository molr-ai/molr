import type { UIPage, UIPackageManifest } from '@molr/sdk/ui';

export type InstalledUI = { manifest: UIPackageManifest } & ({ runtime: 'builtin'; plugin?: never } | { runtime: 'plugin'; plugin: import('@molr/sdk/plugins').PluginUIReference });
export type UISelection = { id: string; version: string };
export type UIConfiguration = { selected: UISelection };
export const defaultSelection: UISelection = { id: 'molr.official', version: '1.0.0' };
export const defaultConfiguration = (): UIConfiguration => ({ selected: { ...defaultSelection } });
export const packageKey = (value: Pick<UIPackageManifest, 'id' | 'version'>) => `${value.id}@${value.version}`;
/** A theme owns its entire page map. Users select one theme, never individual pages. */
export function resolveUI(config: UIConfiguration, packages: InstalledUI[], page: UIPage): InstalledUI {
  const selected = packages.find(item => packageKey(item.manifest) === packageKey(config.selected));
  const fallback = packages.find(item => packageKey(item.manifest) === packageKey(defaultSelection) && item.manifest.pages[page]);
  if (!fallback) throw new Error('missing_default_page');
  return selected?.manifest.pages[page] ? selected : fallback;
}
