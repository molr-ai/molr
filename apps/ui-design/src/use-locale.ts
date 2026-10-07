import { useEffect, useLayoutEffect, useState } from 'react';
import { messages } from './messages';
import type { Locale } from './messages';

const storageKey = 'molr-ui-design-locale';

function browserLocale(): Locale {
  for (const language of navigator.languages.length ? navigator.languages : [navigator.language]) {
    const base = language.toLowerCase().split(/[-_]/)[0];
    if (base === 'zh' || base === 'en') return base;
  }
  return 'en';
}

function readPreference(): Locale | null {
  try {
    const stored = localStorage.getItem(storageKey);
    if (stored === 'zh' || stored === 'en') return stored;
  } catch {
    // Language switching still works when browser storage is unavailable.
  }
  return null;
}

export function useLocale() {
  const [preference, setPreference] = useState(readPreference);
  const [systemLocale, setSystemLocale] = useState(browserLocale);
  const locale = preference ?? systemLocale;
  const copy = messages[locale];

  useEffect(() => {
    const synchronizeLanguage = () => setSystemLocale(browserLocale());
    const synchronizeStorage = (event: StorageEvent) => {
      if (event.key === storageKey || event.key === null) setPreference(readPreference());
    };
    window.addEventListener('languagechange', synchronizeLanguage);
    window.addEventListener('storage', synchronizeStorage);
    return () => {
      window.removeEventListener('languagechange', synchronizeLanguage);
      window.removeEventListener('storage', synchronizeStorage);
    };
  }, []);

  useLayoutEffect(() => {
    document.documentElement.lang = locale === 'zh' ? 'zh-CN' : 'en';
    document.title = copy.meta.title;
    document.querySelector('meta[name="description"]')?.setAttribute('content', copy.meta.description);
  }, [locale, copy]);

  function changeLocale(next: Locale) {
    setPreference(next);
    try {
      localStorage.setItem(storageKey, next);
    } catch {
      // Keep the chosen language for this page even without persistence.
    }
  }

  return { locale, copy, changeLocale };
}
