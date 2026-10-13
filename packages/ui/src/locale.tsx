import { chatMessages } from './chat-messages';
import { createContext, useContext } from 'react';
import type { ReactNode } from 'react';

export const supportedLocales = ['zh', 'en'] as const;
export type UILocale = typeof supportedLocales[number];
const LocaleContext = createContext<UILocale>('zh');
const BrandAssetContext = createContext('/brand');
const ReducedMotionContext = createContext(false);
const messages = {
  zh: {
    secretField: { showLabel: '显示密钥', hideLabel: '隐藏密钥' },
    codeBlock: { label: '代码内容' },
    rangeSlider: { start: '范围起点', end: '范围终点' },
    dateTime: { invalidTime: '所选时间在该时区不存在，请重新选择', timeOnly: '时间', monthAndYear: '月份与年份', presets: '时间快捷选择', '1d': '1天', '7d': '7天', '1m': '1个月', forever: '永久', chooseDate: '自定义', dateAndTime: '日期与时间', calendar: '日历', previousMonth: '上个月', nextMonth: '下个月', month: '月份', year: '年份', hour: '时', minute: '分', today: '今天', cancel: '取消', apply: '确定', beforeMinimum: '请选择允许范围内的时间'  },
    searchOptions: (label: string) => `搜索${label}`, searchOptionsPlaceholder: '搜索选项…', noOptions: '没有匹配的选项',
    connection: { connected: '已连接', reconnecting: '正在重连', disconnected: '已断开', failed: '重连失败', recovered: '连接恢复' },
    retryConnection: '点击重试连接',
    connectionNotice: { reconnecting: '正在重新连接', disconnected: '连接已中断', failed: '重连失败', retry: '重试连接' },
    chat: chatMessages.zh,
    animationPreparing: 'Molr，正在准备动画',
    animationWaiting: (motion: string) => `Molr，点击跳过等待，播放${motion}`,
    animationPlaying: (motion: string) => `Molr，正在${motion}，动作结束后可跳过等待`,
    animationMotions: { greet: '招呼', look: '张望', sleepy: '打盹', listen: '侧耳听', curious: '好奇', idle: '休闲', stretch: '伸懒腰', nuzzle: '轻蹭', blink: '眨眼', peek: '探头', earflick: '抖耳' },
    close: '关闭', clear: '清除搜索', select: '请选择', previous: '上一页', next: '下一页',
    pagination: '分页导航', breadcrumb: '当前位置', notifications: '通知', selectAll: '选择当前页的所有行',
    selectRow: (name: string) => `选择 ${name}`, page: (page: number) => `第 ${page} 页`,
    pageOf: (page: number, total: number) => `第 ${page} / ${total} 页`,
    sort: (name: string) => `按${name}排序`, loading: '正在加载', animation: 'Molr，点击跳过等待，播放下一段动画',
  },
  en: {
    secretField: { showLabel: 'Show secret', hideLabel: 'Hide secret' },
    codeBlock: { label: 'Code content' },
    rangeSlider: { start: 'Range start', end: 'Range end' },
    dateTime: { invalidTime: 'This time does not exist in the selected timezone', timeOnly: 'Time', monthAndYear: 'Month and year', presets: 'Time shortcuts', '1d': '1 day', '7d': '7 days', '1m': '1 month', forever: 'Forever', chooseDate: 'Custom', dateAndTime: 'Date and time', calendar: 'Calendar', previousMonth: 'Previous month', nextMonth: 'Next month', month: 'Month', year: 'Year', hour: 'Hour', minute: 'Minute', today: 'Today', cancel: 'Cancel', apply: 'Apply', beforeMinimum: 'Choose a time within the allowed range'  },
    searchOptions: (label: string) => `Search ${label}`, searchOptionsPlaceholder: 'Search options…', noOptions: 'No matching options',
    connection: { connected: 'Connected', reconnecting: 'Reconnecting', disconnected: 'Disconnected', failed: 'Reconnection failed', recovered: 'Connection restored' },
    retryConnection: 'Retry connection',
    connectionNotice: { reconnecting: 'Reconnecting', disconnected: 'Connection interrupted', failed: 'Reconnection failed', retry: 'Retry connection' },
    chat: chatMessages.en,
    animationPreparing: 'Molr, preparing animation',
    animationWaiting: (motion: string) => `Molr, skip the wait and play ${motion}`,
    animationPlaying: (motion: string) => `Molr, playing ${motion}; you can skip the wait after this animation`,
    animationMotions: { greet: 'greeting', look: 'looking around', sleepy: 'dozing', listen: 'listening', curious: 'curiosity', idle: 'relaxing', stretch: 'stretching', nuzzle: 'nuzzling', blink: 'blinking', peek: 'peeking', earflick: 'ear flicking' },
    close: 'Close', clear: 'Clear search', select: 'Choose an option', previous: 'Previous page', next: 'Next page',
    pagination: 'Pagination', breadcrumb: 'Breadcrumb', notifications: 'Notifications', selectAll: 'Select all rows on this page',
    selectRow: (name: string) => `Select ${name}`, page: (page: number) => `Page ${page}`,
    pageOf: (page: number, total: number) => `Page ${page} of ${total}`,
    sort: (name: string) => `Sort by ${name}`, loading: 'Loading', animation: 'Molr, skip the rest and play the next animation',
  },
};

export function UIProvider({ locale = 'zh', brandAssetBase, reducedMotion, children }: { locale?: UILocale; brandAssetBase?: string; reducedMotion?: boolean; children: ReactNode }) {
  const inheritedBase = useContext(BrandAssetContext);
  const inheritedMotion = useContext(ReducedMotionContext);
  return <LocaleContext.Provider value={locale}><BrandAssetContext.Provider value={brandAssetBase ?? inheritedBase}><ReducedMotionContext.Provider value={reducedMotion ?? inheritedMotion}>{children}</ReducedMotionContext.Provider></BrandAssetContext.Provider></LocaleContext.Provider>;
}
export function useBrandAssetBase() { return useContext(BrandAssetContext); }

export function useUILocale() {
  const locale = useContext(LocaleContext);
  return { locale, messages: messages[locale], reducedMotion: useContext(ReducedMotionContext) };
}
