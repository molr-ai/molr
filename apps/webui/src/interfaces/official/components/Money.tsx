import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { Dialog, Drawer, Select, TextField } from '@molr/ui';
import type { CurrencyContext, CurrencyCode, Money } from '../../../api/types';
import { api } from '../../../api/client';
import { useCopy, useQuery } from '../../../state';
import { convertMoney, currencyText } from '../../../currency';
import { ErrorNotice, Loading } from './common';

export const useCurrency = () => useQuery<CurrencyContext>('/api/settings/currency');

// Fetch once on opening. Preserve the entered denomination throughout editing,
// including when another client changes the display preference or rate snapshot.
export function CurrencyDialog({ title, onClose, children, drawer = false, dismissible = true, className, description, docked }: { title: string; onClose: () => void; children: (context: CurrencyContext) => ReactNode; drawer?: boolean; dismissible?: boolean; className?: string; description?: string; docked?: boolean }) {
  const [context, setContext] = useState<CurrencyContext>(), [error, setError] = useState<unknown>(), [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const abort = new AbortController(); setError(undefined);
    void api.get<CurrencyContext>('/api/settings/currency', abort.signal).then(value => { if (!abort.signal.aborted) setContext(value); }, error => { if (!abort.signal.aborted) setError(error); });
    return () => abort.abort();
  }, [attempt]);
  const pending = <><ErrorNotice error={error} retry={() => setAttempt(value => value + 1)} />{!error && <Loading />}</>;
  const close = (open: boolean) => { if (!open && dismissible) onClose(); };
  if (drawer) return <Drawer docked={docked} open side="right" className={className} description={description} title={title} onOpenChange={close} dismissible={dismissible}>{context ? children(context) : pending}</Drawer>;
  return context ? children(context) : <Dialog open title={title} onOpenChange={close} dismissible={dismissible}>{pending}</Dialog>;
}
export function CurrencySelect({ value, onChange }: { value: CurrencyCode; onChange: (value: CurrencyCode) => void }) {
  const { t } = useCopy();
  return <Select label={t.settings.currency} value={value} onValueChange={onChange} options={[{ value: 'USD', label: t.settings.currencies.USD }, { value: 'CNY', label: t.settings.currencies.CNY }]} />;
}
export function MoneyInput({ label, value, currency, onChange, onCurrencyChange }: { label: string; value: string; currency: CurrencyCode; onChange: (value: string) => void; onCurrencyChange: (value: CurrencyCode) => void }) {
  return <div className="money-input"><TextField label={label.replace('{currency}', currency)} type="number" min="0" step="0.000001" value={value} onChange={event => onChange(event.target.value)} required /><CurrencySelect value={currency} onChange={onCurrencyChange} /></div>;
}
export function MoneyText({ value, originals, context }: { value: Money; originals?: Money[] | null; context?: CurrencyContext }) {
  const { locale } = useCopy();
  const query = useQuery<CurrencyContext>(context ? null : '/api/settings/currency');
  const info = context ?? query.data;
  const native = originals?.length ? originals : [value];
  const original = native.map(amount => currencyText(amount, locale)).join(' + ');
  if (!info) return <span>{original}</span>;
  if (native.every(amount => amount.Currency === info.Currency)) return <span>{native.length === 1 ? original : currencyText({ Currency: info?.Currency ?? value.Currency, Micros: native.reduce((sum, amount) => sum + amount.Micros, 0) }, locale)}</span>;
  try {
    const total = native.reduce((sum, amount) => sum + convertMoney(amount, info.Currency, info.Rates).Micros, 0);
    if (!Number.isSafeInteger(total)) throw new Error('Amount exceeds display precision');
    return <span title={info.Rates.as_of}>≈ {currencyText({ Currency: info.Currency, Micros: total }, locale)}{locale === 'zh' ? `（${original}）` : ` (${original})`}</span>;
  } catch { return <span>{original}</span>; }
}
