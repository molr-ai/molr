import { readFileSync } from 'node:fs';
import type { Plugin } from 'vite';

/** One reviewed notice file is shared by all public frontend artifacts. */
export function licenseAssets(): Plugin {
  const files = ['LICENSE', 'THIRD_PARTY_NOTICES.md'];
  const read = (name: string) => readFileSync(new URL(`../${name}`, import.meta.url), 'utf8');
  return {
    name: 'molr-license-assets',
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        const name = request.url?.split('?')[0].slice(1);
        if (!name || !files.includes(name)) return next();
        response.setHeader('Content-Type', 'text/plain; charset=utf-8');
        response.end(read(name));
      });
    },
    generateBundle() {
      for (const fileName of files) this.emitFile({ type: 'asset', fileName, source: read(fileName) });
    },
    transformIndexHtml() {
      return [{ tag: 'link', attrs: { rel: 'license', href: '/THIRD_PARTY_NOTICES.md' }, injectTo: 'head' }];
    },
  };
}
