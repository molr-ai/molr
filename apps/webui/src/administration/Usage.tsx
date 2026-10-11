import { usePluginsVisible, pluginResourceVisible } from '../plugins/visibility';
import type { GPUDevice, SandboxView } from '../sandbox-types';
import { useState } from 'react';
import type { ReactNode } from 'react';
import { Badge, Button, Select, TextField } from '@molr/ui';
import { RefreshCw } from 'lucide-react';
import type { Money } from '@molr/sdk';
import { useCopy, useQuery } from '../state';
import { Empty, ErrorNotice, Loading } from '../interfaces/official/components/common';
import { MoneyText } from '../interfaces/official/components/Money';
import { ConstraintsSummary } from '../interfaces/official/features/ResourceConstraints';
import type { AccountQuotas, AdminUsageView, ContextStorageUsage, ResourceReference, UsageSummary, UserGroup } from './types';
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
      ? (zh ? '跨账号去重后的全局存储，包含等待回收的数据，不受下方成员筛选影响。' : 'Global storage after deduplication across accounts, including data awaiting collection. Member filters below do not affect this total.')
      : (zh ? '统计本账号引用的数据，账号内去重。跨账号共享数据分别计入各账号，因此账号用量不能直接相加作为全局用量。' : 'Data referenced by your account, deduplicated within this account. Shared data counts toward each account, so account totals cannot be added to obtain global storage.')}
      {' '}{zh ? '包含消息、工具定义及调试详情的存储对象，不含数据库索引、引用元数据和空闲空间。' : 'Includes stored messages, tool definitions and debug details; excludes database indexes, reference metadata and free space.'}</p>
  </section>;
}
function Allowance({ label, used, limit }: { label: string; used: number; limit: number }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  return <div className="usage-allowance"><div><span>{label}</span><strong>{used.toLocaleString(locale)}{limit > 0 ? ` / ${limit.toLocaleString(locale)}` : ` · ${zh ? '不限' : 'Unlimited'}`}</strong></div>
    {limit > 0 && <progress aria-label={label} value={Math.min(used, limit)} max={limit} className={used >= limit ? 'usage-limit-reached' : undefined} />}
  </div>;
}
function ResourceAllowance({ reference, used, name, gpus }: { gpus?: GPUDevice[]; reference: ResourceReference; used?: AccountQuotas['Resources'][number]; name: string }) {
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
    {reference.Kind === 'sandbox' && <div className="usage-resource-body"><p className="muted">{zh ? '以下为成员限额，所有助理合计使用；实时资源用量可在助理权限详情中查看。' : 'Member limits apply across all assistants. See assistant permission details for live resource usage.'}</p>{q.Concurrent > 0 && <p>{zh ? '同时执行上限' : 'Concurrent executions'}: {q.Concurrent}</p>}{q.Compute && <ConstraintsSummary gpus={gpus} value={{ compute: q.Compute }} />}{q.Network && <ConstraintsSummary value={{ network: q.Network }} />}{q.Storage && <ConstraintsSummary value={{ storage: q.Storage }} />}{!q.Compute && !q.Network && !q.Storage && !q.Concurrent && <p>{zh ? '未设置成员资源限额' : 'No member resource limits configured'}</p>}</div>}
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
  const data = query.data, resources = data?.Group.Resources.filter(r => r.Kind !== 'plugin' && pluginResourceVisible(r.Kind, pluginsVisible, r.ID) && (filter === 'all' || (!pluginsVisible && filter === 'connection') || r.Kind === filter)) ?? [];
  return <div className="usage-page" aria-busy={query.loading}>
    <div className="usage-toolbar"><p>{data?.Group.Administrator ? zh ? '查看自己的累计用量。' : 'Your lifetime usage.' : zh ? '查看自己的累计用量，以及当前周期可用的成员额度。' : 'Your lifetime usage and current member allowances.'}</p><Button size="sm" variant="secondary" disabled={query.loading} onClick={query.reload}><RefreshCw size={14} />{t.refresh}</Button></div>
    <ErrorNotice error={query.error} retry={query.reload} />
    {!data ? query.loading && <Loading /> : <>
      <Overview value={data.Summary} />
      <p className="usage-note">{zh ? '费用与调用次数为累计值，附件为当前占用。预留费用尚未确认为实际消费。' : 'Spend and calls are lifetime totals; storage is current usage. Reserved spend is not confirmed spending.'}</p>
      {pluginsVisible && <p className="usage-note">{zh ? 'HTTP 连接调用也计入工具次数，两项不可相加。' : 'HTTP connection calls also count as tool calls; do not add the two.'}</p>}
      <ContextStorage value={data.ContextStorage} />
      {!data.Group.Administrator && <section className="usage-allowances"><div className="usage-section-heading"><div><h3>{zh ? '当前成员额度' : 'Current member allowances'}</h3><p>{groupName(data.Group, zh)} · {zh ? '每位成员独立计量，所有助理合计使用' : 'Measured per member, across all assistants'}</p></div><Select label={zh ? '资源类型' : 'Resource type'} value={!pluginsVisible && filter === 'connection' ? 'all' : filter} onValueChange={setFilter} options={[{ value: 'all', label: zh ? '全部资源' : 'All resources' }, { value: 'model', label: zh ? '模型' : 'Models' }, { value: 'tool', label: zh ? '工具' : 'Tools' }, ...(pluginsVisible ? [{ value: 'connection', label: zh ? '连接' : 'Connections' }] : []), { value: 'sandbox', label: zh ? '沙盒' : 'Sandboxes' }]} /></div>
        {resources.length ? <div className="usage-resources">{resources.map(ref => <ResourceAllowance key={`${ref.Kind}:${ref.ID}`} reference={ref} gpus={sandboxes.data?.find(node => ref.Kind === 'sandbox' && node.ID === ref.ID)?.Info.GPUs} used={data.Resources.find(v => v.Kind === ref.Kind && v.ID === ref.ID)} name={models.data?.find(m => ref.Kind === 'model' && m.ID === ref.ID)?.Name ?? (ref.Kind === 'tool' ? labels[ref.ID]?.name : ref.Kind === 'sandbox' && ref.ID === 'local' ? zh ? '本机沙盒' : 'Local sandbox' : undefined) ?? data.Resources.find(v => v.Kind === ref.Kind && v.ID === ref.ID)?.Name ?? ref.ID} />)}</div> : <Empty title={zh ? '暂无资源额度' : 'No resource allowances'} description={zh ? '当前用户组或筛选条件下没有可展示的资源。' : 'No resources match your group or filter.'} />}
      </section>}
      <p className="usage-note">{zh ? '统计时间：' : 'As of: '}{new Date(data.AsOf).toLocaleString(locale)}</p>
    </>}
  </div>;
}
function aggregate(rows: UsageSummary[]): UsageSummary {
  const result: UsageSummary = { UserID: '', Username: '', DisplayName: '', GroupID: '', ModelCalls: 0, ToolCalls: 0, ConnectionCalls: 0, AttachmentBytes: 0, Active: 0, Unknown: 0, Spent: [], Reserved: [] };
  for (const row of rows) {
    for (const key of ['ModelCalls', 'ToolCalls', 'ConnectionCalls', 'AttachmentBytes', 'Active', 'Unknown'] as const) result[key] += row[key];
    for (const key of ['Spent', 'Reserved'] as const) for (const amount of row[key]) { const item = result[key].find(v => v.Currency === amount.Currency); if (item) item.Micros += amount.Micros; else result[key].push({ ...amount }); }
  }
  return result;
}
export function AdminUsage() {
  const pluginsVisible = usePluginsVisible();
  const { locale, t } = useCopy(), zh = locale === 'zh';
  const query = useQuery<AdminUsageView>('/api/admin/usage'), groups = useQuery<UserGroup[]>('/api/admin/groups');
  const [search, setSearch] = useState(''), [group, setGroup] = useState('all');
  const needle = search.trim().toLocaleLowerCase();
  const rows = query.data?.Users.filter(u => (group === 'all' || group === u.GroupID) && `${u.DisplayName} ${u.Username}`.toLocaleLowerCase().includes(needle));
  return <div className="usage-page" aria-busy={query.loading}>
    <div className="usage-toolbar"><p>{zh ? '按成员汇总累计用量，不包含聊天、附件正文或执行输出。' : 'Lifetime usage by member. No chats, attachment contents, or execution output.'}</p><Button size="sm" variant="secondary" disabled={query.loading} onClick={query.reload}><RefreshCw size={14} />{t.refresh}</Button></div>
    <ErrorNotice error={query.error || groups.error} retry={query.reload} />
    {query.data && <ContextStorage value={query.data.ContextStorage} global />}
    <div className="usage-filters"><TextField label={zh ? '搜索成员' : 'Search members'} value={search} onChange={e => setSearch(e.target.value)} placeholder={zh ? '用户名或显示名称' : 'Username or display name'} /><Select label={zh ? '用户组' : 'User group'} value={group} onValueChange={setGroup} options={[{ value: 'all', label: zh ? '全部用户组' : 'All groups' }, ...(groups.data ?? []).map(g => ({ value: g.ID, label: groupName(g, zh) }))]} /></div>
    {!rows ? query.loading && <Loading /> : <>
      <div className="usage-section-heading"><h3>{zh ? '筛选结果汇总' : 'Filtered totals'}</h3><Badge>{rows.length} {zh ? '位成员' : 'members'}</Badge></div>
      <Overview value={aggregate(rows)} />
      {pluginsVisible && <p className="usage-note">{zh ? 'HTTP 连接调用也计入工具次数，两项不可相加。' : 'HTTP connection calls also count as tool calls; do not add the two.'}</p>}
      <p className="usage-note">{zh ? '调用与费用为累计值，附件为当前存储。只有执行中的调用保留费用预留和并发占用。' : 'Calls and spend are lifetime totals; storage is current. Only running calls retain reserved spend and concurrency.'}</p>
      {!rows.length ? <Empty title={zh ? '没有匹配的成员' : 'No matching members'} /> : <div className="usage-members">{rows.map(u => <details key={u.UserID} className="usage-member"><summary><div className="usage-member-identity"><strong>{u.DisplayName || u.Username}</strong><span>@{u.Username} · {groupName(groups.data?.find(g => g.ID === u.GroupID) ?? { ID: u.GroupID, Name: u.GroupID }, zh)}</span></div><div className="usage-member-spend"><span>{zh ? '已消费' : 'Spent'}</span><Amounts values={u.Spent} />{u.Unknown > 0 && <Badge>{u.Unknown} {zh ? '历史结果未知' : 'past outcomes unknown'}</Badge>}</div></summary><div className="usage-member-details"><Overview value={u} /><p className="usage-note">{zh ? `模型与工具调用占用：${u.Active}；另有 ${u.Unknown} 次历史调用结果未知，已结束费用预留和并发占用。` : `Model and tool calls holding allowance: ${u.Active}; ${u.Unknown} past outcomes are unknown and no longer hold reserved spend or concurrency.`}</p></div></details>)}</div>}
    </>}
  </div>;
}
