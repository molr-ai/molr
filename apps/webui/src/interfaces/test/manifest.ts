import type { InstalledUI } from '@molr/ui-host';
/** This theme replaces only chat. All other pages come from the default theme. */
export const testPackage: InstalledUI = {
  runtime: 'builtin',
  manifest: {
    id: 'molr.test', name: 'Molr 测试主题', version: '1.0.0',
    pages: { assistants: 'test-chat' },
  },
};
