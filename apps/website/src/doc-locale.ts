import type { Locale } from './site-content';

export function docLocale(pathname: string): Locale | undefined {
  return /^\/(zh|en)\/docs(?:\/|$)/.exec(pathname)?.[1] as Locale | undefined;
}

export function isDocPath(pathname: string) {
  return /^\/(?:(?:zh|en)\/)?docs(?:\/|$)/.test(pathname);
}

export function localizedDocPath(pathname: string, locale: Locale) {
  return isDocPath(pathname) ? `/${locale}${pathname.replace(/^\/(zh|en)(?=\/)/, '')}` : pathname;
}
