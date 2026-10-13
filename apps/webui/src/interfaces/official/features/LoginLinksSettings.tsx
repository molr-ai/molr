import { useErrorToast } from '../../../error-toasts';
import { usePluginsVisible } from '../../../plugins/visibility';
import { useEffect, useMemo, useRef, useState } from 'react';
import { ModalPresence, Badge, Button, Dialog, Select, TextField } from '@molr/ui';
import { Copy, Plus } from 'lucide-react';
import { api } from '../../../api/client';
import { useCopy, useQuery, useSession } from '../../../state';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { Confirm, EditDialog, Loading } from '../components/common';

type LoginLink = { ID: string; Name: string; TargetPath: string; CreatedAt: string; ExpiresAt: string; UsedAt: string | null; RevokedAt: string | null; PendingMFAUntil?: string | null };
type IssuedLink = { link: LoginLink; value: string };

export function LoginLinksSettings() {
  const { t, locale } = useCopy(), query = useQuery<LoginLink[]>('/api/auth/login-links', true);
  const [creating, setCreating] = useState(false), [issued, setIssued] = useState<{ url: string; link: LoginLink } | null>(null), [revoking, setRevoking] = useState<LoginLink | null>(null);
  const [now, setNow] = useState(Date.now);
  const dates = useMemo(() => new Map((query.data ?? []).map(link => [link.ID, new Date(link.ExpiresAt).toLocaleString(locale)])), [query.data, locale]);
  useEffect(() => {
    let timer: number | undefined;
    function sync() {
      window.clearTimeout(timer);
      if (document.visibilityState === 'hidden') return;
      const current = Date.now();
      setNow(current);
      let next = Infinity;
      for (const link of query.data ?? []) {
        const expiry = Date.parse(link.ExpiresAt);
        // Used links still expose revoke until they expire.
        if (!link.RevokedAt && expiry > current && expiry < next) next = expiry;
        const pending = Date.parse(link.PendingMFAUntil ?? "");
        if (pending > current && pending < next) next = pending;
      }
      if (next < Infinity) timer = window.setTimeout(sync, Math.min(next - current, 2_147_483_647));
    }
    sync();
    document.addEventListener('visibilitychange', sync);
    return () => { window.clearTimeout(timer); document.removeEventListener('visibilitychange', sync); };
  }, [query.data]);
  useErrorToast(query.error, query.reload);
  function active(link: LoginLink) {
    return !link.RevokedAt && Date.parse(link.ExpiresAt) > now && (!link.UsedAt || Date.parse(link.PendingMFAUntil ?? '') > now);
  }
  const current = (query.data ?? []).filter(active), history = (query.data ?? []).filter(link => !active(link));
  function row(link: LoginLink) {
    const expired = Date.parse(link.ExpiresAt) <= now;
    const pending = Date.parse(link.PendingMFAUntil ?? '') > now;
    const status = link.RevokedAt ? 'revoked' : link.UsedAt ? (pending && !expired ? 'awaitingMFA' : 'used') : expired ? 'expired' : 'pending';
    return <article className="settings-row-item" key={link.ID}><div className="settings-row-item-head"><div className="security-record-title"><h4>{link.Name}</h4><Badge tone={status === 'pending' ? 'positive' : 'neutral'}>{t.loginLinks[status]}</Badge></div>{!link.RevokedAt && !expired && <span className="settings-row-item-actions"><Button variant="ghost" size="sm" onClick={() => setRevoking(link)}>{t.loginLinks.revoke}</Button></span>}</div><dl className="security-record-meta"><div><dt>{t.loginLinks.expires}</dt><dd><time dateTime={link.ExpiresAt}>{dates.get(link.ID)}</time></dd></div></dl></article>;
  }
  return <section className="login-links-settings settings-group settings-card" aria-label={t.loginLinks.title}>
    <header className="record-heading security-section-heading"><div><h3 className="settings-group-title">{t.loginLinks.title}</h3><p className="security-description">{t.loginLinks.help}</p></div><Button size="sm" variant="secondary" onClick={() => setCreating(true)}><Plus />{t.loginLinks.create}</Button></header>
    {!query.data && query.loading ? <Loading /> : current.length ? <div className="settings-rows">{current.map(row)}</div> : <p className="security-description security-empty">{t.loginLinks.empty}</p>}
    {history.length > 0 && <details className="login-link-history"><summary>{t.loginLinks.history}<span className="security-history-count">{history.length}</span></summary><p className="security-description">{t.loginLinks.historyHelp}</p><div className="settings-rows">{history.map(row)}</div></details>}
    <ModalPresence>{creating && <CreateLoginLink onClose={() => setCreating(false)} onIssued={result => {
      const url = new URL(import.meta.env.BASE_URL, location.origin);
      url.hash = `/login?${new URLSearchParams({ token: result.value })}`;
      setIssued({ url: url.href, link: result.link }); setCreating(false); query.reload();
    }} />}</ModalPresence>
    <ModalPresence>{issued && <IssuedLoginLink url={issued.url} link={issued.link} onClose={() => setIssued(null)} />}</ModalPresence>
    <ModalPresence>{revoking && <Confirm title={t.loginLinks.revoke} description={t.loginLinks.revokeHelp} onClose={() => setRevoking(null)} onConfirm={async () => { await api.remove(`/api/auth/login-links/${revoking.ID}`); query.reload(); }} />}</ModalPresence>
  </section>;
}
function CreateLoginLink({ onClose, onIssued }: { onClose: () => void; onIssued: (result: IssuedLink) => void }) {
  const pluginsVisible = usePluginsVisible();
  const { t, locale } = useCopy(), { principal } = useSession(), verification = useReauthentication();
  const [name, setName] = useState(''), [expiry, setExpiry] = useState('3600'), [target, setTarget] = useState('/assistants');
  const destination = !pluginsVisible && target === '/extensions' ? '/assistants' : target;
  return <><EditDialog notifyOnSave={false} title={t.loginLinks.create} onClose={onClose} onSubmit={async () => {
    const proof = await verification.verify('login_link.create');
    onIssued(await api.post<IssuedLink>('/api/auth/login-links', { name, expires_in: Number(expiry), target_path: destination, verification: proof }));
  }}><TextField label={t.name} value={name} required maxLength={128} autoFocus onChange={event => setName(event.target.value)} />
    <Select label={t.loginLinks.validFor} value={expiry} onValueChange={setExpiry} options={[{ value: '900', label: t.loginLinks.minutes15 }, { value: '3600', label: t.loginLinks.hour1 }, { value: '86400', label: t.loginLinks.hours24 }]} />
    <Select label={t.loginLinks.destination} value={destination} onValueChange={setTarget} options={[{ value: '/assistants', label: t.nav.assistants }, { value: '/plans', label: t.nav.plans }, ...(principal?.User?.Administrator ? [{ value: '/resources', label: t.nav.resources }] : []), ...(pluginsVisible ? [{ value: '/extensions', label: locale === 'zh' ? '插件' : 'Plugins' }] : []), { value: '/account/theme', label: t.settings.theme }, { value: '/account/security', label: t.settings.security }]} />
    <p className="muted">{t.loginLinks.singleUse}</p>
  </EditDialog>{verification.dialog}</>;
}
function IssuedLoginLink({ url, link, onClose }: { url: string; link: LoginLink; onClose: () => void }) {
  const { t, locale } = useCopy(), input = useRef<HTMLInputElement>(null);
  const [copied, setCopied] = useState(false), [manual, setManual] = useState(false);
  return <Dialog open title={t.loginLinks.ready} onOpenChange={open => { if (!open) onClose(); }} footer={<Button onClick={onClose}>{t.close}</Button>}>
    <p>{t.loginLinks.singleUse}</p><p className="muted">{t.loginLinks.expires}: {new Date(link.ExpiresAt).toLocaleString(locale)}</p>
    <TextField ref={input} label={t.loginLinks.title} value={url} readOnly onFocus={event => event.target.select()} />
    <Button variant="secondary" onClick={() => { void navigator.clipboard?.writeText(url).then(() => setCopied(true)).catch(() => { setManual(true); input.current?.select(); }); if (!navigator.clipboard) { setManual(true); input.current?.select(); } }}><Copy />{copied ? t.loginLinks.copied : t.loginLinks.copy}</Button>
    {manual && <p role="status">{t.loginLinks.manualCopy}</p>}
  </Dialog>;
}
