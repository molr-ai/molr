import { ChevronDown } from 'lucide-react';
import { UsageAnalytics } from './UsageAnalytics';
import { useErrorToast } from '../error-toasts';
import { usePluginsVisible, pluginResourceVisible } from '../plugins/visibility';
import type { GPUDevice, SandboxView } from '@molr/sdk';
import { useState } from 'react';
import type { ReactNode } from 'react';
import { Badge, Button, Progress, Select } from '@molr/ui';
import type { Money } from '@molr/sdk';
import { useCopy, useQuery } from '../state';
import { Empty, Loading } from '../interfaces/official/components/common';
import { MoneyText } from '../interfaces/official/components/Money';
import { ConstraintsSummary } from '../interfaces/official/features/ResourceConstraints';
import type { AccountQuotas, AdminUsageView, ContextStorageUsage, ResourceReference, UsageSummary } from '@molr/sdk';
import { groupName } from './types';
import './usage.css';

function Amounts({ values }: { values: Money[] }) {
  return <span className="usage-money"><MoneyText originals={values} /></span>;
}
function bytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1048576) return `${(value / 1024).toFixed(1)} KiB`;
  if (value < 1073741824) return `${(value / 1048576).toFixed(1)} MiB`;
  return `${(value / 1073741824).toFixed(2)} GiB`;
}
function Metric({ label, children }: { label: string; children: ReactNode }) {
  return <div className="usage-metric"><dt>{label}</dt><dd>{children}</dd></div>;
}
function Overview({ value }: { value: UsageSummary }) {
  const pluginsVisible = usePluginsVisible();
  const { locale } = useCopy(), zh = locale === 'zh';
  return <dl className="usage-overview">
    <Metric label={zh ? '累计用户消息' : 'Lifetime user messages'}>{value.UserMessages.toLocaleString(locale)}</Metric>
    <Metric label={zh ? '累计模型费用' : 'Lifetime model spend'}><Amounts values={value.Spent} /></Metric>
    <Metric label={zh ? '预留费用' : 'Reserved spend'}><Amounts values={value.Reserved} /></Metric>
    <Metric label={zh ? '模型调用' : 'Model calls'}>{value.ModelCalls.toLocaleString(locale)}</Metric>
    <Metric label={zh ? '工具调用' : 'Tool calls'}>{value.ToolCalls.toLocaleString(locale)}</Metric>
    {pluginsVisible && <Metric label={zh ? 'HTTP 连接调用' : 'HTTP connection calls'}>{value.ConnectionCalls.toLocaleString(locale)}</Metric>}
    <Metric label={zh ? '当前附件存储' : 'Current attachment storage'}>{bytes(value.AttachmentBytes)}</Metric>
  </dl>;
}
function ContextStorage({ value, global = false }: { value: ContextStorageUsage; global?: boolean }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  return <section className="usage-context" aria-label={global ? (zh ? '全局上下文存储' : 'Global context storage') : (zh ? '账号上下文存储' : 'Account context storage')}>
    <div className="usage-section-heading"><h3>{global ? (zh ? '全局上下文存储' : 'Global context storage') : (zh ? '账号上下文存储' : 'Account context storage')}</h3></div>
    <dl className="usage-overview">
      <Metric label={zh ? '上下文数据量' : 'Context data size'}>{bytes(value.Bytes)}</Metric>
      <Metric label={zh ? '去重消息节点' : 'Unique message nodes'}>{value.Messages.toLocaleString(locale)}</Metric>
    </dl>
    <p className="usage-note">{global
      ? (zh ? '跨账号去重后的全局存储，包含等待回收的数据，不受成员筛选影响。' : 'Global storage after deduplication across accounts, including data awaiting collection. Member filters do not affect this total.')
      : (zh ? '统计本账号引用的数据，账号内去重。跨账号共享数据分别计入各账号，因此账号用量不能直接相加作为全局用量。' : 'Data referenced by your account, deduplicated within this account. Shared data counts toward each account, so account totals cannot be added to obtain global storage.')}
      {' '}{zh ? '包含消息、工具定义及调试详情的存储对象，不含数据库索引、引用元数据和空闲空间。' : 'Includes stored messages, tool definitions and debug details; excludes database indexes, reference metadata and free space.'}</p>
  </section>;
}
function Allowance({ label, used, limit }: { label: string; used: number; limit: number }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  return limit > 0
    ? <Progress label={label} value={used} max={limit} valueText={`${used.toLocaleString(locale)} / ${limit.toLocaleString(locale)}`} className={used >= limit ? 'usage-limit-reached' : undefined} />
    : <div className="usage-allowance"><span>{label}</span><strong>{used.toLocaleString(locale)} · {zh ? '不限' : 'Unlimited'}</strong></div>;
}
function ResourceAllowance({ reference, used, name, gpus, gpuAccessMode }: { gpuAccessMode?: string; gpus?: GPUDevice[]; reference: ResourceReference; used?: AccountQuotas['Resources'][number]; name: string }) {
  const { locale } = useCopy(), zh = locale === 'zh', q = reference.Quota;
  const kind = { model: zh ? '模型' : 'Model', tool: zh ? '工具' : 'Tool', connection: zh ? '连接' : 'Connection', sandbox: zh ? '沙盒' : 'Sandbox', plugin: zh ? '插件' : 'Plugin' }[reference.Kind];
  const period = ({ day: zh ? '每日' : 'Daily', week: zh ? '每周' : 'Weekly', month: zh ? '每月' : 'Monthly', lifetime: zh ? '累计' : 'Lifetime' } as Record<string, string>)[q.Period || 'lifetime'];
  const counted = reference.Kind !== 'sandbox';
  return <article className="usage-resource">
    <header><div><h4>{name}</h4>{counted && <p>{period} · {q.Timezone || 'UTC'}{used?.PeriodEnd && <> · {zh ? '下次重置 ' : 'Resets '}{new Date(used.PeriodEnd).toLocaleString(locale, { timeZone: q.Timezone || 'UTC', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</>}</p>}</div><Badge>{kind}</Badge></header>
    {counted && used && <div className="usage-resource-body">
      <Allowance label={zh ? '已占用调用额度' : 'Calls counted toward allowance'} used={used.Calls} limit={q.Calls} />
      <Allowance label={zh ? '并发占用' : 'Concurrency held'} used={used.Active} limit={q.Concurrent} />
      {reference.Kind === 'model' && <dl className="usage-costs"><Metric label={zh ? '已消费' : 'Spent'}><Amounts values={used.Spent} /></Metric><Metric label={zh ? '预留' : 'Reserved'}><Amounts values={used.Reserved} /></Metric><Metric label={zh ? '费用上限' : 'Spend limit'}>{q.Budget ? <MoneyText value={q.Budget} /> : zh ? '不限' : 'Unlimited'}</Metric></dl>}
      {(used.Carried > 0 || used.Unknown > 0) && <p className="usage-held-note">{zh ? `${used.Carried} 次调用仍在执行并跨周期占用额度；${used.Unknown} 次历史调用结果未知，已结束费用预留和并发占用。` : `${used.Carried} calls are still running from earlier periods; ${used.Unknown} past outcomes are unknown and no longer hold reserved spend or concurrency.`}</p>}
    </div>}
    {reference.Kind === 'sandbox' && <div className="usage-resource-body"><p className="muted">{zh ? '以下为成员限额，所有助理合计使用；实时资源用量可在助理权限详情中查看。' : 'Member limits apply across all assistants. See assistant permission details for live resource usage.'}</p>{q.Concurrent > 0 && <p>{zh ? '同时执行上限' : 'Concurrent executions'}: {q.Concurrent}</p>}{q.Compute && <ConstraintsSummary gpuAccessMode={gpuAccessMode} gpus={gpus} value={{ compute: q.Compute }} />}{q.Network && <ConstraintsSummary value={{ network: q.Network }} />}{q.Storage && <ConstraintsSummary value={{ storage: q.Storage }} />}{!q.Compute && !q.Network && !q.Storage && !q.Concurrent && <p>{zh ? '未设置成员资源限额' : 'No member resource limits configured'}</p>}</div>}
  </article>;
}
export function AccountUsage() {
  const pluginsVisible = usePluginsVisible();
  const { locale, t } = useCopy(), zh = locale === 'zh';
  const query = useQuery<AccountQuotas>('/api/account/quotas');
  const sandboxes = useQuery<SandboxView[]>('/api/sandboxes');
  const labels = t.assistantInfo.toolLabels as Record<string, { name: string }>;
  const models = useQuery<{ ID: string; Name: string }[]>('/api/models');
  const [filter, setFilter] = useState('all');
  const data = query.data, resources = data?.Group.Resources.filter(r => r.Kind !== 'plugin' && pluginResourceVisible(r.Kind, pluginsVisible, r.ID) && (filter === 'all' || r.Kind === filter)) ?? [];
  useErrorToast(query.error, query.reload);
  return <div className="usage-page" aria-busy={query.loading}>
    <UsageAnalytics onRefresh={query.reload} />
    <details className="usage-current" open><summary>{zh ? '累计用量与额度' : 'Lifetime usage and allowances'}<ChevronDown size={16} aria-hidden="true" /></summary><div className="usage-current-body">
    <p className="usage-note">{zh ? '用户消息数、费用与调用次数为累计值，不受上方日期、助理或模型筛选影响；存储与额度为当前状态。' : 'User messages, spend and calls are lifetime totals, independent of the date, assistant and model filters above. Storage and allowances reflect the current state.'}</p>
    {!data ? query.loading && <Loading /> : <>
      <section className="usage-summary">
        <Overview value={data.Summary} />
        <p className="usage-note">{zh ? '预留费用尚未确认为实际消费。' : 'Reserved spend is not confirmed spending.'}</p>
        {pluginsVisible && <p className="usage-note">{zh ? 'HTTP 连接调用也计入工具次数，两项不可相加。' : 'HTTP connection calls also count as tool calls; do not add the two.'}</p>}
      </section>
      <ContextStorage value={data.ContextStorage} />
      {!data.Group.Administrator && <section className="usage-allowances"><div className="usage-section-heading"><div><h3>{zh ? '当前成员额度' : 'Current member allowances'}</h3><p>{groupName(data.Group, zh)} · {zh ? '每位成员独立计量，所有助理合计使用' : 'Measured per member, across all assistants'}</p></div><Select size="sm" label={zh ? '资源类型' : 'Resource type'} value={filter} onValueChange={setFilter} options={[{ value: 'all', label: zh ? '全部资源' : 'All resources' }, { value: 'model', label: zh ? '模型' : 'Models' }, { value: 'tool', label: zh ? '工具' : 'Tools' }, { value: 'connection', label: zh ? '连接' : 'Connections' }, { value: 'sandbox', label: zh ? '沙盒' : 'Sandboxes' }]} /></div>
        {resources.length ? <div className="usage-resources">{resources.map(ref => <ResourceAllowance key={`${ref.Kind}:${ref.ID}`} reference={ref} gpuAccessMode={sandboxes.data?.find(node => ref.Kind === 'sandbox' && node.ID === ref.ID)?.Info.Capabilities.GPUAccessMode} gpus={sandboxes.data?.find(node => ref.Kind === 'sandbox' && node.ID === ref.ID)?.Info.GPUs} used={data.Resources.find(v => v.Kind === ref.Kind && v.ID === ref.ID)} name={models.data?.find(m => ref.Kind === 'model' && m.ID === ref.ID)?.Name ?? (ref.Kind === 'tool' ? labels[ref.ID]?.name : ref.Kind === 'sandbox' && ref.ID === 'local' ? zh ? '本机沙盒' : 'Local sandbox' : undefined) ?? data.Resources.find(v => v.Kind === ref.Kind && v.ID === ref.ID)?.Name ?? ref.ID} />)}</div> : <Empty title={zh ? '暂无资源额度' : 'No resource allowances'} description={zh ? '当前用户组或筛选条件下没有可展示的资源。' : 'No resources match your group or filter.'} />}
      </section>}
      <p className="usage-note">{zh ? '统计时间：' : 'As of: '}{new Date(data.AsOf).toLocaleString(locale)}</p>
    </>}
    </div></details>
  </div>;
}
function aggregate(rows: UsageSummary[]): UsageSummary {
  const result: UsageSummary = { UserID: '', Username: '', DisplayName: '', GroupID: '', UserMessages: 0, ModelCalls: 0, ToolCalls: 0, ConnectionCalls: 0, AttachmentBytes: 0, Active: 0, Unknown: 0, Spent: [], Reserved: [] };
  for (const row of rows) {
    for (const key of ['UserMessages', 'ModelCalls', 'ToolCalls', 'ConnectionCalls', 'AttachmentBytes', 'Active', 'Unknown'] as const) result[key] += row[key];
    for (const key of ['Spent', 'Reserved'] as const) for (const amount of row[key]) { const item = result[key].find(v => v.Currency === amount.Currency); if (item) item.Micros += amount.Micros; else result[key].push({ ...amount }); }
  }
  return result;
}
export function AdminUsage() {
  const { locale } = useCopy(), zh = locale === 'zh';
  const query = useQuery<AdminUsageView>('/api/admin/usage');
  const [user, setUser] = useState('');
  useErrorToast(query.error, query.reload);
  const rows = query.data?.Users.filter(row => !user || row.UserID === user);
  return <div className="usage-page">
    <UsageAnalytics admin onRefresh={query.reload} onUserChange={setUser} />
    <details className="usage-current" open><summary>{zh ? '累计用量与资源' : 'Lifetime usage and resources'}<ChevronDown size={16} aria-hidden="true" /></summary><div className="usage-current-body">
      <p className="usage-note">{zh ? '以下为当前占用与累计记录，不随上方日期、用户组或模型筛选变化。成员选择仅影响账号汇总，全局上下文始终独立去重。' : 'Current holdings and lifetime records are independent of date, group and model filters above. Member selection affects account totals; global context remains independently deduplicated.'}</p>
      {!query.data ? query.loading ? <Loading /> : <Button variant="secondary" size="sm" onClick={query.reload}>{zh ? '重试' : 'Retry'}</Button> : <>
        <ContextStorage value={query.data.ContextStorage} global />
        {rows && <section className="usage-summary"><div className="usage-section-heading"><h3>{user ? rows[0]?.DisplayName || rows[0]?.Username : zh ? '全部成员累计汇总' : 'Lifetime totals across all members'}</h3></div>
          <Overview value={aggregate(rows)} />
          <p className="usage-note">{zh ? 'HTTP 连接调用已包含在工具调用中。预留费用尚未确认为消费。' : 'HTTP connection calls are included in tool calls. Reserved spend is not confirmed spending.'}</p>
        </section>}
      </>}
    </div></details>
  </div>;
}
