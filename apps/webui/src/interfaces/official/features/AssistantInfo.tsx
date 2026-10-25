import { useErrorToast } from '../../../error-toasts';
import { usePluginsVisible, pluginResourceVisible } from '../../../plugins/visibility';
import { Activity, useEffect, useId, useState } from 'react';
import { Badge, Button, EmptyState, Tabs } from '@molr/ui';
import { BookOpen, ChevronDown, FlaskConical, Info, Shield, Wrench } from 'lucide-react';
import type { Assistant, AssistantCapabilities, CurrencyContext, Model } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { CurrencyDialog } from '../components/Money';
import { Loading, useAction } from '../components/common';
import { AssistantSettingsForm } from './AssistantDialogs';
import { AssistantPermissions } from './Resources';
import { sandboxReason } from '../../../sandbox';
import { toolGroup, toolGroups } from './tool-groups';

type Tab = 'overview' | 'tools' | 'permissions' | 'skills';
type Props = { assistant: Assistant; models: Model[]; initialTab?: Tab; docked?: boolean; onClose: () => void; onDebug?: () => void };
export function AssistantInfo(props: Props) {
  const { t } = useCopy(), action = useAction(), formID = useId();
  const [tab, setTab] = useState<Tab>(props.initialTab ?? 'overview'), [submitDisabled, setSubmitDisabled] = useState(true);
  useEffect(() => { setTab(props.initialTab ?? 'overview'); }, [props.initialTab]);
  const models = useQuery<Model[]>('/api/models');
  useErrorToast(models.error, models.reload);
  return <CurrencyDialog drawer docked={props.docked} className="assistant-info" title={t.assistantInfo.title} description={props.assistant.Name} dismissible={!action.busy} onClose={props.onClose} footer={tab === 'overview' && <>
      <Button variant="secondary" disabled={action.busy} onClick={props.onClose}>{t.cancel}</Button>
      <Button type="submit" form={formID} disabled={submitDisabled || props.assistant.State !== 'active'} loading={action.busy}>{t.save}</Button>
    </>}>
    {context => <><InfoContents {...props} models={models.data ?? props.models} context={context} action={action} tab={tab} onTabChange={setTab} formID={formID} onSubmitDisabledChange={setSubmitDisabled} /></>}
  </CurrencyDialog>;
}
function InfoContents({ assistant, models, onClose, onDebug, context, action, tab, onTabChange, formID, onSubmitDisabledChange }: Props & { context: CurrencyContext; action: ReturnType<typeof useAction>; tab: Tab; onTabChange: (tab: Tab) => void; formID: string; onSubmitDisabledChange: (disabled: boolean) => void }) {
  const { t } = useCopy();
  const [visited, setVisited] = useState(() => new Set<Tab>([tab]));
  useEffect(() => { setVisited(previous => new Set([...previous, tab])); }, [tab]);
  const archived = assistant.State !== 'active';
  const items = [
    { value: 'overview', label: t.assistantInfo.overview, icon: <Info size={16} />, content: <>
      {onDebug && <div className="assistant-info-debug"><Button size="sm" variant="secondary" onClick={onDebug}><FlaskConical size={16} />{t.assistantInfo.executions}</Button></div>}
      <fieldset disabled={archived} className="assistant-info-fields"><AssistantSettingsForm key={assistant.ID} assistant={assistant} models={models} currencyContext={context} action={action} onClose={onClose} formID={formID} onSubmitDisabledChange={onSubmitDisabledChange} /></fieldset>
    </> },
    { value: 'tools', label: t.assistantInfo.tools, icon: <Wrench size={16} />, content: <Capabilities assistantID={assistant.ID} kind="tools" onPermissions={() => changeTab('permissions')} /> },
    { value: 'permissions', label: t.assistants.permissions, icon: <Shield size={16} />, content: <AssistantPermissions assistant={assistant} /> },
    { value: 'skills', label: t.assistantInfo.skills, icon: <BookOpen size={16} />, content: <Capabilities assistantID={assistant.ID} kind="skills" onPermissions={() => changeTab('permissions')} /> },
  ];
  function changeTab(value: string) { const next = value as Tab; onTabChange(next); setVisited(previous => new Set([...previous, next])); }
  return <Tabs label={t.assistantInfo.title} value={tab} onValueChange={changeTab} items={items.map(item => ({ ...item, disabled: action.busy && item.value !== tab, content: visited.has(item.value as Tab) ? <Activity mode={tab === item.value ? 'visible' : 'hidden'}>{item.content}</Activity> : null }))} />;
}
function Capabilities({ assistantID, kind, onPermissions }: { assistantID: string; kind: 'tools' | 'skills'; onPermissions: () => void }) {
  const pluginsVisible = usePluginsVisible();
  const { t } = useCopy(), query = useQuery<AssistantCapabilities>(`/api/assistants/${assistantID}/capabilities`);
  const visibleTools = query.data?.Tools.filter(tool => pluginResourceVisible('tool', pluginsVisible, tool.Name)) ?? [];
  const tools = t.assistantInfo.toolLabels as Record<string, { name: string; description: string }>;
  const reasons = t.assistantInfo.reasons as Record<string, string>;
  useErrorToast(query.error, query.reload);
  return <div className="assistant-capabilities">
    {query.loading && !query.data && <Loading />}
    {query.data && (kind === 'skills' ? !query.data.Skills.Supported || !query.data.Skills.Items.length ? <EmptyState illustration={<BookOpen />} title={query.data.Skills.Supported ? t.assistantInfo.noSkills : t.assistantInfo.skillsUnsupported} description={query.data.Skills.Supported ? t.assistantInfo.noSkillsHelp : t.assistantInfo.skillsUnsupportedHelp} /> : query.data.Skills.Items.map(skill => <article className="assistant-capability" key={skill.ID}>
      <div className="record-heading"><h3>{skill.Name}</h3>{skill.ID.startsWith('/molr/') && <Badge tone="neutral">{t.assistantInfo.builtinSkill}</Badge>}<Badge tone={skill.Enabled ? 'positive' : 'neutral'}>{skill.Enabled ? t.enabled : t.disabled}</Badge></div><p>{skill.ID === '/molr/.agents/skills/molr-docs/SKILL.md' ? t.assistantInfo.builtinDocsHelp : skill.Description}</p><small className="muted" title={skill.Version}>{t.assistantInfo.version} · {skill.ID.startsWith('/molr/') ? skill.Version.slice(0, 12) : skill.Version}</small>
      <p>{t.assistantInfo.dependencies} · {skill.RequiredTools.map(name => tools[name]?.name ?? name).join('、') || t.none}</p>
      {!skill.ID.startsWith('/molr/') && !!skill.RequiredActions.length && <><p>{t.assistantInfo.requiredActions} · {skill.RequiredActions.map(value => (t.resourcePermissions.generic as Record<string, string>)[value] ?? value).join(' / ')}</p><Button size="sm" variant="secondary" onClick={onPermissions}>{t.assistantInfo.configurePermissions}</Button></>}
    </article>) : <>
      <div className="assistant-info-toolbar"><p className="assistant-info-help">{t.assistantInfo.toolsSummary(visibleTools.length)}</p><Button variant="ghost" size="sm" onClick={onPermissions}><Shield size={14} />{t.assistantInfo.configurePermissions}</Button></div>
      {!visibleTools.length ? <EmptyState title={t.assistantInfo.noTools} /> : toolGroups.map(group => {
        const items = visibleTools.filter(tool => toolGroup(tool.Name) === group.key);
        if (!items.length) return null;
        const Icon = group.icon;
        return <section className="assistant-tool-group" key={group.key} aria-label={t.assistantInfo.toolGroups[group.key]}>
          <div className="assistant-info-section-heading"><h3><Icon size={16} aria-hidden="true" />{t.assistantInfo.toolGroups[group.key]}</h3><Badge size="sm" dot={false}>{items.length}</Badge></div>
          <div className="assistant-tool-list">{items.map(tool => <article className="assistant-tool" key={tool.Name}>
            <details className="assistant-tool-disclosure">
              <summary aria-label={`${tools[tool.Name]?.name ?? tool.Name} · ${t.assistantInfo.toolDetails}`}>
                <div className="assistant-tool-title"><h4>{tools[tool.Name]?.name ?? tool.Name}</h4><Badge size="sm" tone={tool.Available ? 'positive' : 'neutral'}>{tool.Available ? t.assistantInfo.available : t.assistantInfo.unavailable}</Badge><ChevronDown size={16} aria-hidden="true" /></div>
                <p className="assistant-tool-description">{tools[tool.Name]?.description ?? tool.Description}</p>
                {!tool.Available && <p className="assistant-tool-condition">{reasons[tool.Reason] ?? (tool.Reason.startsWith('sandbox_') ? sandboxReason(t, tool.Reason) : tool.Reason)}</p>}
              </summary>
              <div className="assistant-tool-details-content">
                {tool.Available && <p>{reasons[tool.Reason] ?? (tool.Reason.startsWith('sandbox_') ? sandboxReason(t, tool.Reason) : tool.Reason)}</p>}
                <dl className="assistant-info-facts"><div><dt>{t.assistantInfo.toolIdentifier}</dt><dd><code>{tool.Name}</code></dd></div><div><dt>{t.assistantInfo.toolEffect}</dt><dd>{tool.SideEffect ? t.assistantInfo.writesData : t.assistantInfo.readOnly}</dd></div><div><dt>{t.assistantInfo.requiredActions}</dt><dd>{tool.RequiredActions.map(value => (t.resourcePermissions.generic as Record<string, string> | undefined)?.[value] ?? value).join(' / ') || t.none}</dd></div></dl>
                <details className="assistant-tool-schema"><summary>{t.assistantInfo.inputSchema}</summary><pre>{JSON.stringify(tool.InputSchema, null, 2)}</pre></details>
              </div>
            </details>
          </article>)}</div>
        </section>;
      })}
      <p className="assistant-info-help">{t.assistantInfo.toolsHelp}</p>
    </>)}
  </div>;
}
