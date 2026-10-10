import { useEffect, useRef, useState } from 'react';
import { Badge, Button, Dialog, Select, TextField } from '@molr/ui';
import { Copy, Link, Plus } from 'lucide-react';
import { api } from '../../../api/client';
import { useCopy, useQuery, useSession } from '../../../state';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { Confirm, EditDialog, ErrorNotice, Loading } from '../components/common';

type LoginLink = { ID: string; Name: string; TargetPath: string; CreatedAt: string; ExpiresAt: string; UsedAt: string | null; RevokedAt: string | null };
type IssuedLink = { link: LoginLink; value: string };

export function LoginLinksSettings() {
  const { t, locale } = useCopy(), query = useQuery<LoginLink[]>('/api/auth/login-links', true);
  const [creating, setCreating] = useState(false), [issued, setIssued] = useState<{ url: string; link: LoginLink } | null>(null), [revoking, setRevoking] = useState<LoginLink | null>(null);
  const [now, setNow] = useState(Date.now);
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
      }
      if (next < Infinity) timer = window.setTimeout(sync, Math.min(next - current, 2_147_483_647));
    }
    sync();
    document.addEventListener('visibilitychange', sync);
    return () => { window.clearTimeout(timer); document.removeEventListener('visibilitychange', sync); };
  }, [query.data]);
  return <section className="login-links-settings settings-group" aria-label={t.loginLinks.title}>
    <div className="record-heading"><div><h3 className="settings-group-title">{t.loginLinks.title}</h3><p className="muted">{t.loginLinks.help}</p></div><Button variant="secondary" onClick={() => setCreating(true)}><Plus />{t.loginLinks.create}</Button></div>
    <ErrorNotice error={query.error} retry={query.reload} />
    {!query.data && query.loading ? <Loading /> : !query.data?.length ? <p className="muted">{t.loginLinks.empty}</p> : <div className="settings-rows">{query.data.map(link => {
      const expired = new Date(link.ExpiresAt).getTime() <= now;
      const status = link.RevokedAt ? 'revoked' : link.UsedAt ? 'used' : expired ? 'expired' : 'pending';
      return <article className="settings-row-item" key={link.ID}><div className="settings-row-item-head"><h3><Link size={14} aria-hidden="true" /> {link.Name}</h3><Badge tone={status === 'pending' ? 'positive' : 'neutral'}>{t.loginLinks[status]}</Badge>{!link.RevokedAt && !expired && <span className="settings-row-item-actions"><Button variant="ghost" size="sm" onClick={() => setRevoking(link)}>{t.loginLinks.revoke}</Button></span>}</div><p className="muted">{t.loginLinks.expires}: {new Date(link.ExpiresAt).toLocaleString(locale)}</p></article>;
    })}</div>}
    {creating && <CreateLoginLink onClose={() => setCreating(false)} onIssued={result => {
      const url = new URL(import.meta.env.BASE_URL, location.origin);
      url.hash = `/login?${new URLSearchParams({ token: result.value })}`;
      setIssued({ url: url.href, link: result.link }); setCreating(false); query.reload();
    }} />}
    {issued && <IssuedLoginLink url={issued.url} link={issued.link} onClose={() => setIssued(null)} />}
    {revoking && <Confirm title={t.loginLinks.revoke} description={t.loginLinks.revokeHelp} onClose={() => setRevoking(null)} onConfirm={() => api.remove(`/api/auth/login-links/${revoking.ID}`)} />}
  </section>;
}
function CreateLoginLink({ onClose, onIssued }: { onClose: () => void; onIssued: (result: IssuedLink) => void }) {
  const { t, locale } = useCopy(), { principal } = useSession(), verification = useReauthentication();
  const [name, setName] = useState(''), [expiry, setExpiry] = useState('3600'), [target, setTarget] = useState('/assistants');
  return <><EditDialog notifyOnSave={false} title={t.loginLinks.create} onClose={onClose} onSubmit={async () => {
    const proof = await verification.verify('login_link.create');
    onIssued(await api.post<IssuedLink>('/api/auth/login-links', { name, expires_in: Number(expiry), target_path: target, verification: proof }));
  }}><TextField label={t.name} value={name} required maxLength={128} autoFocus onChange={event => setName(event.target.value)} />
    <Select label={t.loginLinks.validFor} value={expiry} onValueChange={setExpiry} options={[{ value: '900', label: t.loginLinks.minutes15 }, { value: '3600', label: t.loginLinks.hour1 }, { value: '86400', label: t.loginLinks.hours24 }]} />
    <Select label={t.loginLinks.destination} value={target} onValueChange={setTarget} options={[{ value: '/assistants', label: t.nav.assistants }, { value: '/plans', label: t.nav.plans }, ...(principal?.User?.Administrator ? [{ value: '/resources', label: t.nav.resources }] : []), { value: '/extensions', label: locale === 'zh' ? '插件' : 'Plugins' }, { value: '/account/theme', label: t.settings.theme }, { value: '/account/security', label: t.settings.security }]} />
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
