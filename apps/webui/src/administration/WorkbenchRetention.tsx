import { useState } from 'react';
import type { DebugRetentionSettings as RetentionSettings } from '../api/types';
import { Button, TextField } from '@molr/ui';
import { api } from '../api/client';
import { useCopy, useData, useQuery } from '../state';
import { SettingsHelp } from '../interfaces/official/components/SettingsHelp';
import { ErrorNotice, Loading, useAction } from '../interfaces/official/components/common';

export function WorkbenchRetentionSettings() {
  const query = useQuery<RetentionSettings>('/api/admin/debug/retention');
  const [saved, setSaved] = useState(false);
  const { locale } = useCopy();
  return <><ErrorNotice error={query.error} retry={query.reload} />{query.data ? <RetentionForm key={query.data.Version} policy={query.data} reload={query.reload} onSaved={() => setSaved(true)} /> : <Loading />}{saved && <p role="status">{locale === 'zh' ? '保留策略已保存' : 'Retention policy saved'}</p>}</>;
}
function RetentionForm({ policy, reload, onSaved }: { policy: RetentionSettings; reload: () => void; onSaved: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const { invalidateTopics } = useData(), action = useAction();
  const [days, setDays] = useState(policy.Configured.RetentionDays?.toString() ?? ''), [runs, setRuns] = useState(policy.Configured.MaxRuns?.toString() ?? ''), [size, setSize] = useState(policy.Configured.MaxBytes == null ? '' : String(policy.Configured.MaxBytes / 1048576)), [keep, setKeep] = useState(policy.Configured.MinRuns?.toString() ?? '');
  const defaultMinimum = Math.min(policy.Defaults.MinRuns, runs === '' ? policy.Defaults.MaxRuns : Number(runs));
  const fields = [
    { label: zh ? '保留天数' : 'Retention days', help: zh ? '超过保留天数的执行会在后续写入时清理。受保护记录不受时间限制。' : 'Older executions are cleaned up on subsequent writes. Protected history is exempt.', unit: zh ? '天' : 'days', value: days, change: setDays, placeholder: String(policy.Defaults.RetentionDays), max: 365 },
    { label: zh ? '执行次数上限' : 'Maximum executions', help: zh ? '账号下所有助理共享的执行记录条数上限，超限时从最早的未保护记录开始清理。' : 'Execution count across all assistants in each account. Oldest unprotected records are removed first.', unit: zh ? '条' : 'runs', value: runs, change: setRuns, placeholder: String(policy.Defaults.MaxRuns), max: 10000 },
    { label: zh ? '详情总大小' : 'Total detail size', ariaLabel: zh ? '详情总大小（MiB）' : 'Total detail size (MiB)', help: zh ? '所有执行详情的大小目标。已有受保护详情会保留；容量不足时，新增详情会标记为未采集。' : 'Size target for execution details. Protected details remain; new details may be skipped if the allowance is full.', unit: 'MiB', value: size, change: setSize, placeholder: String(policy.Defaults.MaxBytes / 1048576), max: 10240, step: 'any' },
    { label: zh ? '最低保留条数' : 'Minimum recent executions', ariaLabel: zh ? '至少保留最近已完成执行数' : 'Minimum recent completed executions', help: zh ? '始终保留最近这些已完成执行，运行中执行另外保护。保护规则优先于天数和大小目标，不能高于执行次数上限。' : 'Always keep these most recent completed executions, plus all running executions. Protection overrides age and size targets and cannot exceed the execution count limit.', unit: zh ? '条' : 'runs', value: keep, change: setKeep, placeholder: String(defaultMinimum), max: runs === '' ? policy.Defaults.MaxRuns : Number(runs) },
  ];
  const row = (field: typeof fields[number]) => <div className="settings-row settings-row--with-help" key={field.label}>
    <span className="settings-label">{field.label}<SettingsHelp label={field.label} content={field.help} /></span>
    <div className="retention-setting-control"><TextField className="retention-setting-input" label="" aria-label={field.ariaLabel ?? field.label} type="number" min={1} max={field.max} step={field.step ?? 1} placeholder={field.placeholder} value={field.value} onChange={e => field.change(e.target.value)} disabled={action.busy} /><span className="retention-setting-unit" aria-hidden="true">{field.unit}</span></div>
  </div>;
  return <form className="settings-section retention-settings" onSubmit={event => { event.preventDefault(); void action.run(async () => {
    await api.mutate('/api/admin/debug/retention', { Policy: { RetentionDays: days === '' ? null : Number(days), MaxRuns: runs === '' ? null : Number(runs), MaxBytes: size === '' ? null : Math.round(Number(size) * 1048576), MinRuns: keep === '' ? null : Number(keep) } }, policy.Version);
    onSaved(); reload(); invalidateTopics(['debug', 'admin']);
  }, undefined, false); }}>
    <p className="retention-settings-note">{zh ? '每个账号独立计算额度。留空使用输入框提示的默认值。' : 'Limits apply independently to each account. Leave fields empty to use the placeholder defaults.'}</p>
    <section className="settings-group"><h3 className="settings-group-title">{zh ? '自动清理' : 'Automatic cleanup'}</h3><div className="settings-list">{fields.slice(0, 3).map(row)}</div></section>
    <section className="settings-group"><h3 className="settings-group-title">{zh ? '最近记录保护' : 'Recent history protection'}</h3><div className="settings-list">{row(fields[3])}</div></section>
    <p className="retention-settings-note">{zh ? '保护记录优先保留。降低上限可能在后续写入时清理更早的记录，梦境记录单独保留。' : 'Protected history is retained first. Lower limits may remove older records on subsequent writes. Dream records are retained separately.'}</p>
    <ErrorNotice error={action.error} retry={() => { action.reset(); reload(); }} />
    <div><Button size="sm" type="submit" disabled={action.busy}>{zh ? '保存保留策略' : 'Save retention policy'}</Button></div>
  </form>;
}
