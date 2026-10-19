import type { InstalledUI } from '@molr/ui-host';
/** Official packages use the same partial page contract as installed packages. */
export const officialPackage: InstalledUI = {
  runtime: 'builtin',
  manifest: { id: 'molr.official', name: 'Molr 默认主题', version: '1.0.0',
    description: 'The built-in Molr interface.',
    pages: { assistants: 'official', plans: 'official', resources: 'official', workbench: 'official', settings: 'official' },
  },
};
