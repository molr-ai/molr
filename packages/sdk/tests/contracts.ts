import { createClient, subscribeRuntime } from '../src/index';
import type { PluginTheme, PluginThemePage } from '../src/plugins';

export const themePages: PluginTheme['pages'] = { plans: 'plans.html' };
export const allThemePages: Record<PluginThemePage, string> = {
  assistants: 'assistants.html', plans: 'plans.html', resources: 'resources.html', workbench: 'workbench.html',
};
// @ts-expect-error Settings is owned by the host.
export const settingsPage: PluginTheme['pages'] = { settings: 'settings.html' };
// @ts-expect-error Unknown theme pages must fail at authoring time.
export const unknownPage: PluginTheme['pages'] = { plan: 'plans.html' };

export function subscribeWithClient() {
  return subscribeRuntime({ session: createClient(), onUpdate() {}, onRestore() {}, onConnection() {} });
}
