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
import type { AccountQuotas, AdminUsageView, StorageUsage, ResourceReference, UsageSummary } from '@molr/sdk';
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
    { value: 'storage', label: zh ? '存储空间' : 'Storage', content: tab === 'storage' ? storage : null },
  ]} />;
}
function CurrentToolbar({ loading, reload }: { loading: boolean; reload: () => void }) {
  const { locale, t } = useCopy(), zh = locale === 'zh';
  return <div className="usage-toolbar"><p className="settings-description">{zh ? '查看各工具的累计调用与执行结果。' : 'Explore lifetime tool calls and outcomes.'}</p><Button size="sm" variant="secondary" disabled={loading} onClick={reload}><RefreshCw size={14} />{t.refresh}</Button></div>;
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
function StorageDetails({ admin = false, children }: { admin?: boolean; children?: ReactNode }) {
  const { locale, t } = useCopy(), zh = locale === 'zh';
  // A workspace scan runs only when this tab is opened or explicitly refreshed.
  const query = useQuery<StorageUsage>(admin ? '/api/admin/storage' : '/api/account/storage', false, 0);
  useErrorToast(query.error, query.reload);
  const data = query.data;
  const rows = data ? [
    { key: 'database', name: zh ? '数据库' : 'Database', value: data.Database, description: data.DatabasePhysical ? zh ? '全部账号的数据文件，包含索引与数据库预留空间。' : 'Data files across all accounts, including indexes and allocated database space.' : zh ? '聊天记录、账号资料和设置等账号数据，不分摊共享索引。' : 'Your chat records, account data and settings; shared indexes are excluded.', files: false },
    { key: 'attachments', name: zh ? '消息附件' : 'Message attachments', value: data.Attachments, description: zh ? '上传的图片、文件与助理交付物。' : 'Uploaded images, files and assistant deliverables.', files: true },
    { key: 'workspaces', name: zh ? '助理工作区' : 'Assistant workspaces', value: data.Workspaces, description: zh ? '本机托管工作区中的文件、技能与缓存。' : 'Files, skills and caches in locally managed workspaces.', files: true },
    { key: 'shared', name: zh ? '共享资料' : 'Shared data', value: data.Shared, description: zh ? '账号内共享的记忆、技能与指令。' : 'Memories, skills and instructions shared within each account.', files: true },
    ...(admin && data.System ? [{ key: 'system', name: zh ? '系统与运行文件' : 'System and runtime files', value: data.System, description: zh ? '可识别的服务日志、缓存、组件、程序配置及临时残留文件。' : 'Identified service logs, caches, components, program/configuration files and temporary leftovers.', files: true }] : []),
  ] : [];
  const total = rows.reduce((sum, row) => sum + (row.value.Available ? row.value.Bytes : 0), 0);
  const complete = rows.every(row => row.value.Available);
  return <div className="usage-tab-content usage-storage" aria-busy={query.loading}>
    <div className="usage-storage-heading">
      <dl className="usage-storage-total"><Metric label={zh ? '已统计用量' : 'Measured storage'}>{rows.some(row => row.value.Available) ? bytes(total) : '—'}</Metric></dl>
      <Button size="sm" variant="secondary" disabled={query.loading} onClick={query.reload}><RefreshCw size={14} />{t.refresh}</Button>
    </div>
    {!data ? query.loading ? <Loading /> : <Empty title={zh ? '存储用量暂时无法加载' : 'Storage usage is temporarily unavailable'} action={<Button variant="secondary" size="sm" onClick={query.reload}>{zh ? '重试' : 'Retry'}</Button>} /> : <>
      <section className="usage-storage-shares" aria-label={zh ? '存储构成' : 'Storage breakdown'}>
        <div className="usage-storage-bar" aria-hidden="true">
          {rows.filter(row => row.value.Available && row.value.Bytes > 0).map(row => <span key={row.key} style={{ flexGrow: row.value.Bytes, background: `var(--color-storage-${row.key})` }} />)}
        </div>
        <dl className="usage-storage-categories">{rows.map(row => {
          const share = total ? row.value.Bytes / total * 100 : 0;
          return <div key={row.key} data-kind={row.key}>
            <dt><span className="usage-storage-dot" style={{ background: `var(--color-storage-${row.key})` }} aria-hidden="true" /><span>{row.name}</span><small>{!row.value.Available ? '—' : `${share > 0 && share < 0.1 ? '<0.1' : share.toFixed(1)}%`}</small></dt>
            <dd><strong>{row.value.Available ? bytes(row.value.Bytes) : zh ? '暂无法统计' : 'Unavailable'}</strong>{row.files && row.value.Available && <small>{row.value.Files.toLocaleString(locale)} {zh ? '个文件' : row.value.Files === 1 ? 'file' : 'files'}</small>}</dd>
            <p>{row.description}</p>
          </div>;
        })}</dl>
      </section>
      {!complete && <p className="usage-note" role="status">{zh ? '部分用量暂无法统计，总量与占比仅包含已统计部分，可刷新重试。' : 'Some usage is unavailable. Totals and shares cover measured categories only; refresh to retry.'}</p>}
      <p className="usage-note">{admin ? zh ? '按文件大小统计本机可识别的实例数据、安装和组件文件；外部接入目录、远程节点、外置备份与系统日志服务托管的日志不计入。' : 'Measures file sizes in identified local instance, installation and component locations; excludes connected directories, remote nodes, external backups and logs held by the system journal.' : zh ? '数据库按账号数据量统计，其余按文件大小统计；不含系统文件、外部接入目录和远程节点。' : 'Database usage measures account data; other categories use file sizes. System files, connected directories and remote nodes are excluded.'}</p>
      {children}
      <AsOf value={data.AsOf} />
    </>}
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
  </div>} storage={<StorageDetails>{allowances('storage')}</StorageDetails>} /></div>;
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
  </div>} storage={<StorageDetails admin />} /></div>;
}
