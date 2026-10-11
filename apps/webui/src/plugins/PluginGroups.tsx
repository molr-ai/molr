import { useErrorToast } from '../error-toasts';
import { useState } from 'react';
import { Button, Checkbox, Dialog } from '@molr/ui';
import { api } from '@molr/sdk';
import type { PluginInstallation, PluginConnection } from '@molr/sdk/plugins';
import { useCopy, useQuery } from '../state';
import { groupName, emptyQuota } from '../administration/types';
import type { UserGroup } from '../administration/types';
import { Loading, useAction } from '../interfaces/official/components/common';

export function PluginGroups({ plugin, onClose }: { plugin: PluginInstallation; onClose: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', query = useQuery<UserGroup[]>('/api/admin/groups');
  useErrorToast(query.error, query.reload);
  return <Dialog open title={`${plugin.Manifest.name} · ${zh ? '授权用户组' : 'Authorize groups'}`} onOpenChange={open => { if (!open) onClose(); }} footer={<Button variant="secondary" onClick={onClose}>{zh ? '关闭' : 'Close'}</Button>}>
    <p className="muted">{zh ? '管理员始终可管理插件。按组开放插件及其工具、服务连接；计算资源权限和已有配额保持独立。' : 'Administrators always manage plugins. Grant group access to this plugin, its tools and connections. Compute permissions and existing quotas remain independent.'}</p>
    {!query.data ? <Loading /> : query.data.filter(g => !g.Administrator).map(group => <GroupAccess key={`${group.ID}:${group.Version}`} group={group} plugin={plugin} onSaved={query.reload} />)}
  </Dialog>;
}
function GroupAccess({ group, plugin, onSaved }: { group: UserGroup; plugin: PluginInstallation; onSaved: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', action = useAction();
  const granted = group.Resources.some(r => r.Kind === 'plugin' && r.ID === plugin.CatalogID);
  const [enabled, setEnabled] = useState(granted);
  useErrorToast(action.error);
  return <section className="plugin-group-access"><Checkbox label={groupName(group,zh)} checked={enabled} onChange={e => setEnabled(e.target.checked)} /><Button size="sm" variant="secondary" disabled={enabled === granted} loading={action.busy} onClick={() => { void action.run(async () => {
    const connections = await api.read<PluginConnection[]>('/api/plugins/connections/list', { PluginID: plugin.ID, Digest: plugin.Digest });
    const refs = [{ Kind: 'plugin' as const, ID: plugin.CatalogID }, ...(plugin.ToolIDs ?? []).map(ID => ({ Kind: 'tool' as const, ID })), ...connections.filter(c => c.Enabled && !c.NeedsReview).map(c => ({ Kind: 'connection' as const, ID: c.ID }))];
    const resources = enabled ? [...group.Resources, ...refs.filter(r => !group.Resources.some(old => old.Kind === r.Kind && old.ID === r.ID)).map(r => ({ ...r, Quota: emptyQuota() }))] : group.Resources.filter(r => r.Kind !== 'plugin' || r.ID !== plugin.CatalogID);
    await api.mutate('/api/admin/groups', { Group: { ...group, Resources: resources } }, group.Version); onSaved();
  }); }}>{zh ? '保存授权' : 'Save access'}</Button></section>;
}
