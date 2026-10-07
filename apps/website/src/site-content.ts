import type { UILocale } from '@molr/ui';
import type { SiteMode, StatusMode } from './site-mode.ts';

export type Locale = UILocale;
type PageCopy = { heading: string; title: string; description: string };

export const messages: Record<Locale, { logoAction: string; pages: Record<SiteMode, PageCopy> }> = {
  zh: {
    logoAction: 'Molr，播放下一段动画',
    pages: {
      'coming-soon': { heading: '即将上线', title: 'Molr — 即将上线', description: 'Molr，即将上线。' },
      maintenance: { heading: '马上回来', title: 'Molr — 马上回来', description: 'Molr 正在更新，马上回来。' },
      live: { heading: '从好奇出发。', title: 'Molr — 从好奇出发', description: 'Molr，从好奇出发。你的个人 AI 助理，全天候工作，从你的反馈中学习。本地模型，也是一等公民，自由选择云端或本地模型，在你设定的权限与预算内行动。' },
    },
  },
  en: {
    logoAction: 'Molr, play the next animation',
    pages: {
      'coming-soon': { heading: 'Coming soon', title: 'Molr — Coming soon', description: 'Molr. Coming soon.' },
      maintenance: { heading: 'Be right back', title: 'Molr — Be right back', description: 'Molr is being updated. Be right back.' },
      live: { heading: 'It starts with curiosity.', title: 'Molr — It starts with curiosity.', description: 'Molr. It starts with curiosity. Your personal AI assistant works around the clock and learns from your feedback. Local models are first-class citizens, too. Choose cloud or local models, within the permissions and budget you set.' },
    },
  },
};

export const statusPhrases: readonly ({ language: string } & Record<StatusMode, string>)[] = [
  { language: 'zh-CN', 'coming-soon': '即将上线', maintenance: '马上回来' },
  { language: 'en', 'coming-soon': 'Coming soon', maintenance: 'Be right back' },
  { language: 'ja', 'coming-soon': 'まもなく公開', maintenance: 'まもなく戻ります' },
  { language: 'fr', 'coming-soon': 'Bientôt disponible', maintenance: 'De retour bientôt' },
  { language: 'ko', 'coming-soon': '곧 공개됩니다', maintenance: '곧 돌아올게요' },
  { language: 'es', 'coming-soon': 'Próximamente', maintenance: 'Volvemos pronto' },
  { language: 'de', 'coming-soon': 'Demnächst', maintenance: 'Bis gleich' },
  { language: 'it', 'coming-soon': 'In arrivo', maintenance: 'Torniamo presto' },
  { language: 'pt', 'coming-soon': 'Em breve', maintenance: 'Voltamos já' },
  { language: 'ar', 'coming-soon': 'قريبًا', maintenance: 'سنعود قريبًا' },
];
