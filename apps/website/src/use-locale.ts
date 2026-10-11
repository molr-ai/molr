import { useEffect, useLayoutEffect, useState } from 'react';
import { docLocale, isDocPath } from './doc-locale';
import { messages } from './site-content';
import type { Locale } from './site-content';
import type { SiteMode } from './site-mode';

function browserLocale(): Locale {
  const languages = navigator.languages.length ? navigator.languages : [navigator.language];
  for (const language of languages) {
    const base = language.toLowerCase().split(/[-_]/)[0];
    if (base === 'zh' || base === 'en') return base;
  }
  return 'en';
}

const storageKey = 'molr-website-locale';

function preferredLocale(mode: SiteMode): Locale {
  if (mode === 'live') {
    try {
      const stored = localStorage.getItem(storageKey);
      if (stored === 'zh' || stored === 'en') return stored;
    } catch { /* Browser language remains available without storage. */ }
  }
  return browserLocale();
}

export function useLocale(mode: SiteMode, pathname: string) {
  const [preference, setLocale] = useState<Locale>(() => preferredLocale(mode));
  const [manualLocale, setManualLocale] = useState<Locale | null>(null);
  const locale = (mode === 'live' ? docLocale(pathname) : undefined) ?? preference;
  const changeLocale = (next: Locale) => {
    setManualLocale(next);
    setLocale(next);
    try { localStorage.setItem(storageKey, next); } catch { /* Keep the in-memory preference. */ }
  };
  const copy = messages[locale].pages[mode];

  useEffect(() => {
    const synchronize = () => setLocale(mode === 'live' && manualLocale ? manualLocale : preferredLocale(mode));
    const resume = () => { if (document.visibilityState === 'visible') synchronize(); };
    synchronize();
    window.addEventListener('languagechange', synchronize);
    window.addEventListener('focus', resume);
    document.addEventListener('visibilitychange', resume);
    return () => {
      window.removeEventListener('languagechange', synchronize);
      window.removeEventListener('focus', resume);
      document.removeEventListener('visibilitychange', resume);
    };
  }, [mode, manualLocale]);

  useLayoutEffect(() => {
    document.documentElement.lang = locale === 'zh' ? 'zh-CN' : 'en';
    document.documentElement.dataset.locale = locale;
    document.querySelector('meta[property="og:locale"]')?.setAttribute('content', locale === 'zh' ? 'zh_CN' : 'en_US');
    if (mode === 'live' && isDocPath(pathname)) return;
    document.querySelectorAll('link[hreflang]').forEach(link => link.remove());
    document.querySelector('link[rel="canonical"]')?.setAttribute('href', 'https://molr.ai/');
    document.querySelector('meta[property="og:url"]')?.setAttribute('content', 'https://molr.ai/');
    document.title = copy.title;
    document.querySelector('meta[name="description"]')?.setAttribute('content', copy.description);
    document.querySelector('meta[property="og:title"]')?.setAttribute('content', copy.title);
    document.querySelector('meta[property="og:description"]')?.setAttribute('content', copy.description);
  }, [locale, copy, mode, pathname]);

  return { locale, changeLocale, copy, logoAction: messages[locale].logoAction };
}
