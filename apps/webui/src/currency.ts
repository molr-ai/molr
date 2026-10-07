import type { ExchangeRates, Money } from './api/types';

export function convertMoney(value: Money, currency: string, rates: ExchangeRates): Money {
  if (value.Currency === currency) return value;
  const from = rates.units[value.Currency], to = rates.units[currency];
  if (!Number.isSafeInteger(value.Micros) || value.Micros < 0 || !from || !to) throw new Error('Unsupported amount or currency');
  const micros = (BigInt(value.Micros) * BigInt(to) + BigInt(from) - 1n) / BigInt(from);
  if (micros > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('Amount exceeds display precision');
  return { Currency: currency, Micros: Number(micros) };
}
export function currencyText(value: Money, locale: string) {
  return `${new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 6 }).format(value.Micros / 1_000_000)} ${value.Currency}`;
}
export function timezoneLabel(zone: string, names: Record<string, string>, withID = false, at = new Date()) {
  let offset = '';
  try {
    const raw = new Intl.DateTimeFormat('en', { timeZone: zone, timeZoneName: 'longOffset' }).formatToParts(at).find(part => part.type === 'timeZoneName')?.value ?? '';
    offset = raw === 'GMT' ? 'UTC+00:00' : raw.replace('GMT', 'UTC');
  } catch { /* Retain the actual saved zone even if this browser lacks its data. */ }
  const name = names[zone] ?? zone.replaceAll('_', ' ');
  return `${offset} ${name}${withID && name !== zone ? ` (${zone})` : ''}`.trim();
}
