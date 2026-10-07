import { productMessages } from './i18n';
import { useEffect, useState } from 'react';
import { Bot, CalendarDays, Folder, ListTodo, MessageSquare, Settings } from 'lucide-react';
import { SearchPanel } from '@molr/ui';
import { demoResources } from './interface-demo';
import type { InterfaceDemo } from './interface-demo';
import type { Locale } from './messages';

export type SearchDestination =
  | { kind: 'page'; page: 'plans' | 'home' | 'resources' | 'todos' | 'schedule' | 'settings' }
  | { kind: 'assistant'; id: string }
  | { kind: 'resource'; id: string }
  | { kind: 'task'; id?: string };
type Source = 'chats' | 'assistants' | 'resources' | 'tasks' | 'todos';
type SearchItem = { id: string; source: Source; title: string; description: string; keywords: string; destination: SearchDestination };

export function WorkspaceSearchPanel({ open, onOpenChange, locale, demo, onNavigate, mobile = false }: {
  open: boolean; onOpenChange: (open: boolean) => void; locale: Locale; demo: InterfaceDemo; onNavigate: (destination: SearchDestination) => void; mobile?: boolean;
}) {
  const t = productMessages[locale];
  const [query, setQuery] = useState('');
  const [scopes, setScopes] = useState<Source[]>([]);
  useEffect(() => { if (!open) { setQuery(''); setScopes([]); } }, [open]);
  const sources = [
    { value: 'chats' as const, label: t.workspaceSearchPanel.chats, icon: MessageSquare },
    { value: 'assistants' as const, label: t.workspaceSearchPanel.assistants, icon: Bot },
    { value: 'resources' as const, label: t.common.resources, icon: Folder },
    { value: 'tasks' as const, label: t.common.tasks, icon: ListTodo },
    { value: 'todos' as const, label: t.common.todos, icon: CalendarDays },
  ];
  const shortcuts: { label: string; description: string; icon: typeof Bot; destination: SearchDestination }[] = [
    { label: t.workspaceSearchPanel.assistants, description: t.workspaceSearchPanel.continueAConversation, icon: Bot, destination: { kind: 'page', page: 'home' } },
    { label: t.common.resources, description: t.workspaceSearchPanel.filesAndTools, icon: Folder, destination: { kind: 'page', page: 'resources' } },
    { label: locale === 'zh' ? '计划' : 'Plans', description: t.workspaceSearchPanel.yourUpcomingPlans, icon: CalendarDays, destination: { kind: 'page', page: 'plans' } },
    { label: t.common.settings, description: t.workspaceSearchPanel.preferencesAndConnections, icon: Settings, destination: { kind: 'page', page: 'settings' } },
  ];
  const items: SearchItem[] = [
    ...demo.assistants.flatMap(assistant => assistant.messages.filter(message => message.text || message.attachments?.length).map(message => ({
      id: `chat-${assistant.id}-${message.id}`, source: 'chats' as const,
      title: locale === 'en' && message.textEn ? message.textEn : message.text || message.attachments!.map(file => file.name).join(', '),
      description: `${assistant.name} · ${message.role === 'user' ? t.workspaceSearchPanel.sentByYou : t.workspaceSearchPanel.assistantMessage}`,
      keywords: [assistant.id, assistant.name, message.text, message.textEn, ...(message.attachments?.map(file => file.name) ?? [])].join(' '),
      destination: { kind: 'assistant' as const, id: assistant.id },
    }))),
    ...demo.assistants.map(assistant => ({
      id: `assistant-${assistant.id}`, source: 'assistants' as const, title: assistant.name,
      description: t.workspaceSearchPanel.messagesOpenConversation(assistant.messages.length),
      keywords: `${assistant.name} ${assistant.id}`, destination: { kind: 'assistant' as const, id: assistant.id },
    })),
    ...demoResources.map(resource => ({
      id: `resource-${resource.id}`, source: 'resources' as const, title: resource[locale],
      description: `${resource.category} · ${resource.path}`, keywords: `${resource.zh} ${resource.en} ${resource.path} ${resource.files.join(' ')}`,
      destination: { kind: 'resource' as const, id: resource.id },
    })),
    ...demo.works.map(work => ({
      id: `task-${work.id}`, source: 'tasks' as const, title: (locale === 'zh' ? work.title : work.titleEn),
      description: `${demo.activeAssistant.name} · #${work.id}`, keywords: `${work.title} ${work.titleEn} ${work.id}`,
      destination: { kind: 'task' as const, id: work.id },
    })),
    ...demo.todos.map(todo => ({
      id: `todo-${todo.id}`, source: 'todos' as const, title: todo.title,
      description: todo.completed ? t.common.completed : t.workspaceSearchPanel.toDoOpenTodos,
      keywords: `${todo.title} ${todo.notes}`, destination: { kind: 'page' as const, page: 'plans' as const },
    })),
  ];
  const term = query.trim().toLowerCase();
  const matches = term ? items.filter(item => term.split(/\s+/).every(word => `${item.title} ${item.description} ${item.keywords}`.toLowerCase().includes(word))) : [];
  const visible = matches.filter(item => !scopes.length || scopes.includes(item.source));
  return <SearchPanel open={open} onOpenChange={onOpenChange} mobile={mobile}
    query={query} onQueryChange={setQuery} scopes={scopes} onScopesChange={values => setScopes(values as Source[])}
    sources={sources.map(({ icon: Icon, ...source }) => ({ ...source, icon: <Icon size={18} aria-hidden="true" /> }))}
    shortcuts={shortcuts.map(({ label, description, icon: Icon, destination }) => ({ id: label, label, description, icon: <Icon size={18} aria-hidden="true" />, onSelect: () => onNavigate(destination) }))}
    items={visible.map(item => ({ ...item, onSelect: () => onNavigate(item.destination) }))}
    suggestions={[t.workspaceSearchPanel.travel, t.workspaceSearchPanel.notes, 'Molr']}
    labels={{ title: t.common.search, input: t.workspaceSearchPanel.searchChatsAssistantsResourcesAndMore, placeholder: t.workspaceSearchPanel.searchChatsAssistantsResources,
      clear: t.common.clearSearch, filter: t.workspaceSearchPanel.filterSearchTypes, all: t.common.all,
      quickNavigation: t.workspaceSearchPanel.quickNavigation, trySearching: t.workspaceSearchPanel.trySearching,
      resultsFound: t.workspaceSearchPanel.resultsFound, emptyTitle: t.common.noMatchingResults,
      emptyHelp: t.workspaceSearchPanel.tryAnotherKeywordOrSearchAcrossAllTypes, reset: t.workspaceSearchPanel.startANewSearch,
      keyboardHint: t.workspaceSearchPanel.toChooseToOpenEscToClose }} />;
}
