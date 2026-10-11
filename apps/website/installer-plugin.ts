import { checkReleaseFiles } from '../../scripts/release-assets';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import type { Plugin } from 'vite';

// One bootstrap source; production catalogs contain URLs, never binary packages.
export function installerPlugin(): Plugin {
 const root = fileURLToPath(new URL('../../', import.meta.url));
 const releaseDir = process.env.MOLR_RELEASE_DIR;
 const catalogDir = process.env.MOLR_CATALOG_DIR && resolve(root, process.env.MOLR_CATALOG_DIR);
 const releaseURL = process.env.MOLR_RELEASE_URL || 'https://molr.ai/releases';
 if (!/^https?:\/\/[A-Za-z0-9.:[\]-]+(?:\/[A-Za-z0-9._/-]*)?$/.test(releaseURL)) throw new Error('MOLR_RELEASE_URL must be a plain HTTP(S) URL');
 if (releaseDir && catalogDir) throw new Error('Use either MOLR_RELEASE_DIR or MOLR_CATALOG_DIR');
 const script = () => readFileSync(resolve(root, 'scripts/install.sh'), 'utf8').replace("'https://molr.ai/releases'", JSON.stringify(releaseURL));
 const assets = () => {
  const files: Record<string, string | Buffer> = { 'install.sh': script() };
  if (catalogDir) {
   checkReleaseFiles('release-directory', catalogDir);
   const collect = (path: string, prefix: string) => {
    for (const entry of readdirSync(path, { withFileTypes: true })) {
     const name = `${prefix}${entry.name}`;
     if (entry.isDirectory()) collect(resolve(path, entry.name), `${name}/`);
     else files[name] = readFileSync(resolve(path, entry.name));
    }
   };
   collect(catalogDir, 'releases/');
  } else {
   files['releases/catalog.json'] = JSON.stringify({ schema: 1, channels: { stable: null, preview: null, nightly: null } });
   if (releaseDir) {
    checkReleaseFiles('release-directory', resolve(releaseDir));
    for (const name of readdirSync(releaseDir).filter(name => /^(molr_[0-9A-Za-z.-]+_[a-z0-9]+_[a-z0-9]+\.tar\.gz(?:\.sha256)?|latest-[a-z0-9]+-[a-z0-9]+\.json)$/.test(name))) files[`packages/${name}`] = readFileSync(resolve(releaseDir, name));
   }
  }
  return files;
 };
 let developmentAssets: ReturnType<typeof assets>;
 return { name: 'molr-installer',
  configureServer(server) {
   developmentAssets = assets();
   server.middlewares.use((req, res, next) => {
    const path = req.url?.split('?')[0].slice(1) || '';
    const content = path === 'install.sh' ? script() : developmentAssets[path];
    if (content === undefined) return next();
    res.setHeader('Content-Type', path.endsWith('.json') ? 'application/json' : path.endsWith('.sh') ? 'text/plain; charset=utf-8' : 'application/octet-stream');
    res.setHeader('Cache-Control', 'no-cache'); res.end(content);
   });
  },
  generateBundle() {
   for (const [fileName, source] of Object.entries(assets())) this.emitFile({ type: 'asset', fileName, source });
   this.emitFile({ type: 'asset', fileName: '_headers', source: '/releases/*\n  Cache-Control: no-cache\n/install.sh\n  Cache-Control: no-cache\n  Content-Type: text/plain; charset=utf-8\n' });
  },
 };
}
