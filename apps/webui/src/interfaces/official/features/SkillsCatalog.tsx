import { useState } from 'react';
import { Badge, Button, Drawer, EmptyState, IconButton, SearchField, Select } from '@molr/ui';
import { BookOpen, Bot, Layers, RefreshCw, Search, Users } from 'lucide-react';
import type { CatalogSkill, SkillContent, SkillsCatalog as Catalog } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { Loading } from '../components/common';
import { ToolCatalogRow } from './ToolCatalogRow';
import './tools-catalog.css';
import './skills-catalog.css';

const skillKey = (skill: CatalogSkill) => JSON.stringify([skill.AssistantID, skill.Path]);

export function SkillsCatalog() {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const query = useQuery<Catalog>('/api/skills');
  const [search, setSearch] = useState(''), [source, setSource] = useState('all'), [assistant, setAssistant] = useState('all');
  const [selected, setSelected] = useState<string | null>(null);
  useErrorToast(query.error, query.reload);
  const items = query.data?.Items ?? [], assistants = query.data?.Assistants ?? [];
  const allAssistants = zh ? '所有助理' : 'All assistants';
  const owner = (skill: CatalogSkill) => skill.AssistantID ? assistants.find(a => a.ID === skill.AssistantID)?.Name ?? skill.AssistantID : allAssistants;
  const sources = [
    { key: 'builtin', label: zh ? '内置' : 'Built-in', icon: BookOpen },
    { key: 'shared', label: zh ? '账号共享' : 'Account shared', icon: Users },
    { key: 'assistant', label: zh ? '助理专属' : 'Assistant specific', icon: Bot },
  ];
  const terms = search.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  const summary = (skill: CatalogSkill) => skill.Source === 'builtin' ? t.assistantInfo.builtinDocsHelp : skill.Description || (zh ? '查看技能说明与工作流程。' : 'View the skill instructions and workflow.');
  const matches = items.filter(skill => (assistant === 'all' || !skill.AssistantID || skill.AssistantID === assistant) && terms.every(term => [skill.Name, skill.Path, summary(skill), owner(skill)].join(' ').toLocaleLowerCase().includes(term)));
  const groups = sources.map(group => ({ ...group, items: matches.filter(skill => skill.Source === group.key) }));
  const shown = matches.filter(skill => source === 'all' || skill.Source === source);
  const filters = [{ key: 'all', label: t.all, icon: Layers, count: matches.length }, ...groups.map(group => ({ ...group, count: group.items.length }))];
  const detail = items.find(skill => skillKey(skill) === selected);
  const reset = () => { setSearch(''); setSource('all'); setAssistant('all'); };
  return <div className="tools-catalog skills-catalog">
    <header className="tools-catalog-heading"><div><h2>Skills</h2><p>{zh ? '查看技能的用途、来源和适用助理。' : 'Explore skill workflows, sources and assistants.'}</p></div>
      <SearchField label={zh ? '搜索技能、用途或助理' : 'Search skills, workflows or assistants'} placeholder={zh ? '搜索技能、用途或助理' : 'Search skills, workflows or assistants'} hideLabel value={search} onValueChange={setSearch} />
    </header>
    <div className="skills-catalog-toolbar">
      <div className="tools-catalog-filters" role="group" aria-label={zh ? '技能来源' : 'Skill sources'}>{filters.map(filter => <button type="button" key={filter.key} aria-pressed={source === filter.key} onClick={() => setSource(filter.key)}><filter.icon size={15} aria-hidden="true" /><span>{filter.label}</span>{query.data && <span className="tools-catalog-count">{filter.count}</span>}</button>)}</div>
      <div className="skills-catalog-actions"><Select size="sm" label={zh ? '适用助理' : 'Assistant scope'} hideLabel value={assistant} onValueChange={setAssistant} searchable={assistants.length >= 8} options={[{ value: 'all', label: allAssistants }, ...assistants.map(a => ({ value: a.ID, label: a.Name }))]} /><IconButton label={zh ? '刷新技能' : 'Refresh skills'} disabled={query.loading} onClick={query.reload}><RefreshCw size={16} /></IconButton></div>
    </div>
    {query.loading && !query.data && <Loading />}
    {query.data && <>
      <div className="tools-catalog-summary" role="status">{zh ? `显示 ${shown.length} / ${items.length} 项技能` : `${shown.length} of ${items.length} skills`}</div>
      {!shown.length ? <div className="tools-catalog-empty"><EmptyState illustration={<Search />} title={t.noResults} description={zh ? '尝试其他关键词或调整筛选。' : 'Try another search or adjust the filters.'} /><Button variant="secondary" size="sm" onClick={reset}>{t.toolsCatalog.reset}</Button></div> : groups.filter(group => source === 'all' || source === group.key).map(group => !!group.items.length && <section className="tools-catalog-group" key={group.key} aria-label={group.label}>
        <h3><group.icon size={16} aria-hidden="true" />{group.label}<span>{group.items.length}</span></h3>
        <ul>{group.items.map(skill => <li key={skillKey(skill)}><ToolCatalogRow name={skill.Name} description={summary(skill)} status={owner(skill)} icon={group.icon} onOpen={() => setSelected(skillKey(skill))} /></li>)}</ul>
      </section>)}
      <p className="tools-catalog-help">{zh ? '在聊天中让助理安装或维护 Skill。技能提供流程指引，执行时沿用助理已有的工具与权限。' : 'Ask an assistant in chat to install or maintain a skill. Skills guide workflows using the assistant’s existing tools and permissions.'}</p>
    </>}
    <Drawer open={!!detail} title={detail?.Name ?? 'Skill'} description={detail?.Path} className="tool-catalog-detail" onOpenChange={open => { if (!open) setSelected(null); }}>
      {detail && <SkillDetails key={skillKey(detail)} skill={detail} summary={summary(detail)} source={sources.find(s => s.key === detail.Source)!.label} owner={owner(detail)} />}
    </Drawer>
  </div>;
}

function SkillDetails({ skill, summary, source, owner }: { skill: CatalogSkill; summary: string; source: string; owner: string }) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const content = useQuery<SkillContent>(`/api/skills?${new URLSearchParams({ path: skill.Path, version: skill.Version, ...(skill.AssistantID ? { assistant_id: skill.AssistantID } : {}) })}`);
  useErrorToast(content.error, content.reload);
  return <div className="tool-catalog-content">
    <section className="tool-catalog-overview"><Badge dot={false}>{source}{skill.Source === 'builtin' ? (zh ? ' · 只读' : ' · Read-only') : ''}</Badge><p className="tool-catalog-description">{summary}</p>
      <dl className="tool-catalog-facts"><div><dt>{zh ? '适用助理' : 'Assistant scope'}</dt><dd>{owner}</dd></div><div><dt>{t.assistantInfo.version}</dt><dd title={content.data?.Version ?? skill.Version}>{(content.data?.Version ?? skill.Version).slice(0, 12)}</dd></div></dl>
    </section>
    <section className="skills-catalog-instructions" aria-label={zh ? '技能原文' : 'Skill instructions'}><h3>SKILL.md</h3>{content.loading && !content.data && <Loading />}{content.data && <div className="tool-catalog-model-description">{content.data.Text}</div>}</section>
  </div>;
}
