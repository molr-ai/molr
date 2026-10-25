import { useErrorToast } from '../../../error-toasts';
import { useEffect, useState } from 'react';
import { Button, SearchPanel, useBreakpoint } from '@molr/ui';
import { Bot, Brain, CalendarDays, Folder, ListTodo, MessageSquare, Settings } from 'lucide-react';
import type { SearchHit, SearchResults } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { Loading } from '../components/common';
import type { RoutePage } from '../../../navigation';

export function Search({ open, onClose, navigate, onOpenTasks }: {
  open: boolean; onClose: () => void; navigate: (page: RoutePage, id?: string, turnID?: string, messageID?: string) => void;
  onOpenTasks: (assistantID: string, taskID?: string) => void;
}) {
  const { t } = useCopy(), mobile = useBreakpoint('mobile');
  const [text, setText] = useState(''), [debounced, setDebounced] = useState(''), [scopes, setScopes] = useState<string[]>([]), [cursors, setCursors] = useState<string[]>(['']);
  useEffect(() => { if (!open) { setText(''); setDebounced(''); setScopes([]); setCursors(['']); } }, [open]);
  const term = text.trim();
  useEffect(() => {
    if (term === debounced) return;
    const timer = setTimeout(() => { setDebounced(term); setCursors(['']); }, 250);
    return () => clearTimeout(timer);
  }, [term, debounced]);
  const params = new URLSearchParams({ q: debounced, limit: '50', cursor: cursors.at(-1) ?? '' }); scopes.forEach(value => params.append('source', value));
  const query = useQuery<SearchResults>(open && debounced && term === debounced ? `/api/search?${params}` : null);
  const busy = query.loading || term !== debounced;
  useErrorToast(query.error, query.reload);
  const sources = [
    { value: 'messages', label: t.searchTypes.messages, icon: <MessageSquare size={18} aria-hidden="true" /> },
    { value: 'assistants', label: t.searchTypes.assistants, icon: <Bot size={18} aria-hidden="true" /> },
    { value: 'resources', label: t.searchTypes.resources, icon: <Folder size={18} aria-hidden="true" /> },
    { value: 'tasks', label: t.searchTypes.tasks, icon: <ListTodo size={18} aria-hidden="true" /> },
    { value: 'memories', label: t.searchTypes.memories, icon: <Brain size={18} aria-hidden="true" /> },
  ];
  function selectHit(hit: SearchHit) {
    if (hit.Source === 'messages') navigate('assistants', hit.AssistantID, '', hit.Object.ID);
    else if (hit.Source === 'tasks') onOpenTasks(hit.AssistantID, hit.Object.ID);
    else if (hit.Source === 'resources') navigate('resources', hit.Object.ID);
    else if (hit.Source === 'memories' && hit.Memory) { const r = hit.Memory; location.hash = '/account/memory?' + new URLSearchParams({ scope: r.Scope, assistant_id: r.AssistantID, path: r.Path }); }
    else navigate('assistants', hit.AssistantID || hit.Object.ID);
  }
  return <SearchPanel open={open} onOpenChange={value => { if (!value) onClose(); }} mobile={mobile}
    query={text} onQueryChange={setText} scopes={scopes} onScopesChange={values => { setScopes(values); setCursors(['']); }} sources={sources}
    shortcuts={[
      { id: 'assistants', label: t.nav.assistants, description: t.searchPanel.continueConversation, icon: <Bot size={18} aria-hidden="true" />, onSelect: () => navigate('assistants') },
      { id: 'resources', label: t.nav.resources, description: t.searchPanel.filesAndTools, icon: <Folder size={18} aria-hidden="true" />, onSelect: () => navigate('resources') },
      { id: 'plans', label: t.nav.plans, description: t.searchPanel.upcomingPlans, icon: <CalendarDays size={18} aria-hidden="true" />, onSelect: () => navigate('plans') },
      { id: 'settings', label: t.nav.settings, description: t.searchPanel.preferencesAndConnections, icon: <Settings size={18} aria-hidden="true" />, onSelect: () => navigate('account') },
    ]}
    items={(query.data?.Hits.Items ?? []).map(hit => ({ id: hit.Memory ? JSON.stringify(hit.Memory) : `${hit.Source}-${hit.Object.ID}`, source: hit.Source, title: hit.Title || t.searchTypes[hit.Source], description: hit.Snippet, onSelect: () => selectHit(hit) }))}
    total={Object.values(query.data?.Counts ?? {}).reduce((sum, count) => sum + count, 0)}
    suggestions={[t.searchPanel.travel, t.searchPanel.notes, 'Molr']}
    labels={{ ...t.searchPanel, title: t.search, filter: t.filters, all: t.all, emptyTitle: t.noResults }}
    busy={busy} statusContent={busy && (!query.data || term !== debounced) ? <Loading key={text} delay={600} /> : undefined}
    pagination={(cursors.length > 1 || query.data?.Hits.NextCursor) && <div className="workspace-search__pagination">
      {cursors.length > 1 && <Button size="sm" variant="secondary" disabled={busy} onClick={() => setCursors(values => values.slice(0, -1))}>{t.previousPage}</Button>}
      {query.data?.Hits.NextCursor && <Button size="sm" variant="secondary" disabled={busy} onClick={() => setCursors(values => [...values, query.data!.Hits.NextCursor])}>{t.nextPage}</Button>}
    </div>} />;
}
