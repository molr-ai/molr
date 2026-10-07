import { releaseAssets } from '../../scripts/release-assets';
import { licenseAssets } from '../../scripts/license-assets';
import breakpointQueries from '@molr/ui/postcss-breakpoints';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { themedBrandPlugin } from '@molr/ui/themed-brand-plugin';

export default defineConfig({
  plugins: [releaseAssets(), licenseAssets(), react(), themedBrandPlugin()],
  css: { postcss: { plugins: [breakpointQueries()] } },
  publicDir: fileURLToPath(new URL('../../packages/ui/public', import.meta.url)),
  server: {
    host: '0.0.0.0',
    port: 5188,
    strictPort: true,
  },
  preview: {
    host: '0.0.0.0',
  },
});
