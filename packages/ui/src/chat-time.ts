import type { UILocale } from './locale';

const formatters = new Map<string, Intl.DateTimeFormat>();
const formatterLimit = 32;

function chatFormatter(locale: UILocale, timeZone?: string) {
  // An implicit browser/server zone can change without a prop change. Resolve it
  // afresh as before; explicit account zones can safely share a formatter.
  const key = timeZone === undefined ? undefined : `${locale}:${timeZone}`;
  const cached = key === undefined ? undefined : formatters.get(key);
  if (cached) return cached;
  const formatter = new Intl.DateTimeFormat(locale === 'zh' ? 'zh-CN' : 'en-US', {
    timeZone, year: 'numeric', month: locale === 'zh' ? 'numeric' : 'long', day: 'numeric',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
  });
  if (key !== undefined) {
    if (formatters.size >= formatterLimit) formatters.delete(formatters.keys().next().value!);
    formatters.set(key, formatter);
  }
  return formatter;
}

/** Compare calendar dates in the same zone used to display the message. */
export function formatChatTime(timestamp: string, locale: UILocale, timeZone?: string, now = new Date()) {
  const date = new Date(timestamp);
  if (!Number.isFinite(date.getTime())) return undefined;
  const formatter = chatFormatter(locale, timeZone);
  const parts = (value: Date) => Object.fromEntries(formatter.formatToParts(value).map(part => [part.type, part.value]));
  const sent = parts(date), current = parts(now);
  const time = `${sent.hour}:${sent.minute}`;
  const day = locale === 'zh' ? `${sent.month}月${sent.day}日` : `${sent.month} ${sent.day}`;
  const full = locale === 'zh' ? `${sent.year}年 ${day} ${time}` : `${day}, ${sent.year}, ${time}`;
  const sameYear = sent.year === current.year;
  const sameDay = sameYear && sent.month === current.month && sent.day === current.day;
  return { text: sameDay ? time : sameYear ? `${day}${locale === 'zh' ? ' ' : ', '}${time}` : full, full };
}
