import type { CurrencyContext, Money } from '@molr/sdk';
import { convertMoney, currencyText } from '../currency';
export type UsageTotals = { ReferenceCalls?: number; InputTokens: number; OutputTokens: number; CachedTokens: number; TotalTokens: number; ModelCalls: number; MissingTokenCalls: number; UnknownCostCalls: number; ActiveCalls: number; Spent: Money[] };
export type UsageOption = { ID: string; Name: string; Secondary: string; State: string };
export type UsageDay = UsageTotals & { Date: string };
export type UsageRow = UsageTotals & UsageOption;
export type UsageAnalyticsView = CurrencyContext & { From: string; Until: string; AsOf: string; Summary: UsageTotals; ActiveDays: number; ActiveUsers: number; Daily: UsageDay[]; Rows: UsageRow[]; TotalRows: number; Page: number; PageSize: number; Options: { Assistants: UsageOption[]; Models: UsageOption[]; Users: UsageOption[]; Groups: UsageOption[] } };
export const dateAdd = (date: string, days: number) => { const d = new Date(`${date}T12:00:00Z`); d.setUTCDate(d.getUTCDate() + days); return d.toISOString().slice(0, 10); };
export const calendarDate = (at: Date, timezone: string) => { const parts = new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(at); return ['year', 'month', 'day'].map(type => parts.find(p => p.type === type)?.value).join('-'); };
export const compactTokens = (n: number, locale: string) => new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 2 }).format(n);
export function amountMicros(values: Money[], context: CurrencyContext) { const sum = values.reduce((n, value) => n + convertMoney(value, context.Currency, context.Rates).Micros, 0); if (!Number.isSafeInteger(sum)) throw new Error('Amount overflow'); return sum; }
export function amountLabel(values: Money[], context: CurrencyContext, locale: string) { try { return `${values.some(v => v.Currency !== context.Currency) ? '≈ ' : ''}${currencyText({ Currency: context.Currency, Micros: amountMicros(values, context) }, locale)}`; } catch { return '—'; } }
export const emptyUsage: UsageTotals = { InputTokens: 0, OutputTokens: 0, CachedTokens: 0, TotalTokens: 0, ModelCalls: 0, MissingTokenCalls: 0, UnknownCostCalls: 0, ActiveCalls: 0, Spent: [] };
