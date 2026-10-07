import { readFileSync, realpathSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { Plugin } from 'vite';
import { devDocRoutes, docRoutes, type DocRoute } from './src/docs-routes.ts';

const repository = fileURLToPath(new URL('../../', import.meta.url));
const moduleID = 'virtual:molr-documents';
const resolvedID = '\0' + moduleID;

/** Only reviewed route entries can supply text to the browser bundle. */
export function documentPath(entry: DocRoute, root = repository) {
  if (!['docs', 'dev-docs'].includes(entry.root)
      || !/^[A-Za-z0-9_/-]+\.md$/.test(entry.file)
      || entry.file.split('/').some(part => ['..', 'internal-docs'].includes(part))) {
    throw new Error('Documentation must use a reviewed public Markdown path');
  }
  const path = resolve(root, entry.root, entry.file);
  if (!path.startsWith(resolve(root, entry.root) + '/') || realpathSync(path) !== path) {
    throw new Error('Documentation links and paths outside the public directory are forbidden');
  }
  return path;
}

export function publicDocuments(): Plugin {
  const paths = new Set<string>();
  return {
    name: 'molr-public-documents',
    resolveId(id) { if (id === moduleID) return resolvedID; },
    load(id) {
      if (id !== resolvedID) return;
      paths.clear();
      const sources: Record<string, string> = {};
      for (const entry of [...docRoutes, ...devDocRoutes]) {
        const path = documentPath(entry);
        paths.add(path);
        this.addWatchFile(path);
        sources[`../../../${entry.root}/${entry.file}`] = readFileSync(path, 'utf8');
      }
      return `export default ${JSON.stringify(sources)};`;
    },
    handleHotUpdate({ file, server }) {
      if (!paths.has(file)) return;
      const module = server.moduleGraph.getModuleById(resolvedID);
      if (module) server.moduleGraph.invalidateModule(module);
      server.ws.send({ type: 'full-reload' });
      return [];
    },
  };
}
