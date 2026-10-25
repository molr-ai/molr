import { StrictMode, useState } from 'react';
import { AssistantNavigationItem } from '../../src/AssistantNavigationItem';
import { createRoot } from 'react-dom/client';
import { DataProvider, useData, useQuery, useQueryValue } from '../../src/state';

type Data = { Count: number; Detail: string };
declare global { interface Window { selectionRenders: number; refreshSelection: () => void; invalidateSelection: (topic?: string) => void; selectionPath: (path: string | null) => void; navigationRenders: number } }
window.selectionRenders = 0;
const selectCount = (data?: Data) => data?.Count ?? 0;

function Count({ path }: { path: string | null }) {
  window.selectionRenders++;
  const value = useQueryValue(path, selectCount, false, 0);
  return <output aria-label="count">{value}</output>;
}
function Status({ path }: { path: string | null }) {
  const query = useQuery<Data>(path, false, 0);
  window.refreshSelection = query.reload;
  return <output aria-label="status">{query.error ? 'error' : query.loading ? 'loading' : 'ready'}</output>;
}
function Navigation() {
  window.navigationRenders++;
  return <nav><AssistantNavigationItem active mobile={false} onNavigate={() => {}} /></nav>;
}
function Fixture() {
  const { topics } = useData();
  const [path, setPath] = useState<string | null>('/api/query-fixture');
  window.selectionPath = setPath;
  window.invalidateSelection = (topic = 'query-fixture') => topics.invalidate([topic]);
  return <><Count path={path} /><Status path={path} /><Navigation /></>;
}
window.navigationRenders = 0;
createRoot(document.getElementById('root')!).render(<StrictMode><DataProvider><Fixture /></DataProvider></StrictMode>);
