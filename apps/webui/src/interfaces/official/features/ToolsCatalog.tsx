import { useState } from 'react';
import { Badge, Button, Drawer, EmptyState, SearchField, Tabs } from '@molr/ui';
import { ChevronRight, Eye, Layers, PenLine, Search, Wrench } from 'lucide-react';
import type { ToolCapability } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { pluginResourceVisible, usePluginsVisible } from '../../../plugins/visibility';
import { ErrorNotice, Loading } from '../components/common';
import { toolGroup, toolGroups } from './tool-groups';
import type { ToolGroup } from './tool-groups';
import './tools-catalog.css';

export function ToolsCatalog() {
  const { t } = useCopy(), copy = t.toolsCatalog, pluginsVisible = usePluginsVisible();
  const query = useQuery<ToolCapability[]>('/api/tools');
  const [search, setSearch] = useState(''), [group, setGroup] = useState<ToolGroup | 'all'>('all');
  const [selected, setSelected] = useState<string | null>(null);
  const labels = t.assistantInfo.toolLabels as Record<string, { name: string; description: string }>;
  const tools = (query.data ?? []).filter(tool => pluginResourceVisible('tool', pluginsVisible, tool.Name));
  const terms = search.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  const matches = tools.filter(tool => {
    const text = [tool.Name, labels[tool.Name]?.name, labels[tool.Name]?.description ?? tool.Description, t.assistantInfo.toolGroups[toolGroup(tool.Name)]].join(' ').toLocaleLowerCase();
    return terms.every(term => text.includes(term));
  });
  const groups = toolGroups.map(item => ({ ...item, tools: matches.filter(tool => toolGroup(tool.Name) === item.key) }));
  const shown = group === 'all' ? matches : groups.find(item => item.key === group)!.tools;
  const detail = tools.find(tool => tool.Name === selected);
  const filters = [{ key: 'all' as const, icon: Layers, label: t.all, count: matches.length }, ...groups.filter(item => tools.some(tool => toolGroup(tool.Name) === item.key)).map(item => ({ ...item, label: t.assistantInfo.toolGroups[item.key], count: item.tools.length }))];

  return <div className="tools-catalog">
    <header className="tools-catalog-heading"><div><h2>{copy.title}</h2><p>{copy.description}</p></div>
      <SearchField label={copy.search} placeholder={copy.search} hideLabel value={search} onValueChange={setSearch} />
    </header>
    <div className="tools-catalog-filters" role="group" aria-label={copy.categories}>{filters.map(item => <button type="button" key={item.key} aria-pressed={group === item.key} onClick={() => setGroup(item.key)}>
      <item.icon size={15} aria-hidden="true" /><span>{item.label}</span>{query.data && <span className="tools-catalog-count">{item.count}</span>}
    </button>)}</div>
    <ErrorNotice error={query.error} retry={query.reload} />
    {query.loading && !query.data && <Loading />}
    {query.data && <>
      <div className="tools-catalog-summary" role="status">{copy.results(shown.length, tools.length)}</div>
      {!shown.length ? <div className="tools-catalog-empty"><EmptyState illustration={tools.length ? <Search /> : <Wrench />} title={tools.length ? t.noResults : copy.empty} description={tools.length ? copy.noResultsHelp : copy.emptyHelp} />{tools.length > 0 && <Button variant="secondary" size="sm" onClick={() => { setSearch(''); setGroup('all'); }}>{copy.reset}</Button>}</div> : groups.filter(item => group === 'all' || group === item.key).map(item => item.tools.length > 0 && <section className="tools-catalog-group" key={item.key} aria-label={t.assistantInfo.toolGroups[item.key]}>
        <h3><item.icon size={16} aria-hidden="true" />{t.assistantInfo.toolGroups[item.key]}<span>{item.tools.length}</span></h3>
        <ul>{item.tools.map(tool => {
          const EffectIcon = tool.SideEffect ? PenLine : Eye;
          return <li key={tool.Name}><button type="button" className="tools-catalog-row" aria-haspopup="dialog" onClick={() => setSelected(tool.Name)}>
            <span className="tools-catalog-icon"><item.icon size={18} aria-hidden="true" /></span>
            <span className="tools-catalog-copy"><strong>{labels[tool.Name]?.name ?? tool.Name}</strong><span>{labels[tool.Name]?.description ?? tool.Description}</span></span>
            <span className="tools-catalog-effect"><EffectIcon size={13} aria-hidden="true" />{tool.SideEffect ? copy.changesData : copy.readOnly}</span>
            <ChevronRight className="tools-catalog-chevron" size={16} aria-hidden="true" />
          </button></li>;
        })}</ul>
      </section>)}
      <p className="tools-catalog-help">{copy.permissionsHelp}</p>
    </>}
    <Drawer open={!!detail} title={detail ? labels[detail.Name]?.name ?? detail.Name : t.assistantInfo.toolDetails} description={detail?.Name} className="tool-catalog-detail" onOpenChange={open => { if (!open) setSelected(null); }}>
      {detail && <ToolDetails key={detail.Name} tool={detail} summary={labels[detail.Name]?.description} />}
    </Drawer>
  </div>;
}

function ToolDetails({ tool, summary }: { tool: ToolCapability; summary?: string }) {
  const { t } = useCopy(), copy = t.toolsCatalog;
  const [tab, setTab] = useState('description');
  const EffectIcon = tool.SideEffect ? PenLine : Eye;
  return <div className="tool-catalog-content">
    <section className="tool-catalog-overview" aria-label={copy.overview}>
      <Badge dot={false}>{t.assistantInfo.toolGroups[toolGroup(tool.Name)]}</Badge>
      {summary && <p className="tool-catalog-description">{summary}</p>}
      <dl className="tool-catalog-facts">
        <div><dt>{t.assistantInfo.toolEffect}</dt><dd><EffectIcon size={15} aria-hidden="true" /><span>{tool.SideEffect ? t.assistantInfo.writesData : t.assistantInfo.readOnly}</span></dd></div>
        <div><dt>{t.assistantInfo.requiredActions}</dt><dd>{tool.RequiredActions.map(action => (t.resourcePermissions.generic as Record<string, string>)[action] ?? action).join(' / ') || t.none}</dd></div>
      </dl>
      <p className="tool-catalog-permissions">{copy.permissionsHelp}</p>
    </section>
    <Tabs className="tool-catalog-definition" label={copy.definition} value={tab} onValueChange={setTab} items={[
      { value: 'description', label: copy.modelDescription, content: <div className="tool-catalog-model-description">{tool.Description || copy.noDescription}</div> },
      { value: 'schema', label: t.assistantInfo.inputSchema, content: <pre className="tool-catalog-schema" tabIndex={0} aria-label={t.assistantInfo.inputSchema}>{JSON.stringify(tool.InputSchema, null, 2)}</pre> },
    ]} />
  </div>;
}
