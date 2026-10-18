import { test, expect } from '@playwright/test';
import { browserCookie, browserCookieName, runtimeCookies } from '../cookie-proxy';

test('proxy forwards only the current port credentials, including pre-login CSRF', () => {
  const header = 'molr_dev_session=legacy; molr_dev_session_p5194=other; molr_dev_session_p5193=current; molr_dev_login_p5193=challenge; molr_dev_csrf_p5193=proof; unrelated=private';
  expect(runtimeCookies(header, '5193')).toBe('molr_dev_session_p5193=current; molr_dev_csrf_p5193=proof; molr_dev_login_p5193=challenge');
  expect(runtimeCookies('molr_dev_session=legacy; molr_dev_session_p5194=other', '5193')).toBe('');
  expect(runtimeCookies(undefined, '5193')).toBe('');
  expect(runtimeCookies('__Host-molr_session_p5193=secure; __Host-molr_csrf_p5193=proof', '5193')).toBe('__Host-molr_session_p5193=secure; __Host-molr_csrf_p5193=proof');
});

test('proxy preserves secure cookie attributes and scopes logout deletions', () => {
  const attributes = '=token; Path=/; Secure; HttpOnly; SameSite=Strict; Expires=Wed, 21 Oct 2026 07:28:00 GMT';
  expect(browserCookie(`__Host-molr_session${attributes}`, '5193')).toBe(`__Host-molr_session_p5193${attributes}`);
  expect(browserCookie('molr_dev_csrf=; Path=/; Max-Age=0; SameSite=Strict', '5193')).toBe('molr_dev_csrf_p5193=; Path=/; Max-Age=0; SameSite=Strict');
  expect(browserCookie('other=value; Path=/', '5193')).toBe('other=value; Path=/');
});


test('LAN Runtime credentials keep the correct port in both proxy directions', () => {
  expect(browserCookie('molr_lan_session_p8787=token; Path=/; HttpOnly; SameSite=Strict', '5193')).toBe('molr_lan_session_p5193=token; Path=/; HttpOnly; SameSite=Strict');
  expect(runtimeCookies('molr_lan_session_p5193=token; molr_lan_csrf_p5193=proof; molr_lan_session_p5194=other', '5193', '8787')).toBe('molr_lan_session_p8787=token; molr_lan_csrf_p8787=proof');
});

test('CSRF discovery uses exactly the cookie name exposed by the proxy', () => {
  for (const name of ['__Host-molr_csrf', '__Host-molr_csrf_p8443', 'molr_dev_csrf', 'molr_dev_csrf_p8787', 'molr_lan_csrf_p8787']) {
    expect(browserCookieName(name, '5193')).toBe(browserCookie(`${name}=proof; Path=/`, '5193').split('=')[0]);
  }
  expect(browserCookieName('molr_lan_csrf_p8787', '5193')).toBe('molr_lan_csrf_p5193');
});
