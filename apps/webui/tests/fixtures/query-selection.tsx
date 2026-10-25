import { createRoot } from 'react-dom/client';
import { DataProvider, useQuery, useQueryValue } from '../../src/state';

type Data = { Count: number; Detail: string };
declare global { interface Window { selectionRenders: number; refreshSelection: () => void } }
window.selectionRenders = 0;
const selectCount = (data?: Data) => data?.Count ?? 0;

function Count() {
  window.selectionRenders++;
  const value = useQueryValue('/api/query-fixture', selectCount, false, 0);
  return <output aria-label="count">{value}</output>;
}
function Status() {
  const query = useQuery<Data>('/api/query-fixture', false, 0);
  window.refreshSelection = query.reload;
  return <output aria-label="status">{query.error ? 'error' : query.loading ? 'loading' : 'ready'}</output>;
}
createRoot(document.getElementById('root')!).render(<DataProvider><Count /><Status /></DataProvider>);
