import type { PluginPackage } from '@molr/sdk/plugins';
export function httpPluginPackage(id: string, origin: string): PluginPackage & { manifest: { settingsPage: NonNullable<PluginPackage['manifest']['settingsPage']> } };
