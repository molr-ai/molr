import { releaseAssets } from '../../scripts/release-assets';
import { licenseAssets } from '../../scripts/license-assets';
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import type { Plugin, ProxyOptions, ViteDevServer } from 'vite';
import react from '@vitejs/plugin-react';
import { themedBrandPlugin } from '@molr/ui/themed-brand-plugin';
import breakpointQueries from '@molr/ui/postcss-breakpoints';
import { browserCookie, runtimeCookies } from './cookie-proxy.ts';

const root = fileURLToPath(new URL('../../', import.meta.url));
const version = process.env.MOLR_VERSION || JSON.parse(readFileSync(new URL('../../package.json', import.meta.url), 'utf8')).version;
const commit = process.env.MOLR_COMMIT || execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
if (!/^[0-9][0-9A-Za-z.-]*$/.test(version)) throw new Error('Invalid MOLR_VERSION');
if (!/^(?:[0-9a-f]{40}|[0-9a-f]{64})$/.test(commit)) throw new Error('MOLR_COMMIT must be a full Git commit ID');

const target = new URL(process.env.MOLR_RUNTIME_URL ?? 'http://127.0.0.1:8080');
if (!['http:', 'https:'].includes(target.protocol) || target.username || target.password || target.pathname !== '/') throw new Error('MOLR_RUNTIME_URL must be an HTTP(S) origin without credentials or a path');
const proxy: ProxyOptions = { target: target.origin, changeOrigin: true, xfwd: false, configure(server) {
  const port = (host: string | undefined) => host?.match(/:(\d+)$/)?.[1] || '80';
  server.on('proxyReq', (request, incoming) => {
    if (incoming.headers.origin) request.setHeader('Origin', target.origin);
    const cookies = runtimeCookies(incoming.headers.cookie, port(incoming.headers.host), target.port || (target.protocol === "https:" ? "443" : "80"));
    if (cookies) request.setHeader('Cookie', cookies);
    else request.removeHeader('Cookie');
  });
  server.on('proxyRes', (response, incoming) => {
    const cookies = response.headers['set-cookie'];
    if (cookies) response.headers['set-cookie'] = cookies.map(cookie => browserCookie(cookie, port(incoming.headers.host)));
  });
} };
// Origin rewriting is permitted ONLY after rejecting cross-origin browser
// requests. Never turn the local development proxy into a CSRF bypass.
function apiOriginGuard(): Plugin {
  const install = (server: Pick<ViteDevServer, 'middlewares'>) => {
    server.middlewares.use((request, response, next) => {
      if (!request.url?.startsWith('/api/')) return next();
      const origin = request.headers.origin;
      const sameOrigin = origin === `http://${request.headers.host}`;
      if ((origin && !sameOrigin) || (!['GET', 'HEAD', 'OPTIONS'].includes(request.method ?? '') && !sameOrigin)) {
        response.statusCode = 403; response.end('Origin denied'); return;
      }
      next();
    });
  };
  return { name: 'runtime-origin-guard', configureServer: install, configurePreviewServer: install };
}
function notificationWorker(): Plugin {
  const source = () => readFileSync(new URL('./src/notifications/worker.js', import.meta.url), 'utf8');
  return { name: 'notification-worker',
    configureServer(server) { server.middlewares.use((request, response, next) => {
      if (request.url?.split('?')[0] !== '/molr-notifications-sw.js') return next();
      response.setHeader('Content-Type', 'text/javascript'); response.setHeader('Cache-Control', 'no-cache'); response.end(source());
    }); },
    generateBundle() { this.emitFile({ type: 'asset', fileName: 'molr-notifications-sw.js', source: source() }); },
  };
}
export default defineConfig({
  define: { __MOLR_BUILD__: JSON.stringify({ version, commit }) },
  plugins: [releaseAssets(), licenseAssets(), notificationWorker(), apiOriginGuard(), react(), themedBrandPlugin()],
  publicDir: fileURLToPath(new URL('../../packages/ui/public', import.meta.url)),
  css: { postcss: { plugins: [breakpointQueries()] } },
  server: { host: '0.0.0.0', port: 5191, strictPort: true, proxy: { '/api': proxy } },
  preview: { host: '0.0.0.0', port: 5192, strictPort: true, proxy: { '/api': proxy } },
});
