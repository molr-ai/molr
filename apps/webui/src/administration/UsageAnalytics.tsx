import { useEffect, useState } from 'react';
import { Badge, Button, Pagination, Progress, SearchField, SegmentedControl, Select, Table, TextField, Tooltip } from '@molr/ui';
import { RefreshCw, X } from 'lucide-react';
import { useCopy, useQuery } from '../state';
import { useErrorToast } from '../error-toasts';
import { useCurrency } from '../interfaces/official/components/Money';
import { Empty, Loading } from '../interfaces/official/components/common';
import { UsageCalendar } from './UsageCalendar';
import { amountLabel, amountMicros, calendarDate, compactTokens, dateAdd } from './usage-analytics';
import type { UsageAnalyticsView, UsageOption, UsageRow } from './usage-analytics';
import type { TableColumn } from '@molr/ui';
import { groupName } from './types';
import './usage-analytics.css';

export function UsageAnalytics({ admin = false, onRefresh, onUserChange }: { admin?: boolean; onRefresh?: () => void; onUserChange?: (id: string) => void }) {
 const { locale, t } = useCopy(), zh = locale === 'zh';
 const currency = useCurrency();
 const timezone = currency.data?.Timezone || 'UTC';
 const today = calendarDate(new Date(), timezone), year = today.slice(0, 4);
 const [period, setPeriod] = useState('year'), [customFrom, setCustomFrom] = useState(`${year}-01-01`), [customTo, setCustomTo] = useState(today);
 const [assistant, setAssistant] = useState('all'), [model, setModel] = useState('all'), [user, setUser] = useState('all'), [group, setGroup] = useState('all');
 const [search, setSearch] = useState(''), [searchQuery, setSearchQuery] = useState('');
 const [groupBy, setGroupBy] = useState(admin ? 'user' : 'assistant'), [sort, setSort] = useState('tokens'), [page, setPage] = useState(1), [day, setDay] = useState('');
 const [options, setOptions] = useState<UsageAnalyticsView['Options']>({ Assistants: [], Models: [], Users: [], Groups: [] });
 useEffect(() => { const timer = setTimeout(() => { setSearchQuery(search); setPage(1); setDay(''); }, 250); return () => clearTimeout(timer); }, [search]);
 let from = `${year}-01-01`, until = `${Number(year) + 1}-01-01`;
 if (period === 'month') { from = `${today.slice(0, 7)}-01`; until = dateAdd(today, 1); }
 if (period === '30' || period === '90') { from = dateAdd(today, 1 - Number(period)); until = dateAdd(today, 1); }
 if (period.startsWith('year:')) { const selected = period.slice(5); from = `${selected}-01-01`; until = `${Number(selected) + 1}-01-01`; }
 if (period === 'custom') { from = customFrom; until = /^\d{4}-\d{2}-\d{2}$/.test(customTo) ? dateAdd(customTo, 1) : ''; }
 const valid = Boolean(/^\d{4}-\d{2}-\d{2}$/.test(from) && until > from && new Date(until).getTime() - new Date(from).getTime() <= 366 * 86400000);
 const params = new URLSearchParams({ from, until, timezone, group_by: groupBy, sort, page: String(page) });
 for (const [key, value] of Object.entries({ assistant_id: assistant, model_id: model, user_id: user, group_id: group })) if (value !== 'all') params.set(key, value);
 if (searchQuery) params.set('search', searchQuery);
 if (day && day >= from && day < until) params.set('day', day);
 const query = useQuery<UsageAnalyticsView>(currency.data && valid ? `${admin ? '/api/admin/usage/analytics' : '/api/account/usage'}?${params}` : null);
 const [previous, setPrevious] = useState<UsageAnalyticsView>();
 useEffect(() => { if (query.data) setPrevious(query.data); }, [query.data]);
 const data = query.data ?? previous;
 useEffect(() => { if (data) setOptions(data.Options); }, [data]);
 const reset = () => { setPage(1); setDay(''); };
 const chooseUser = (id: string) => { setUser(id); onUserChange?.(id === 'all' ? '' : id); reset(); };
 const refresh = () => { query.reload(); currency.reload(); onRefresh?.(); };
 useErrorToast(query.error || currency.error, refresh);
 const label = (v: UsageOption, kind?: string) => `${kind === 'group' ? groupName(v, zh) : kind === 'model' && !v.ID ? (zh ? '未知模型' : 'Unknown model') : v.Name || v.ID}${v.State === 'dismissed' ? zh ? ' · 已解雇' : ' · Dismissed' : v.State === 'deleted' ? zh ? ' · 已删除' : ' · Deleted' : ''}`;
 const opts = (items: UsageOption[], all: string, kind?: string) => [{ value: 'all', label: all }, ...items.filter(v => v.ID).map(v => ({ value: v.ID, label: label(v, kind) }))];
 const selectedDay = data?.Daily.find(d => d.Date === day);
 const tableTotals = selectedDay ?? data?.Summary;
 const spend = (values: UsageAnalyticsView['Summary']['Spent']) => data ? amountLabel(values, data, locale) : '—';
 const shareValue = (row: UsageAnalyticsView['Summary']) => { if (sort === 'calls') return row.ModelCalls; if (sort === 'spend' && data) { try { return amountMicros(row.Spent, data); } catch { return 0; } } return row.TotalTokens; };
 const columns: TableColumn<UsageRow>[] = [
  { key: 'name', header: ({ assistant: zh ? '助理' : 'Assistant', model: zh ? '模型' : 'Model', user: zh ? '成员' : 'Member', group: zh ? '用户组' : 'User group' } as Record<string, string>)[groupBy], cell: row => <div className="usage-row-name"><Button variant="ghost" size="sm" disabled={groupBy === 'model' && !row.ID} onClick={() => { if (groupBy === 'assistant') { setAssistant(row.ID); setGroupBy('model'); } else if (groupBy === 'user') { chooseUser(row.ID); setGroupBy('model'); } else if (groupBy === 'group') { setGroup(row.ID); chooseUser('all'); setGroupBy('user'); } else if (row.ID) setModel(row.ID); reset(); }}>{label(row, groupBy)}</Button>{row.Secondary && <small>@{row.Secondary}</small>}{row.MissingTokenCalls + row.UnknownCostCalls > 0 && <Tooltip content={zh ? `${row.MissingTokenCalls} 次 Token 未知，${row.UnknownCostCalls} 次费用未知` : `${row.MissingTokenCalls} calls without token usage; ${row.UnknownCostCalls} with unknown cost`}><span tabIndex={0}><Badge size="sm" dot={false}>{zh ? '含未知用量' : 'Incomplete'}</Badge></span></Tooltip>}</div> },
  { key: 'input', header: zh ? '输入' : 'Input', align: 'end', cell: row => <><span title={row.InputTokens.toLocaleString(locale)}>{compactTokens(row.InputTokens, locale)}</span><small>{zh ? '缓存' : 'Cached'} {compactTokens(row.CachedTokens, locale)}</small></> },
  { key: 'output', header: zh ? '输出' : 'Output', align: 'end', cell: row => <span title={row.OutputTokens.toLocaleString(locale)}>{compactTokens(row.OutputTokens, locale)}</span> },
  { key: 'tokens', header: zh ? '总 Token' : 'Total tokens', align: 'end', cell: row => <span title={row.TotalTokens.toLocaleString(locale)}>{compactTokens(row.TotalTokens, locale)}</span> },
  { key: 'spend', header: zh ? '费用' : 'Spend', align: 'end', cell: row => spend(row.Spent) },
  { key: 'calls', header: zh ? '调用' : 'Calls', align: 'end', cell: row => row.ModelCalls.toLocaleString(locale) },
  { key: 'share', header: zh ? '占比' : 'Share', align: 'end', cell: row => {
   const total = tableTotals ? shareValue(tableTotals) : 0, share = total > 0 ? shareValue(row) / total * 100 : 0;
   return <div className="usage-share"><Progress label={`${label(row, groupBy)} ${zh ? '占比' : 'share'}`} value={share} showValue={false} /><span>{share.toFixed(1)}%</span></div>;
  } },
 ];
 return <div className="usage-analytics" aria-busy={query.loading}>
  <div className="usage-toolbar"><p className="settings-description">{admin ? zh ? '了解成员、用户组与模型的消耗分布。' : 'Explore usage across members, groups and models.' : zh ? '查看每一天、每位助理的 Token 与模型费用。' : 'Explore daily tokens and model spend for each assistant.'}</p><Button size="sm" variant="secondary" disabled={query.loading} onClick={refresh}><RefreshCw size={14} />{t.refresh}</Button></div>
  {(data?.Summary.ReferenceCalls ?? 0) > 0 && <p className="muted">{zh ? '含 ChatGPT 套餐调用，金额包含内部预算参考计价，不代表 OpenAI 实际扣费。' : 'Includes ChatGPT plan calls valued at internal reference prices, not OpenAI charges.'}</p>}
  <div className="usage-analytics-filters">
   <Select size="sm" label={zh ? '日期范围' : 'Date range'} value={period} onValueChange={v => { setPeriod(v); reset(); }} options={[{ value: 'year', label: zh ? '本年' : 'This year' }, { value: 'month', label: zh ? '本月' : 'This month' }, { value: '30', label: zh ? '近 30 天' : 'Last 30 days' }, { value: '90', label: zh ? '近 90 天' : 'Last 90 days' }, ...[1, 2, 3].map(n => ({ value: `year:${Number(year) - n}`, label: String(Number(year) - n) })), { value: 'custom', label: zh ? '自定义' : 'Custom' }]} />
   {!admin && <Select size="sm" label={zh ? '助理' : 'Assistant'} value={assistant} onValueChange={v => { setAssistant(v); reset(); }} options={opts(options.Assistants, zh ? '全部助理' : 'All assistants')} />}
   {admin && <><Select size="sm" label={zh ? '用户组' : 'User group'} value={group} onValueChange={v => { setGroup(v); chooseUser('all'); }} options={opts(options.Groups, zh ? '全部用户组' : 'All groups', 'group')} /><Select size="sm" label={zh ? '成员' : 'Member'} value={user} onValueChange={chooseUser} options={opts(options.Users, zh ? '全部成员' : 'All members')} /></>}
   <Select size="sm" label={zh ? '模型' : 'Model'} value={model} onValueChange={v => { setModel(v); reset(); }} options={opts(options.Models, zh ? '全部模型' : 'All models')} />
   {admin && <SearchField label={zh ? '搜索成员' : 'Search members'} value={search} onValueChange={setSearch} placeholder={zh ? '用户名或显示名称' : 'Username or display name'} />}
  </div>
  {period === 'custom' && <div className="usage-custom-dates"><TextField type="date" label={zh ? '开始日期' : 'Start date'} value={customFrom} onChange={e => { setCustomFrom(e.target.value); reset(); }} /><TextField type="date" label={zh ? '结束日期' : 'End date'} value={customTo} onChange={e => { setCustomTo(e.target.value); reset(); }} /><span>{zh ? '包含起止日期，最多一年' : 'Inclusive dates, up to one year'}</span></div>}
  {!valid && <p role="alert" className="usage-note">{zh ? '请选择有效的日期范围，跨度不超过一年。' : 'Choose a valid date range of up to one year.'}</p>}
  <div className="usage-scope"><span>{from} — {until ? dateAdd(until, -1) : '—'} · {timezone}</span>{[assistant, model, user, group].some(v => v !== 'all') || search ? <Button variant="ghost" size="sm" onClick={() => { setAssistant('all'); setModel('all'); chooseUser('all'); setGroup('all'); setSearch(''); }}><X size={13} />{zh ? '清除筛选' : 'Clear filters'}</Button> : null}</div>
  {data && (query.loading || Boolean(query.error)) && <p role="status" className="usage-note">{query.error ? zh ? '更新失败，以下保留上次统计；可点击刷新重试。' : 'Refresh failed. Previous statistics remain below; retry with Refresh.' : zh ? '正在更新统计…' : 'Updating statistics…'}</p>}
  {!data ? query.error || currency.error ? <Empty title={zh ? '用量暂时无法加载' : 'Usage is temporarily unavailable'} action={<Button variant="secondary" size="sm" onClick={refresh}>{zh ? '重试' : 'Retry'}</Button>} /> : valid && <Loading /> : <div className="usage-analytics-results" inert={!query.data || !valid}>
   <dl className="usage-analytics-overview">
    <div className="usage-primary-metric"><dt>{data.Summary.MissingTokenCalls ? zh ? '已记录 Token' : 'Recorded tokens' : zh ? '总 Token' : 'Total tokens'}</dt><dd title={data.Summary.TotalTokens.toLocaleString(locale)}>{compactTokens(data.Summary.TotalTokens, locale)}</dd><p>{zh ? '输入' : 'Input'} {compactTokens(data.Summary.InputTokens, locale)} · {zh ? '输出' : 'Output'} {compactTokens(data.Summary.OutputTokens, locale)}</p><p>{zh ? '其中缓存输入' : 'Including cached input'} {compactTokens(data.Summary.CachedTokens, locale)}</p></div>
    <div><dt>{zh ? '模型费用' : 'Model spend'}</dt><dd>{spend(data.Summary.Spent)}</dd><p>{zh ? '已确认消费' : 'Confirmed spend'}</p></div>
    <div><dt>{zh ? '模型调用' : 'Model calls'}</dt><dd>{data.Summary.ModelCalls.toLocaleString(locale)}</dd><p>{zh ? '所选日期范围内' : 'Within selected dates'}</p></div>
    <div><dt>{admin ? zh ? '活跃成员' : 'Active members' : zh ? '活跃天数' : 'Active days'}</dt><dd>{admin ? data.ActiveUsers : data.ActiveDays}</dd><p>{admin ? `${data.ActiveDays} ${zh ? '个活跃日' : 'active days'}` : zh ? '有模型调用的日期' : 'Days with model calls'}</p></div>
   </dl>
   {(data.Summary.MissingTokenCalls > 0 || data.Summary.UnknownCostCalls > 0 || data.Summary.ActiveCalls > 0) && <div className="usage-evidence-note" role="status">{data.Summary.MissingTokenCalls > 0 && <span>{data.Summary.MissingTokenCalls} {zh ? '次调用未取得 Token 用量' : 'calls without token usage'}</span>}{data.Summary.UnknownCostCalls > 0 && <span>{data.Summary.UnknownCostCalls} {zh ? '次调用费用未知' : 'calls with unknown cost'}</span>}{data.Summary.ActiveCalls > 0 && <span>{data.Summary.ActiveCalls} {zh ? '次调用执行中' : 'calls in progress'}</span>}</div>}
   <UsageCalendar data={data} selected={day} onSelect={d => { setDay(d); setPage(1); }} />
   <section className="usage-breakdown" aria-label={zh ? '用量明细' : 'Usage breakdown'}>
    <div className="usage-section-heading"><div><h3>{selectedDay ? `${day} · ${zh ? '当天明细' : 'Daily breakdown'}` : zh ? '用量明细' : 'Usage breakdown'}</h3><p>{selectedDay ? `${selectedDay.TotalTokens.toLocaleString(locale)} Token · ${spend(selectedDay.Spent)}` : zh ? '按所选日期与筛选条件汇总' : 'Totals for the selected dates and filters'}</p></div>{selectedDay && <Button size="sm" variant="ghost" onClick={() => { setDay(''); setPage(1); }}><X size={13} />{zh ? '查看整个范围' : 'Show full range'}</Button>}</div>
    <div className="usage-breakdown-controls"><SegmentedControl hideLabel label={zh ? '分组方式' : 'Group by'} value={groupBy} onValueChange={v => { setGroupBy(v); setPage(1); }} options={(admin ? ['user', 'group', 'model'] : ['assistant', 'model']).map(value => ({ value, label: ({ assistant: zh ? '按助理' : 'By assistant', model: zh ? '按模型' : 'By model', user: zh ? '按成员' : 'By member', group: zh ? '按用户组' : 'By group' } as Record<string, string>)[value] }))} /><SegmentedControl label={zh ? '排序' : 'Sort by'} value={sort} onValueChange={v => { setSort(v); setPage(1); }} options={[{ value: 'tokens', label: zh ? 'Token 从多到少' : 'Most tokens' }, { value: 'spend', label: zh ? '费用从高到低' : 'Highest spend' }, { value: 'calls', label: zh ? '调用从多到少' : 'Most calls' }]} /></div>
    <Table className="usage-table" caption={zh ? '用量明细' : 'Usage breakdown'} columns={columns} rows={data.Rows} rowKey={row => row.ID} emptyState={<Empty title={zh ? '暂无用量记录' : 'No usage recorded'} description={zh ? '当前日期与筛选条件下没有模型调用。' : 'No model calls match these dates and filters.'} />} />
    {data.TotalRows > data.PageSize && <Pagination page={page} totalPages={Math.ceil(data.TotalRows / data.PageSize)} onPageChange={setPage} /> }
   </section>
   <p className="usage-note">{zh ? '总 Token = 输入 + 输出，缓存已包含在输入中。费用不含执行中的预留；迟到回执计入调用发起日。' : 'Total tokens = input + output; cached tokens are part of input. Spend excludes active reservations; late receipts count toward the original call date.'}{admin && (zh ? ' 用户组按成员当前归属汇总。' : 'Groups reflect current membership.')}</p>
   <p className="usage-note">{zh ? '统计时间：' : 'As of: '}{new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short', timeZone: timezone }).format(new Date(data.AsOf))}{data.Summary.Spent.some(v => v.Currency !== data.Currency) && ` · ${zh ? '汇率日期' : 'Exchange rates as of'} ${data.Rates.as_of}`}</p>
  </div>}
 </div>;
}
