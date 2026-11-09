import { ChevronDown, RefreshCw } from 'lucide-react';
import { UsageAnalytics } from './UsageAnalytics';
import { useErrorToast } from '../error-toasts';
import { usePluginsVisible, pluginResourceVisible } from '../plugins/visibility';
import type { GPUDevice, SandboxView } from '@molr/sdk';
import { useState } from 'react';
import type { ReactNode } from 'react';
import { Badge, Button, Pagination, Progress, SearchField, Select, Table, Tabs } from '@molr/ui';
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
  const { locale } = useCopy(), zh = locale === 'zh';
  return <dl className="usage-overview">
    <Metric label={zh ? '累计用户消息' : 'Lifetime user messages'}>{value.UserMessages.toLocaleString(locale)}</Metric>
    <Metric label={zh ? '累计模型费用' : 'Lifetime model spend'}><Amounts values={value.Spent} /></Metric>
    <Metric label={zh ? '预留费用' : 'Reserved spend'}><Amounts values={value.Reserved} /></Metric>
    <Metric label={zh ? '模型调用' : 'Model calls'}>{value.ModelCalls.toLocaleString(locale)}</Metric>
  </dl>;
}
function ContextStorage({ global = false }: { global?: boolean }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  return <section className="usage-context" aria-label={global ? (zh ? '全局上下文存储' : 'Global context storage') : (zh ? '账号上下文存储' : 'Account context storage')}>
    <div className="usage-section-heading"><h3>{global ? (zh ? '全局上下文存储' : 'Global context storage') : (zh ? '账号上下文存储' : 'Account context storage')}</h3></div>
    <p className="usage-note">{global
      ? (zh ? '跨账号去重后的全局存储，包含等待回收的数据，不受成员筛选影响。' : 'Global storage after deduplication across accounts, including data awaiting collection. Member filters do not affect this total.')
      : (zh ? '统计本账号引用的数据，账号内去重。跨账号共享数据分别计入各账号，因此账号用量不能直接相加作为全局用量。' : 'Data referenced by your account, deduplicated within this account. Shared data counts toward each account, so account totals cannot be added to obtain global storage.')}</p>
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
type UsageTab = 'models' | 'tools' | 'storage';
function UsageTabs({ models, tools, storage }: { models: ReactNode; tools: ReactNode; storage: ReactNode }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const [tab, setTab] = useState<UsageTab>('models');
  return <Tabs className="usage-tabs" label={zh ? '用量分类' : 'Usage category'} value={tab} onValueChange={setTab} items={[
    { value: 'models', label: zh ? '模型' : 'Models', content: models },
    { value: 'tools', label: zh ? '工具' : 'Tools', content: tools },
    { value: 'storage', label: zh ? '存储空间' : 'Storage', content: storage },
  ]} />;
}
function CurrentToolbar({ storage = false, loading, reload }: { storage?: boolean; loading: boolean; reload: () => void }) {
  const { locale, t } = useCopy(), zh = locale === 'zh';
  return <div className="usage-toolbar"><p className="settings-description">{storage ? zh ? '查看当前数据量与存储构成。' : 'Explore current data size and storage composition.' : zh ? '查看各工具的累计调用与执行结果。' : 'Explore lifetime tool calls and outcomes.'}</p><Button size="sm" variant="secondary" disabled={loading} onClick={reload}><RefreshCw size={14} />{t.refresh}</Button></div>;
}
function AsOf({ value }: { value: string }) {
  const { locale } = useCopy();
  return <p className="usage-note">{locale === 'zh' ? '统计时间：' : 'As of: '}{new Date(value).toLocaleString(locale)}</p>;
}
function ToolDetails({ value }: { value: UsageSummary }) {
  const { locale, t } = useCopy(), zh = locale === 'zh';
  const pluginsVisible = usePluginsVisible();
  const [search, setSearch] = useState(''), [page, setPage] = useState(1);
  const labels = t.assistantInfo.toolLabels as Record<string, { name: string }>;
  const name = (id: string) => labels[id]?.name ?? id;
  const visible = value.Tools.filter(row => pluginResourceVisible('tool', pluginsVisible, row.Name));
  const rows = visible.filter(row => `${name(row.Name)} ${row.Name}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()));
  const totals = visible.reduce((sum, row) => ({ Calls: sum.Calls + row.Calls, Succeeded: sum.Succeeded + row.Succeeded, Failed: sum.Failed + row.Failed, Canceled: sum.Canceled + row.Canceled, Active: sum.Active + row.Active, Unknown: sum.Unknown + row.Unknown }), { Calls: 0, Succeeded: 0, Failed: 0, Canceled: 0, Active: 0, Unknown: 0 });
  const metrics = [ ['Calls', zh ? '累计工具调用' : 'Lifetime tool calls'], ['Succeeded', zh ? '成功' : 'Succeeded'], ['Failed', zh ? '失败' : 'Failed'], ['Canceled', zh ? '已取消' : 'Canceled'], ['Active', zh ? '执行中' : 'In progress'], ['Unknown', zh ? '结果未知' : 'Unknown outcome'] ] as const;
  const currentPage = Math.min(page, Math.max(1, Math.ceil(rows.length / 20)));
  return <section className="usage-details">
    <dl className="usage-overview">{metrics.map(([key, label]) => <Metric key={key} label={label}>{totals[key].toLocaleString(locale)}</Metric>)}</dl>
    <p className="usage-note">{zh ? '按已发起的调用计数，包含历史记录；尚未执行的准备项不计入。' : 'Counts started calls, including history; prepared operations are excluded.'}{!pluginsVisible && (zh ? ' 插件工具按显示偏好隐藏。' : 'Plugin tools follow your visibility preference.')}{pluginsVisible && (zh ? ` HTTP 连接调用 ${value.ConnectionCalls.toLocaleString(locale)} 次，已包含在工具调用中。` : ` Includes ${value.ConnectionCalls.toLocaleString(locale)} HTTP connection calls.`)}</p>
    <SearchField label={zh ? '搜索工具' : 'Search tools'} value={search} onValueChange={v => { setSearch(v); setPage(1); }} placeholder={zh ? '工具名称或标识' : 'Tool name or identifier'} />
    <Table caption={zh ? '工具调用明细' : 'Tool call breakdown'} rows={rows.slice((currentPage - 1) * 20, currentPage * 20)} rowKey={row => row.Name} columns={[
      { key: 'name', header: zh ? '工具' : 'Tool', cell: row => <div className="usage-tool-name"><strong>{name(row.Name)}</strong>{name(row.Name) !== row.Name && <small>{row.Name}</small>}</div> },
      ...metrics.map(([key, label]) => ({ key, header: key === 'Calls' ? zh ? '调用' : 'Calls' : label, align: 'end' as const, cell: (row: UsageSummary['Tools'][number]) => row[key].toLocaleString(locale) })),
    ]} emptyState={<Empty title={search ? zh ? '没有匹配的工具' : 'No matching tools' : zh ? '暂无工具调用' : 'No tool calls recorded'} />} />
    {rows.length > 20 && <Pagination page={currentPage} totalPages={Math.ceil(rows.length / 20)} onPageChange={setPage} />}
  </section>;
}
function StorageDetails({ context, attachments, global = false }: { context: ContextStorageUsage; attachments: UsageSummary['Attachments']; global?: boolean }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const fileBytes = attachments.reduce((n, row) => n + row.Bytes, 0), fileCount = attachments.reduce((n, row) => n + row.Files, 0);
  const total = context.Bytes + fileBytes;
  const names: Record<string, string> = { image: zh ? '图片附件' : 'Images', audio: zh ? '音频附件' : 'Audio', video: zh ? '视频附件' : 'Video', text: zh ? '文本附件' : 'Text files', other: zh ? '其他附件与文件' : 'Other files' };
  const rows = [
    { Kind: 'messages', Name: zh ? '消息正文' : 'Message content', Bytes: context.MessageBytes, Count: context.Messages },
    { Kind: 'values', Name: zh ? '工具定义与共享数据' : 'Tool definitions and shared data', Bytes: context.ValueBytes, Count: context.Values },
    { Kind: 'documents', Name: zh ? '上下文结构与详情' : 'Context structure and details', Bytes: context.DocumentBytes, Count: context.Documents },
    ...attachments.map(row => ({ ...row, Name: names[row.Kind] ?? row.Kind, Count: row.Files })),
  ];
  return <div className="usage-details">
    <dl className="usage-overview usage-storage-overview">
      <Metric label={zh ? '已统计数据量' : 'Measured data size'}>{bytes(total)}</Metric>
      <Metric label={zh ? '上下文数据' : 'Context data'}>{bytes(context.Bytes)}</Metric>
      <Metric label={zh ? '附件与文件' : 'Attachments and files'}>{bytes(fileBytes)}</Metric>
      <Metric label={zh ? '文件数量' : 'File count'}>{fileCount.toLocaleString(locale)}</Metric>
    </dl>
    <section className="usage-storage-shares" aria-label={zh ? '存储构成' : 'Storage breakdown'}>
      <div className="usage-section-heading"><h3>{zh ? '存储构成' : 'Storage breakdown'}</h3></div>
      <div className="usage-storage-bar" aria-hidden="true">
        {rows.filter(row => row.Bytes > 0).map(row => <span key={row.Kind} style={{ flexGrow: row.Bytes, background: `var(--color-storage-${row.Kind})` }} />)}
      </div>
      <dl className="usage-storage-legend">{rows.map(row => {
        const share = total ? row.Bytes / total * 100 : 0;
        return <div key={row.Kind}>
          <dt><span className="usage-storage-dot" style={{ background: `var(--color-storage-${row.Kind})` }} aria-hidden="true" />{row.Name}</dt>
          <dd>{share > 0 && share < 0.1 ? '<0.1' : share.toFixed(1)}% · {bytes(row.Bytes)} · {row.Count.toLocaleString(locale)} {zh ? '个对象' : row.Count === 1 ? 'object' : 'objects'}</dd>
        </div>;
      })}</dl>
    </section>
    <p className="usage-note">{zh ? '附件按已就绪的文件计量，按媒体类型归类。以上不含助理工作区、接入目录、数据库索引、日志和缓存，因此不代表磁盘总占用。工作区实时用量可在助理权限详情中查看。' : 'Attachments count ready files, grouped by media type. Assistant workspaces, connected directories, database indexes, logs and caches are excluded; this is not total disk usage. Live workspace usage is available in assistant permission details.'}</p>
    <ContextStorage global={global} />
  </div>;
}
export function AccountUsage() {
  const pluginsVisible = usePluginsVisible();
  const { locale, t } = useCopy(), zh = locale === 'zh';
  const query = useQuery<AccountQuotas>('/api/account/quotas');
  const sandboxes = useQuery<SandboxView[]>('/api/sandboxes');
  const models = useQuery<{ ID: string; Name: string }[]>('/api/models');
  const labels = t.assistantInfo.toolLabels as Record<string, { name: string }>;
  const data = query.data;
  useErrorToast(query.error, query.reload);
  const pending = query.loading ? <Loading /> : <Button variant="secondary" size="sm" onClick={query.reload}>{zh ? '重试' : 'Retry'}</Button>;
  const allowances = (tab: UsageTab) => {
    if (!data || data.Group.Administrator) return null;
    const resources = data.Group.Resources.filter(ref => ref.Kind !== 'plugin' && pluginResourceVisible(ref.Kind, pluginsVisible, ref.ID) && (tab === 'models' ? ref.Kind === 'model' : tab === 'storage' ? ref.Kind === 'sandbox' && Boolean(ref.Quota.Storage) : ref.Kind !== 'model' && (ref.Kind !== 'sandbox' || !ref.Quota.Storage || Boolean(ref.Quota.Compute || ref.Quota.Network || ref.Quota.Concurrent))));
    return <section className="usage-allowances"><div className="usage-section-heading"><div><h3>{zh ? '当前成员额度' : 'Current member allowances'}</h3><p>{groupName(data.Group, zh)} · {zh ? '每位成员独立计量，所有助理合计使用' : 'Measured per member, across all assistants'}</p></div></div>
      {resources.length ? <div className="usage-resources">{resources.map(ref => <ResourceAllowance key={`${ref.Kind}:${ref.ID}`} reference={ref.Kind === 'sandbox' ? { ...ref, Quota: tab === 'storage' ? { ...ref.Quota, Compute: undefined, Network: undefined, Concurrent: 0 } : { ...ref.Quota, Storage: undefined } } : ref} gpuAccessMode={sandboxes.data?.find(node => node.ID === ref.ID)?.Info.Capabilities.GPUAccessMode} gpus={sandboxes.data?.find(node => node.ID === ref.ID)?.Info.GPUs} used={data.Resources.find(v => v.Kind === ref.Kind && v.ID === ref.ID)} name={models.data?.find(m => ref.Kind === 'model' && m.ID === ref.ID)?.Name ?? (ref.Kind === 'tool' ? labels[ref.ID]?.name : ref.Kind === 'sandbox' && ref.ID === 'local' ? zh ? '本机沙盒' : 'Local sandbox' : undefined) ?? data.Resources.find(v => v.Kind === ref.Kind && v.ID === ref.ID)?.Name ?? ref.ID} />)}</div> : <Empty title={zh ? '暂无资源额度' : 'No resource allowances'} />}
    </section>;
  };
  return <div className="usage-page"><UsageTabs models={<div className="usage-tab-content">
    <UsageAnalytics onRefresh={query.reload} />
    <details className="usage-current" open><summary>{zh ? '累计用量与额度' : 'Lifetime usage and allowances'}<ChevronDown size={16} aria-hidden="true" /></summary><div className="usage-current-body">
      <p className="usage-note">{zh ? '以下为账号累计记录和当前额度，不受上方日期、助理或模型筛选影响。预留费用尚未确认为消费。' : 'Account lifetime totals and current allowances are independent of the filters above. Reserved spend is not confirmed spending.'}</p>
      {data ? <><Overview value={data.Summary} />{allowances('models')}<AsOf value={data.AsOf} /></> : pending}
    </div></details>
  </div>} tools={<div className="usage-tab-content" aria-busy={query.loading}>
    <CurrentToolbar loading={query.loading} reload={query.reload} />
    {data ? <><ToolDetails value={data.Summary} />{allowances('tools')}<AsOf value={data.AsOf} /></> : pending}
  </div>} storage={<div className="usage-tab-content" aria-busy={query.loading}>
    <CurrentToolbar storage loading={query.loading} reload={query.reload} />
    {data ? <><StorageDetails context={data.ContextStorage} attachments={data.Summary.Attachments} />{allowances('storage')}<AsOf value={data.AsOf} /></> : pending}
  </div>} /></div>;
}
function aggregate(rows: UsageSummary[]): UsageSummary {
  const result: UsageSummary = { Tools: [], Attachments: [], UserID: '', Username: '', DisplayName: '', GroupID: '', UserMessages: 0, ModelCalls: 0, ToolCalls: 0, ConnectionCalls: 0, AttachmentBytes: 0, Active: 0, Unknown: 0, Spent: [], Reserved: [] };
  for (const row of rows) {
    for (const key of ['UserMessages', 'ModelCalls', 'ToolCalls', 'ConnectionCalls', 'AttachmentBytes', 'Active', 'Unknown'] as const) result[key] += row[key];
    for (const tool of row.Tools) {
      const item = result.Tools.find(v => v.Name === tool.Name);
      if (item) for (const key of ['Calls', 'Succeeded', 'Failed', 'Canceled', 'Active', 'Unknown'] as const) item[key] += tool[key];
      else result.Tools.push({ ...tool });
    }
    for (const category of row.Attachments) {
      const item = result.Attachments.find(v => v.Kind === category.Kind);
      if (item) { item.Bytes += category.Bytes; item.Files += category.Files; } else result.Attachments.push({ ...category });
    }
    for (const key of ['Spent', 'Reserved'] as const) for (const amount of row[key]) { const item = result[key].find(v => v.Currency === amount.Currency); if (item) item.Micros += amount.Micros; else result[key].push({ ...amount }); }
  }
  result.Tools.sort((a, b) => b.Calls - a.Calls || a.Name.localeCompare(b.Name));
  return result;
}
export function AdminUsage() {
  const { locale } = useCopy(), zh = locale === 'zh';
  const query = useQuery<AdminUsageView>('/api/admin/usage');
  const [user, setUser] = useState(''), [toolUser, setToolUser] = useState('all');
  useErrorToast(query.error, query.reload);
  const data = query.data;
  const rows = data?.Users.filter(row => !user || row.UserID === user);
  const pending = query.loading ? <Loading /> : <Button variant="secondary" size="sm" onClick={query.reload}>{zh ? '重试' : 'Retry'}</Button>;
  return <div className="usage-page"><UsageTabs models={<div className="usage-tab-content">
    <UsageAnalytics admin onRefresh={query.reload} onUserChange={setUser} />
    <details className="usage-current" open><summary>{zh ? '累计用量与资源' : 'Lifetime usage and resources'}<ChevronDown size={16} aria-hidden="true" /></summary><div className="usage-current-body">
      <p className="usage-note">{zh ? '以下为累计记录，不随日期、用户组或模型筛选变化；仅跟随成员选择。预留费用尚未确认为消费。' : 'Lifetime records follow member selection, independent of date, group or model filters. Reserved spend is not confirmed spending.'}</p>
      {rows ? <section className="usage-summary"><div className="usage-section-heading"><h3>{user ? rows[0]?.DisplayName || rows[0]?.Username : zh ? '全部成员累计汇总' : 'Lifetime totals across all members'}</h3></div><Overview value={aggregate(rows)} /></section> : pending}
    </div></details>
  </div>} tools={<div className="usage-tab-content" aria-busy={query.loading}>
    <CurrentToolbar loading={query.loading} reload={query.reload} />
    {data ? <><Select size="sm" label={zh ? '成员' : 'Member'} value={toolUser} onValueChange={setToolUser} options={[{ value: 'all', label: zh ? '全部成员' : 'All members' }, ...data.Users.map(row => ({ value: row.UserID, label: row.DisplayName || row.Username }))]} /><ToolDetails value={aggregate(data.Users.filter(row => toolUser === 'all' || row.UserID === toolUser))} /><AsOf value={data.AsOf} /></> : pending}
  </div>} storage={<div className="usage-tab-content" aria-busy={query.loading}>
    <CurrentToolbar storage loading={query.loading} reload={query.reload} />
    {data ? <><StorageDetails global context={data.ContextStorage} attachments={aggregate(data.Users).Attachments} /><AsOf value={data.AsOf} /></> : pending}
  </div>} /></div>;
}
