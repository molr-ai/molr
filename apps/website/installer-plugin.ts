import { checkReleaseFiles } from '../../scripts/release-assets';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import type { Plugin } from 'vite';

// Keep the bootstrap script in one source file; release assets are opt-in for local acceptance.
export function installerPlugin(): Plugin {
 const releaseDir = process.env.MOLR_RELEASE_DIR;
 const releaseURL = process.env.MOLR_RELEASE_URL || '';
 if (releaseURL && !/^https?:\/\/[A-Za-z0-9.:[\]-]+(?:\/[A-Za-z0-9._/-]*)?$/.test(releaseURL)) throw new Error('MOLR_RELEASE_URL must be a plain HTTP(S) URL');
 const script = () => readFileSync(fileURLToPath(new URL('../../scripts/install.sh', import.meta.url)), 'utf8').replace('source_dir="${MOLR_RELEASE_URL:-}"', `source_dir="\${MOLR_RELEASE_URL:-${releaseURL}}"`);
 const files = () => {
  if (!releaseDir) return [];
  checkReleaseFiles('release-directory', resolve(releaseDir));
  return readdirSync(releaseDir).filter(name => /^(molr_[0-9A-Za-z.-]+_linux_(amd64|arm64)\.tar\.gz(?:\.sha256)?|latest-linux-(amd64|arm64)\.json)$/.test(name));
 };
 return { name: 'molr-installer',
  configureServer(server) { server.middlewares.use((req,res,next) => {
   const path = req.url?.split('?')[0];
   if (path === '/install.sh') {res.setHeader('Content-Type','text/plain; charset=utf-8');res.setHeader('Cache-Control','no-cache');res.end(script());return;}
   if (releaseDir && path?.startsWith('/releases/') && files().includes(path.slice('/releases/'.length))) {res.setHeader('Content-Type','application/octet-stream');res.end(readFileSync(resolve(releaseDir,path.slice('/releases/'.length))));return;}
   next();
  }); },
  generateBundle() {
   this.emitFile({ type:'asset',fileName:'install.sh',source:script() });
   for (const name of files()) this.emitFile({type:'asset',fileName:`releases/${name}`,source:readFileSync(resolve(releaseDir!,name))});
  },
 };
}
