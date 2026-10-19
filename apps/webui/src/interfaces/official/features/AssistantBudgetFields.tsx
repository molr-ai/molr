import { Select } from '@molr/ui';
import type { CurrencyCode } from '../../../api/types';
import { useCopy } from '../../../state';
import { MoneyInput } from '../components/Money';

export function AssistantBudgetFields({ limit, currency, period, onLimitChange, onCurrencyChange, onPeriodChange }: { limit: string; currency: CurrencyCode; period: 'daily' | 'weekly'; onLimitChange: (value: string) => void; onCurrencyChange: (value: CurrencyCode) => void; onPeriodChange: (value: 'daily' | 'weekly') => void }) {
  const { t } = useCopy();
  return <>
    <MoneyInput label={t.assistants.budgetLimit} value={limit} currency={currency} onChange={onLimitChange} onCurrencyChange={onCurrencyChange} />
    <Select label={t.assistants.budgetPeriod} value={period} onValueChange={onPeriodChange} options={[{ value: 'daily', label: t.assistants.daily }, { value: 'weekly', label: t.assistants.weekly }]} />
  </>;
}
