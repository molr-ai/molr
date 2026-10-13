import type { QueryInterval } from './query-store';

// Events provide prompt updates; these intervals reconcile missed/cross-client changes.
export function queryInterval(path: string): QueryInterval {
  const url = path.split('?')[0];
  if (url === '/api/status' || /\/debug\/runs\/[^/]+\/steps\//.test(url) || /^\/api\/files\/[^/]+$/.test(url)) return 0;
  if (['/api/account/quotas', '/api/admin/usage', '/api/account/usage', '/api/admin/usage/analytics', '/api/resource-usage'].includes(url)) return 30_000;
  if (/\/assistants\/[^/]+\/status$/.test(url)) return 30_000;
  if (url === '/api/dreams') return data => Array.isArray(data) && data.some(run => [run.State, run.SessionState].some(state => ['queued', 'running', 'committing'].includes(state))) ? 5000 : 30_000;
  if (/^\/api\/(settings|models|plugins|resources|grants|sandboxes|tools|setup|auth|admin|account)(\/|$)/.test(url) || url.endsWith('/capabilities') || url === '/api/dreams/config') return 300_000;
  if (/^\/api\/(debug\/runs|tasks|operations)\/[^/]+$/.test(url)) return data => data && typeof data === 'object' && 'State' in data && ['completed', 'succeeded', 'failed', 'canceled', 'interrupted', 'stopped'].includes(String(data.State)) ? false : 30_000;
  return 30_000;
}
