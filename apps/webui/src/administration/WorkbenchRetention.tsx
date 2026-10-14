import { useErrorToast } from '../error-toasts';
import { useState } from 'react';
import type { DebugRetentionSettings as RetentionSettings } from '../api/types';
import { Button, TextField } from '@molr/ui';
import { api } from '../api/client';
import { useCopy, useData, useQuery } from '../state';
import { SettingsHelp } from '../interfaces/official/components/SettingsHelp';
import { Loading, useAction } from '../interfaces/official/components/common';

export function WorkbenchRetentionSettings() {
  const query = useQuery<RetentionSettings>('/api/admin/debug/retention');
  const [saved, setSaved] = useState(false);
  const { locale } = useCopy();
  useErrorToast(query.error, query.reload);
  return <>{query.data ? <RetentionForm key={query.data.Version} policy={query.data} reload={query.reload} onSaved={() => setSaved(true)} /> : <Loading />}{saved && <p role="status">{locale === 'zh' ? '保留策略已保存' : 'Retention policy saved'}</p>}</>;
}
function RetentionForm({ policy, reload, onSaved }: { policy: RetentionSettings; reload: () => void; onSaved: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const { invalidateTopics } = useData(), action = useAction();
  const [runs, setRuns] = useState(policy.Configured.MaxRuns?.toString() ?? '');
  const label = zh ? '执行次数上限' : 'Maximum executions';
  const help = zh ? '账号下所有助理共享的历史执行条数，包含做梦记录。超过上限时删除最早的已完成执行；运行中的执行保留到结束。上下文只在没有任何业务或历史引用后回收。' : 'History across all assistants in each account, including dreams. Oldest completed executions are removed first; running executions remain until completion. Context is reclaimed only after all execution and history references are released.';
  useErrorToast(action.error, () => { action.reset(); reload(); });
  return <form className="settings-section retention-settings" onSubmit={event => { event.preventDefault(); void action.run(async () => {
    await api.mutate('/api/admin/debug/retention', { Policy: { MaxRuns: runs === '' ? null : Number(runs) } }, policy.Version);
    onSaved(); reload(); invalidateTopics(['debug', 'admin']);
  }, undefined, false); }}>
    <p className="settings-description">{zh ? '每个账号独立计算额度。留空使用输入框提示的默认值。' : 'Limits apply independently to each account. Leave fields empty to use the placeholder defaults.'}</p>
    <section className="settings-group"><h3 className="settings-group-title">{zh ? '自动清理' : 'Automatic cleanup'}</h3><div className="settings-list">
      <div className="settings-row settings-row--with-help"><span className="settings-label">{label}<SettingsHelp label={label} content={help} /></span>
        <div className="retention-setting-control"><TextField className="retention-setting-input" label="" aria-label={label} type="number" min={1} max={10000} step={1} placeholder={String(policy.Defaults.MaxRuns)} value={runs} onChange={event => setRuns(event.target.value)} disabled={action.busy} /><span className="retention-setting-unit" aria-hidden="true">{zh ? '条' : 'runs'}</span></div>
      </div></div></section>
    <p className="settings-description">{zh ? '仅按执行条数清理，不限制保留时间或详情总大小。降低上限会在后续写入时清理更早的记录，不会影响 Agent 当前上下文。' : 'History is limited by execution count, not age or total detail size. Lower limits remove older records on subsequent writes without removing the current Agent context.'}</p>
    <div><Button size="sm" type="submit" disabled={action.busy}>{zh ? '保存保留策略' : 'Save retention policy'}</Button></div>
  </form>;
}
