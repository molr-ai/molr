import { execFileSync } from 'node:child_process';
import { existsSync, realpathSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { Plugin, ResolvedConfig } from 'vite';

const checker = fileURLToPath(new URL('./release_safety.py', import.meta.url));

export function checkReleaseFiles(kind: 'public' | 'frontend' | 'release-directory', path: string) {
  execFileSync('python3', [checker, kind, path], { stdio: ['ignore', 'pipe', 'pipe'] });
}

/** A public directory is copied verbatim by Vite, even if Git ignores its files. */
export function releaseAssets(): Plugin {
  let config: ResolvedConfig;
  return {
    name: 'molr-release-safety',
    enforce: 'pre',
    load(id) {
      const path = id.split('?')[0];
      const privatePath = /(?:^|[/\\])internal-docs(?:[/\\]|$)/i;
      if (privatePath.test(path) || (existsSync(path) && privatePath.test(realpathSync(path)))) {
        throw new Error('Internal documents cannot be imported into public applications');
      }
    },
    config(value) {
      // Keep Vite's credential deny rules when adding our private directory.
      const defaults = ['.env', '.env.*', '*.{crt,pem,key,p12,pfx,cer,der}', '.npmrc', '.yarnrc.yml', '**/.git/**'];
      return { server: { fs: { deny: [...defaults, ...(value.server?.fs?.deny ?? []), '**/internal-docs/**'] } } };
    },
    configResolved(value) {
      config = value;
      if (config.publicDir && existsSync(config.publicDir)) checkReleaseFiles('public', config.publicDir);
    },
    writeBundle: {
      order: 'post',
      handler() {
        checkReleaseFiles('frontend', resolve(config.root, config.build.outDir));
      },
    },
    configurePreviewServer() {
      checkReleaseFiles('frontend', resolve(config.root, config.build.outDir));
    },
  };
}
