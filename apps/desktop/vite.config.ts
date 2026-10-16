import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { licenseAssets } from '../../scripts/license-assets.ts';
import { releaseAssets } from '../../scripts/release-assets.ts';
import breakpointQueries from '@molr/ui/postcss-breakpoints';

export default defineConfig({
  base: './',
  plugins: [releaseAssets(), react(), licenseAssets(), {
    name: 'desktop-inline-script-hashes',
    transformIndexHtml: { order: 'post', handler(html) {
      const hashes = [...html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)].filter(match => !/\bsrc=/.test(match[1]) && match[2].trim()).map(match => `'sha256-${createHash('sha256').update(match[2]).digest('base64')}'`);
      return hashes.length ? html.replace("script-src 'self'", `script-src 'self' ${hashes.join(' ')}`) : html;
    } },
  }],
  publicDir: fileURLToPath(new URL('../../packages/ui/public', import.meta.url)),
  css: { postcss: { plugins: [breakpointQueries()] } },
  build: { outDir: 'dist/renderer' },
  server: { host: '0.0.0.0', port: 5196, strictPort: true },
});
