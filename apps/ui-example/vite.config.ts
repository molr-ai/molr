import { releaseAssets } from '../../scripts/release-assets';
import { licenseAssets } from '../../scripts/license-assets';
import { defineConfig } from 'vite';
import type { Plugin, ViteDevServer } from 'vite';
import manifest from './manifest.json' with { type: 'json' };

function liveManifest(): Plugin {
  const install = (server: Pick<ViteDevServer, 'middlewares'>) => { server.middlewares.use((request, response, next) => {
    if (request.url !== '/manifest.json') return next();
    response.setHeader('Content-Type', 'application/json');
    response.setHeader('Content-Disposition', 'attachment; filename=focus-ui.json');
    response.end(JSON.stringify({ ...manifest, pages: { assistants: `http://${request.headers.host}/` } }, null, 2));
  }); };
  return { name: 'example-ui-manifest', configureServer: install, configurePreviewServer: install };
}
// Sandboxed documents have an opaque origin. The example exposes only public assets,
// never an API proxy or credentials. Restrict framing in your production deployment.
export default defineConfig({
  plugins: [releaseAssets(), licenseAssets(), liveManifest()],
  server: { host: '0.0.0.0', port: 5311, strictPort: true, cors: { origin: 'null' } },
  preview: { host: '0.0.0.0', port: 5312, strictPort: true, cors: { origin: 'null' } },
});
