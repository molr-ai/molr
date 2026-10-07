import { uiPages, uiPermissions } from '@molr/sdk/ui';
import type { UIPage, UIPackageManifest, UIPermission } from '@molr/sdk/ui';

export type InstalledUI = { manifest: UIPackageManifest; runtime: 'builtin' | 'frame'; plugin?: import('@molr/sdk/plugins').PluginUIReference };
export type UISelection = { id: string; version: string };
export type UIConfiguration = { schema: 2; selected: UISelection };
export const defaultSelection: UISelection = { id: 'molr.official', version: '1.0.0' };
export const defaultConfiguration = (): UIConfiguration => ({ schema: 2, selected: { ...defaultSelection } });
export const packageKey = (value: Pick<UIPackageManifest, 'id' | 'version'>) => `${value.id}@${value.version}`;
export function parseManifest(input: unknown): UIPackageManifest {
  if (!input || typeof input !== 'object' || Array.isArray(input)) throw new Error('invalid_manifest');
  const item = input as Record<string, unknown>;
  if (typeof item.id !== 'string' || !/^[a-z][a-z0-9-]*(?:\.[a-z0-9-]+)+$/.test(item.id) || item.id.length > 96 || typeof item.name !== 'string' || !item.name.trim() || item.name.length > 80 || typeof item.version !== 'string' || !/^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/.test(item.version) || item.version.length > 64) throw new Error('invalid_manifest');
  if (item.apiVersion !== 1) throw new Error('incompatible_ui_api');
  if (!item.pages || typeof item.pages !== 'object' || Array.isArray(item.pages) || !Object.keys(item.pages).length || Object.keys(item.pages).some(page => !uiPages.includes(page as UIPage))) throw new Error('invalid_pages');
  if (!Array.isArray(item.permissions) || item.permissions.length > uiPermissions.length || item.permissions.some(p => !uiPermissions.includes(p as UIPermission))) throw new Error('invalid_permissions');
  if (item.description !== undefined && (typeof item.description !== 'string' || item.description.length > 500)) throw new Error('invalid_manifest');
  const pages: UIPackageManifest['pages'] = {};
  for (const [page, entry] of Object.entries(item.pages)) {
    if (typeof entry !== 'string' || !entry || entry.length > 2048) throw new Error('invalid_entry');
    pages[page as UIPage] = entry;
  }
  return { id: item.id, name: item.name.trim(), version: item.version, apiVersion: 1, description: item.description as string | undefined, pages, permissions: [...new Set(item.permissions)] as UIPermission[] };
}
/** External packages are always frames, regardless of publisher or ID. */
export function externalPackage(input: unknown, hostOrigin: string): InstalledUI {
  const manifest = parseManifest(input);
  for (const entry of Object.values(manifest.pages)) {
    let url: URL;
    try { url = new URL(entry); } catch { throw new Error('invalid_entry'); }
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.hash || url.origin === hostOrigin) throw new Error('invalid_entry');
    if (new URL(hostOrigin).protocol === 'https:' && url.protocol !== 'https:') throw new Error('insecure_entry');
  }
  return { manifest, runtime: 'frame' };
}
/** A theme owns its entire page map. Users select one theme, never individual pages. */
export function resolveUI(config: UIConfiguration, packages: InstalledUI[], page: UIPage): InstalledUI {
  const selected = packages.find(item => packageKey(item.manifest) === packageKey(config.selected));
  const fallback = packages.find(item => packageKey(item.manifest) === packageKey(defaultSelection) && item.manifest.pages[page]);
  if (!fallback) throw new Error('missing_default_page');
  return selected?.manifest.pages[page] ? selected : fallback;
}
export function readConfiguration(raw: string | null, packages: InstalledUI[]): UIConfiguration {
  if (!raw) return defaultConfiguration();
  const input = JSON.parse(raw);
  // The initial route-by-route prototype is intentionally not reactivated.
  if (input?.schema === 1) return defaultConfiguration();
  if (input?.schema !== 2 || !input.selected || typeof input.selected.id !== 'string' || typeof input.selected.version !== 'string') throw new Error('invalid_configuration');
  const selected = packages.find(item => packageKey(item.manifest) === packageKey(input.selected));
  return selected ? { schema: 2, selected: { id: selected.manifest.id, version: selected.manifest.version } } : defaultConfiguration();
}
