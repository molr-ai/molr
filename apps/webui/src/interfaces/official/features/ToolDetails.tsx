import { useState } from 'react';
import { Badge, Tabs } from '@molr/ui';
import { Eye, PenLine } from 'lucide-react';
import type { ToolCapability } from '../../../api/types';
import { useCopy } from '../../../state';
import { toolGroup } from './tool-groups';

export function ToolDetails({ tool, summary, sourceLabel }: { tool: ToolCapability; summary?: string; sourceLabel?: string }) {
  const { t } = useCopy(), copy = t.toolsCatalog;
  const [tab, setTab] = useState('description');
  const EffectIcon = tool.SideEffect ? PenLine : Eye;
  return <div className="tool-catalog-content">
    <section className="tool-catalog-overview" aria-label={copy.overview}>
      <Badge dot={false}>{sourceLabel ?? t.assistantInfo.toolGroups[toolGroup(tool.Name)]}</Badge>
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
