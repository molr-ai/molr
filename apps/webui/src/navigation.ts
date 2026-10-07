import { useCallback, useEffect, useState } from 'react';
export type RoutePage = 'plans' | 'assistants' | 'todos' | 'schedules' | 'resources' | 'settings' | 'workbench' | 'extensions' | 'account';
const pages: RoutePage[] = ['plans', 'assistants', 'todos', 'schedules', 'resources', 'settings', 'workbench', 'extensions', 'account'];
function readRoute() {
  const [path, query = ''] = location.hash.replace(/^#\/?/, '').split('?');
  const [raw, rawID = '', contribution = ''] = path.split('/');
  let entry = contribution;
  try { entry = decodeURIComponent(contribution); } catch { entry = ''; }
  const id = raw === 'extensions' ? `${rawID}${entry ? '/' + entry : ''}` : rawID;
  const turn = new URLSearchParams(query).get('turn') ?? '';
  const turnID = raw === 'workbench' && /^[A-Za-z0-9_-]{1,128}$/.test(turn) ? turn : '';
  const message = new URLSearchParams(query).get('message') ?? '';
  const messageID = raw === 'assistants' && /^[A-Za-z0-9_-]{1,128}$/.test(message) ? message : '';
  const requestedPanel = new URLSearchParams(query).get('panel');
  const panel = raw === 'assistants' && ['info','permissions','channel','tasks'].includes(requestedPanel ?? '') ? requestedPanel as 'info'|'permissions'|'channel'|'tasks' : undefined;
  const requestedTask = new URLSearchParams(query).get('task') ?? '';
  const taskID = panel === 'tasks' && /^[A-Za-z0-9_-]{1,128}$/.test(requestedTask) ? requestedTask : '';
  return { panel, taskID, page: raw === 'todos' || raw === 'schedules' ? 'plans' as const : pages.includes(raw as RoutePage) ? raw as RoutePage : 'assistants' as const, id: (raw === 'extensions' ? /^[a-z0-9.-]+\/(?:[a-z0-9_-]+|@settings|@admin)$/.test(id) : /^[A-Za-z0-9_-]{1,128}$/.test(id)) ? id : '', turnID, messageID };
}
export function useNavigation() {
  const [route, setRoute] = useState(readRoute);
  useEffect(() => { const update = () => setRoute(readRoute()); window.addEventListener('hashchange', update); return () => window.removeEventListener('hashchange', update); }, []);
  const navigate = useCallback((page: RoutePage, id = '', turnID = '', messageID = '') => {
    if (page === 'todos' || page === 'schedules') page = 'plans';
    if (page !== 'workbench') turnID = '';
    if (page !== 'assistants') messageID = '';
    const query = new URLSearchParams();
    if (turnID) query.set('turn', turnID);
    if (messageID) query.set('message', messageID);
    location.hash = `/${page}${id ? `/${(page === 'extensions' ? id.split('/').map(encodeURIComponent).join('/') : encodeURIComponent(id))}` : ''}${query.size ? `?${query}` : ''}`;
    setRoute({ page, id, turnID, messageID, panel: undefined, taskID: '' });
  }, []);
  return { ...route, navigate };
}
