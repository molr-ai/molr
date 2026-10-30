import { useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { Button, SegmentedControl, Select, useTheme } from '@molr/ui';
import { onMutation } from '@molr/sdk';
import type { Assistant, Grant, Palette } from '@molr/sdk';
import '@fontsource-variable/dm-sans';
import '@molr/ui/styles.css';
import '../../src/styles.css';
import './full-access-cards.css';
import { LocaleProvider, useAppearance, useCopy, useData } from '../../src/state';
import { ErrorToastProvider } from '../../src/error-toasts';
import { PermissionRequestCard } from '../../src/interfaces/official/features/PermissionRequests';
import { createCardCases, resources, sandboxes } from './full-access-cards-data';

// This development-only gallery renders production components with in-memory data.
// Every API request is handled here; reviewing a case never changes Runtime grants.
const originalFetch = window.fetch.bind(window);
let cases = createCardCases();
window.fetch = async (request, init) => {
  const url = new URL(typeof request === 'string' ? request : request instanceof URL ? request.href : request.url, location.origin);
  if (!url.pathname.startsWith('/api/')) return originalFetch(request, init);
  const reply = (json: unknown, status = 200) => new Response(JSON.stringify(json), { status, headers: { 'Content-Type': 'application/json', 'X-Molr-CSRF-Cookie': 'molr_dev_csrf' } });
  if (url.pathname === '/api/auth/me') return reply({});
  if (url.pathname === '/api/models') return reply([]);
  if (url.pathname === '/api/grants') return reply({ Items: cases.flatMap(c => c.request.GrantedAll ?? []), NextCursor: '' });
  if (url.pathname === '/api/sandboxes') return reply(sandboxes);
  if (url.pathname === '/api/resources') return reply({ Items: resources, NextCursor: '' });
  if (url.pathname.startsWith('/api/resources/')) return reply(resources.find(r => r.ID === url.pathname.split('/').at(-1)));
  if (url.pathname === '/api/permission-requests/resolve') {
    const data = JSON.parse(String(init?.body));
    const row = cases.find(c => c.request.ID === data.RequestID)?.request;
    if (!row) return reply({ code: 'not_found' }, 404);
    row.State = data.Decision === 'approve' ? 'approved' : 'rejected';
    if (row.State === 'approved') {
      const grants: Grant[] = [data, ...(data.Additional ?? [])].map((item, i) => ({ ID: `preview-grant-${i}`, AssistantID: row.AssistantID, ResourceID: item.ResourceID, Actions: item.Actions, Scope: item.Scope, ExpiresAt: item.ExpiresAt, Constraints: item.Constraints, Temporary: item.Temporary, Version: 1, RevokedAt: null }));
      row.Granted = grants[0]; row.GrantedAll = grants;
    }
    row.Version++; return reply(row);
  }
  if (url.pathname.startsWith('/api/permission-requests/')) return reply(cases.find(c => c.request.ID === url.pathname.split('/').at(-1))?.request);
  return reply({ code: 'not_found' }, 404);
};
const assistant = { ID: 'preview-assistant', Name: '范围预览助理', State: 'active' } as Assistant;
function Gallery() {
  const { locale, setLocale } = useCopy(), zh = locale === 'zh';
  const { theme, changePreference } = useTheme();
  const { palette, changePalette } = useAppearance();
  const { topics } = useData();
  const params = new URLSearchParams(location.search), selected = params.get('case');
  const visible = selected ? cases.filter(c => c.id === selected) : cases;
  useEffect(() => onMutation(() => topics.invalidate(['all'])), [topics]);
  useEffect(() => { setLocale(params.get('lang') === 'en' ? 'en' : 'zh'); }, []);
  return <main className="permission-gallery">
    <header className="permission-gallery__header"><div><p className="permission-gallery__eyebrow">FULL ACCESS</p><h1>{zh ? '权限申请卡片' : 'Permission request cards'}</h1><p>{zh ? '分别查看授权范围、参数规则与审批状态。示例操作仅在本页生效。' : 'Review each scope, argument rule and approval state. Example actions affect this page only.'}</p></div>
      <div className="permission-gallery__controls">
        <SegmentedControl size="sm" label={zh ? '语言' : 'Language'} value={locale} onValueChange={v => setLocale(v as 'zh' | 'en')} options={[{ value: 'zh', label: '中文' }, { value: 'en', label: 'English' }]} />
        <SegmentedControl size="sm" label={zh ? '显示模式' : 'Appearance'} value={theme} onValueChange={v => changePreference(v as 'light' | 'dark')} options={[{ value: 'light', label: zh ? '浅色' : 'Light' }, { value: 'dark', label: zh ? '深色' : 'Dark' }]} />
        <Select label={zh ? '配色' : 'Palette'} value={palette} onValueChange={v => changePalette(v as Palette)} options={['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome'].map(value => ({ value, label: value }))} />
      </div>
    </header>
    <nav className="permission-gallery__nav" aria-label={zh ? '预览案例' : 'Preview cases'}><a href="full-access-cards.html">{zh ? '全部案例' : 'All cases'}</a>{cases.map(c => <a key={c.id} href={`?case=${c.id}`} aria-current={selected === c.id ? 'page' : undefined}>{zh ? c.zh : c.en}</a>)}</nav>
    <div className="permission-gallery__grid">{visible.map(c => <section className="permission-gallery__case" id={c.id} key={c.id}>
      <h2><span>{String(cases.indexOf(c) + 1).padStart(2, '0')}</span>{zh ? c.zh : c.en}</h2>
      <PermissionRequestCard requestID={c.request.ID} assistant={assistant} title="" summary="" timeZone="Asia/Shanghai" />
    </section>)}</div>
    <footer className="permission-gallery__footer"><Button variant="secondary" onClick={() => { cases = createCardCases(); topics.invalidate(['all']); }}>{zh ? '重置示例' : 'Reset examples'}</Button></footer>
  </main>;
}
createRoot(document.getElementById('root')!).render(<LocaleProvider><ErrorToastProvider><Gallery /></ErrorToastProvider></LocaleProvider>);
