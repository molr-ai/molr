import type { Settings } from '@molr/sdk';
import { useQuery } from '../state';

export function usePluginsVisible() {
  return useQuery<Settings>('/api/settings').data?.PluginsVisible === true;
}

// Service connections belong to plugins. Display preferences never alter grants.
export function pluginResourceVisible(kind: string, visible: boolean, id = '') {
  return visible || (kind !== 'plugin' && (kind !== 'connection' || id.startsWith('mcpconn_')) && !(kind === 'tool' && id.startsWith('plg_')));
}
