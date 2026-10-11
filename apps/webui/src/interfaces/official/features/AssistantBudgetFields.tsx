import { useErrorToast } from '../../../error-toasts';
import { Select } from '@molr/ui';
import type { CurrencyCode, CurrencyContext, Usage } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { MoneyInput, MoneyText, useCurrency } from '../components/Money';
import { timezoneLabel } from '../../../currency';

export function AssistantBudgetFields({ limit, currency, period, context, onLimitChange, onCurrencyChange, onPeriodChange }: { limit: string; currency: CurrencyCode; period: 'daily' | 'weekly'; context: CurrencyContext; onLimitChange: (value: string) => void; onCurrencyChange: (value: CurrencyCode) => void; onPeriodChange: (value: 'daily' | 'weekly') => void }) {
  const { t } = useCopy();
  return <>
    <MoneyInput label={t.assistants.budgetLimit} value={limit} currency={currency} onChange={onLimitChange} onCurrencyChange={onCurrencyChange} />
    <Select label={t.assistants.budgetPeriod} value={period} onValueChange={onPeriodChange} options={[{ value: 'daily', label: t.assistants.daily }, { value: 'weekly', label: t.assistants.weekly }]} />
    <p className="muted">{t.assistants.budgetHelp.replace('{timezone}', timezoneLabel(context.Timezone, t.settings.timezoneNames, true)).replace('{reset}', period === 'daily' ? t.assistants.dailyReset : t.assistants.weeklyReset)}</p>
  </>;
}
export function AssistantBudgetUsage({ assistantID }: { assistantID: string }) {
  const { t, locale } = useCopy();
  const usage = useQuery<Usage>(`/api/usage?assistant_id=${assistantID}&current_period=true`);
  const currency = useCurrency();
  useErrorToast(usage.error || currency.error, () => { usage.reload(); currency.reload(); });
  return <>{usage.data && <dl className="connection-details">
    {usage.data.Limit && <div><dt>{t.assistants.budgetSummary}</dt><dd><MoneyText value={usage.data.Limit} /></dd></div>}
    <div><dt>{t.assistants.periodSpent}</dt><dd><MoneyText value={usage.data.Spent} originals={usage.data.OriginalSpent} /></dd></div>
    <div><dt>{t.settings.reserved}</dt><dd><MoneyText value={usage.data.Reserved} originals={usage.data.OriginalReserved} /></dd></div>
    {usage.data.PeriodEnd && currency.data && <div><dt>{t.assistants.nextReset}</dt><dd>{new Intl.DateTimeFormat(locale, { dateStyle: 'short', timeStyle: 'short', timeZone: currency.data.Timezone }).format(new Date(usage.data.PeriodEnd))}</dd></div>}
  </dl>}</>;
}
