import type { CurrencyContext, Money } from './types';

export type UsageTotals = { ReferenceCalls?: number; InputTokens: number; OutputTokens: number; CachedTokens: number; TotalTokens: number; ModelCalls: number; MissingTokenCalls: number; UnknownCostCalls: number; ActiveCalls: number; Spent: Money[] };
export type UsageOption = { ID: string; Name: string; Secondary: string; State: string };
export type UsageDay = UsageTotals & { Date: string };
export type UsageRow = UsageTotals & UsageOption;
export type UsageAnalyticsView = CurrencyContext & { From: string; Until: string; AsOf: string; Summary: UsageTotals; ActiveDays: number; ActiveUsers: number; Daily: UsageDay[]; Rows: UsageRow[]; TotalRows: number; Page: number; PageSize: number; Options: { Assistants: UsageOption[]; Models: UsageOption[]; Users: UsageOption[]; Groups: UsageOption[] } };
