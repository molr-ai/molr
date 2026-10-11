import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

export async function triggerWebsite(hook) {
  let url;
  try { url = new URL(hook); } catch { throw new Error('Configure the CLOUDFLARE_DEPLOY_HOOK repository secret'); }
  if (url.protocol !== 'https:' || url.hostname !== 'api.cloudflare.com' || url.port || url.username || url.password || url.search || url.hash || !/^\/client\/v4\/workers\/builds\/deploy_hooks\/[A-Za-z0-9-]+$/.test(url.pathname)) throw new Error('CLOUDFLARE_DEPLOY_HOOK must be a Workers Builds Deploy Hook');
  let response;
  try { response = await fetch(url, { method: 'POST', redirect: 'error', signal: AbortSignal.timeout(30000) }); }
  catch { throw new Error('Cloudflare Deploy Hook request failed; inspect Cloudflare build history before retrying'); }
  if (!response.ok) throw new Error(`Cloudflare Deploy Hook rejected the request: HTTP ${response.status}`);
  let body;
  try { body = await response.json(); } catch { throw new Error('Invalid Cloudflare Deploy Hook response'); }
  if (body.success !== true) throw new Error('Cloudflare did not accept the build request');
  console.log('Cloudflare accepted the website build request; deployment is still pending');
}
if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) await triggerWebsite(process.env.CLOUDFLARE_DEPLOY_HOOK);
