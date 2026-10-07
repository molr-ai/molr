import type { UIPermission } from '@molr/sdk/ui';
/** Explicit endpoint allowlist. New Runtime endpoints are denied until reviewed. */
const id = '[A-Za-z0-9_-]+';
const reads: [UIPermission, RegExp][] = [
  ['chat.read', new RegExp(`^/api/(assistants(?:/${id}(?:/(status|capabilities))?)?|conversations(?:/${id}(?:/messages)?)?)$`)],
  ['plans.read', new RegExp(`^/api/(plans|tasks|todos|schedules)(?:/${id}(?:/(revisions|occurrences|tasks))?)?$`)],
  ['resources.read', new RegExp(`^/api/(resources(?:/${id}(?:/connection)?)?|models|grants|sandbox|status)$`)],
  ['files.read', new RegExp(`^/api/files/${id}(?:/content)?$`)],
  ['settings.read', /^\/api\/(settings(?:\/currency)?|usage|memories(?:\/[A-Za-z0-9_-]+)?)$/],
  ['workbench.read', /^\/api\/debug\/(runs(?:\/[A-Za-z0-9_-]+(?:\/steps\/\d+)?)?|trajectory|assistants\/[A-Za-z0-9_-]+\/checkpoint)$/],
];
const writes: [UIPermission, RegExp][] = [
  ['chat.write', new RegExp(`^/api/(assistants(?:/(clone|dismiss|rehire|update|place|groups(?:/(delete|update))?|${id}/(stop|expedite)))?|conversations/(messages|read|feedback))$`)],
  ['plans.write', /^\/api\/(plans(?:\/(configure|execute))?|tasks(?:\/(cancel|pause|resume|revise))?|todos(?:\/(delete|update))?|schedules(?:\/(delete|update))?)$/],
  ['resources.write', new RegExp(`^/api/(resources(?:/(disconnect|delete-provider|update|provider|test-connection|refresh-models|configure-model|${id}/check))?|grants(?:/(revoke|update))?)$`)],
  ['settings.write', /^\/api\/(settings|memories(?:\/(delete|update))?)$/],
];
export function requestPermission(method: string, path: string): UIPermission | null {
  if (typeof path !== 'string' || path.length > 4096 || !/^\/api\/[A-Za-z0-9/_-]+(?:\?[^#]*)?$/.test(path) || path.includes('..') || path.includes('\\')) return null;
  const pathname = path.split('?')[0];
  if (method === 'POST' && pathname === '/api/files' || method === 'DELETE' && new RegExp(`^/api/files/${id}$`).test(pathname)) return 'files.write';
  return (method === 'GET' ? reads : method === 'POST' ? writes : []).find(([, pattern]) => pattern.test(pathname))?.[0] ?? null;
}
