import { useState } from 'react';
import { Button, Drawer, EmptyState, SearchField, Tabs } from '@molr/ui';
import { Layers, Search, Wrench } from 'lucide-react';
import type { ToolCapability } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { pluginResourceVisible, usePluginsVisible } from '../../../plugins/visibility';
import { Loading } from '../components/common';
import { useErrorToast } from '../../../error-toasts';
import { toolGroup, toolGroups } from './tool-groups';
import type { ToolGroup } from './tool-groups';
import './tools-catalog.css';
import { ToolCatalogRow } from './ToolCatalogRow';
import { ToolDetails } from './ToolDetails';
import { MCPConnections } from './MCPConnections';

export function ToolsCatalog() {
 const {locale}=useCopy(), zh=locale==='zh', pluginsVisible=usePluginsVisible();
 const [source,setSource]=useState('builtin');
 return <div className="tools-workspace"><Tabs label={zh?'工具来源':'Tool source'} value={source} onValueChange={setSource} items={[
 {value:'builtin',label:zh?'内置工具':'Built-in tools',content:source==='builtin'?<ToolList source="builtin"/>:null},
 {value:'mcp',label:zh?'MCP 工具':'MCP tools',content:source==='mcp'?<MCPConnections/>:null},
 ...(pluginsVisible?[{value:'plugin',label:zh?'插件工具':'Plugin tools',content:source==='plugin'?<ToolList source="plugin"/>:null}]:[]),
 ]}/></div>;
}
function ToolList({source}:{source:'builtin'|'plugin'}) {
  const { t } = useCopy(), copy = t.toolsCatalog, pluginsVisible = usePluginsVisible();
  const query = useQuery<ToolCapability[]>('/api/tools');
  useErrorToast(query.error, query.reload);
  const [search, setSearch] = useState(''), [group, setGroup] = useState<ToolGroup | 'all'>('all');
  const [selected, setSelected] = useState<string | null>(null);
  const labels = t.assistantInfo.toolLabels as Record<string, { name: string; description: string }>;
  const tools = (query.data ?? []).filter(tool => (tool.Source ?? (tool.Name.startsWith('plg_') ? 'plugin' : 'builtin')) === source && pluginResourceVisible('tool', pluginsVisible, tool.Name));
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
    {query.loading && !query.data && <Loading />}
    {query.data && <>
      <div className="tools-catalog-summary" role="status">{copy.results(shown.length, tools.length)}</div>
      {!shown.length ? <div className="tools-catalog-empty"><EmptyState illustration={tools.length ? <Search /> : <Wrench />} title={tools.length ? t.noResults : copy.empty} description={tools.length ? copy.noResultsHelp : copy.emptyHelp} />{tools.length > 0 && <Button variant="secondary" size="sm" onClick={() => { setSearch(''); setGroup('all'); }}>{copy.reset}</Button>}</div> : groups.filter(item => group === 'all' || group === item.key).map(item => item.tools.length > 0 && <section className="tools-catalog-group" key={item.key} aria-label={t.assistantInfo.toolGroups[item.key]}>
        <h3><item.icon size={16} aria-hidden="true" />{t.assistantInfo.toolGroups[item.key]}<span>{item.tools.length}</span></h3>
        <ul>{item.tools.map(tool => <li key={tool.Name}><ToolCatalogRow name={labels[tool.Name]?.name ?? tool.Name} description={labels[tool.Name]?.description ?? tool.Description} icon={item.icon} onOpen={() => setSelected(tool.Name)} /></li>)}</ul>
      </section>)}
      <p className="tools-catalog-help">{copy.permissionsHelp}</p>
    </>}
    <Drawer open={!!detail} title={detail ? labels[detail.Name]?.name ?? detail.Name : t.assistantInfo.toolDetails} description={detail?.Name} className="tool-catalog-detail" onOpenChange={open => { if (!open) setSelected(null); }}>
      {detail && <ToolDetails key={detail.Name} tool={detail} summary={labels[detail.Name]?.description} />}
    </Drawer>
  </div>;
}
