import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import type { Plugin } from 'vite';
import { paletteBrandColors } from './src/palettes.ts';

/** Serve and export recolored SVG copies for opted-in applications. Source assets and PNGs stay intact. */
export function themedBrandPlugin(): Plugin {
  const directory = fileURLToPath(new URL('./public/brand', import.meta.url));
  let assets = new Map<string, string>();
  function loadAssets() {
    const next = new Map<string, string>();
    for (const file of readdirSync(directory).filter(file => file.endsWith('.svg'))) {
      const original = readFileSync(`${directory}/${file}`, 'utf8');
      for (const [palette, colors] of Object.entries(paletteBrandColors)) {
        if (palette === 'forest') continue;
        const replacements: Record<string, string> = { '#263b31': colors.ink, '#f5f6ef': colors.paper, '#d5f478': colors.accent };
        next.set(`brand/themes/${palette}/${file}`, original.replace(/#(?:263b31|f5f6ef|d5f478)\b/gi, color => replacements[color.toLowerCase()]));
      }
    }
    assets = next;
  }
  return {
    name: 'molr-themed-brand',
    buildStart: loadAssets,
    configureServer(server) {
      loadAssets();
      server.watcher.add(directory);
      server.watcher.on('change', file => {
        if (file.startsWith(`${directory}/`) && file.endsWith('.svg')) {
          loadAssets();
          server.ws.send({ type: 'full-reload' });
        }
      });
      server.middlewares.use((request, response, next) => {
        // Exact lookup never reads user-provided paths from disk.
        const path = new URL(request.url ?? '/', 'http://vite.local').pathname.slice(1);
        const source = assets.get(path);
        if (!source) return next();
        response.setHeader('Content-Type', 'image/svg+xml');
        response.setHeader('Cache-Control', 'no-cache');
        response.end(source);
      });
    },
    generateBundle() {
      for (const [fileName, source] of assets) this.emitFile({ type: 'asset', fileName, source });
    },
  };
}
