import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import type { Assistant, RuntimeInfo, Settings as SettingsDTO } from '@molr/sdk';
import { useErrorToast } from '../../error-toasts';
import { guardedNavigation, useNavigation } from '../../navigation';
import type { RoutePage } from '../../navigation';
import { useQuery } from '../../state';
import type { AssistantsPageMemory } from './AssistantsPage';
import { Loading } from './components/common';
import { Search } from './features/Search';

const AssistantsPage = lazy(() => import('./AssistantsPage').then(module => ({ default: module.AssistantsPage })));
const Plans = lazy(() => import('./features/Plans').then(module => ({ default: module.Plans })));
const Resources = lazy(() => import('./features/Resources').then(module => ({ default: module.Resources })));
const Settings = lazy(() => import('./features/Settings').then(module => ({ default: module.Settings })));
const Workbench = lazy(() => import('./features/Workbench').then(module => ({ default: module.Workbench })));

export function OfficialPage() {
  const { page, id, turnID, eventID, navigate } = useNavigation();
  const assistants = useQuery<Assistant[]>('/api/assistants', true);
  const settings = useQuery<SettingsDTO>('/api/settings');
  const runtime = useQuery<RuntimeInfo>('/api/status');
  const [search, setSearch] = useState(false);
  const assistantMemory = useRef<AssistantsPageMemory>({});
  const workbenchVisible = !!runtime.data?.workbench_available && !!settings.data?.WorkbenchVisible;
  useErrorToast(assistants.error, assistants.reload);
  useEffect(() => {
    const open = () => setSearch(true);
    window.addEventListener('molr:search', open);
    return () => window.removeEventListener('molr:search', open);
  }, []);
  function go(next: RoutePage, objectID?: string, turnID?: string, messageID?: string) {
    setSearch(false);
    navigate(next, objectID, turnID, messageID);
  }
  function openTasks(assistantID: string, taskID?: string) {
    guardedNavigation(() => {
      setSearch(false);
      const query = new URLSearchParams({ panel: 'tasks' });
      if (taskID) query.set('task', taskID);
      location.hash = `/assistants/${encodeURIComponent(assistantID)}?${query}`;
    });
  }
  return <>
    <Suspense fallback={<Loading />}>
      {page === 'assistants' ? <AssistantsPage assistants={assistants} settings={settings.data} workbenchVisible={workbenchVisible} memory={assistantMemory.current} onSearch={() => setSearch(true)} />
        : page === 'plans' || page === 'todos' || page === 'schedules' ? <Plans key={id} assistantID={id.startsWith('assistant-') ? id.slice(10) : undefined} selectedID={id} assistants={assistants.data ?? []} />
        : page === 'resources' ? <Resources resourceID={id} />
        : page === 'workbench' ? <Workbench turnID={turnID} eventID={eventID} timezone={settings.data?.Timezone} available={!!runtime.data?.workbench_available} assistants={assistants.data ?? []} assistantID={id} onSelectAssistant={value => go('workbench', value)} onSettings={() => go('account', 'general')} />
        : <Settings initialTab={id} onSelectSection={value => go('account', value)} assistants={assistants.data ?? []} openAssistant={value => go('assistants', value)} />}
    </Suspense>
    <Search open={search} onClose={() => setSearch(false)} navigate={go} onOpenTasks={openTasks} />
  </>;
}
