import { useErrorToast, useErrorToasts } from '../../../error-toasts';
import { useEffect, useState } from 'react';
import { Alert, Button, Logo, Tabs, TextField, useTheme } from '@molr/ui';
import { APIError } from '@molr/sdk';
import { useCopy, useSession } from '../../../state';
import { finishLoginLink } from '../../../login-link';
import type { LoginLinkAttempt } from '../../../login-link';
import { LocalQRCode } from './ChannelQRCode';

export function LoginLinkLogin({ attempt }: { attempt: LoginLinkAttempt }) {
  const { t, locale } = useCopy(), { theme } = useTheme(), session = useSession(), notices = useErrorToasts();
  const [confirmed, setConfirmed] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>(null);
  const [expires, setExpires] = useState('');
  const [mode, setMode] = useState<'device' | 'qr'>('device'), [expired, setExpired] = useState(false);
  const [challenge, setChallenge] = useState(''), [code, setCode] = useState(''), [target, setTarget] = useState('/assistants');
  useEffect(() => {
    if (attempt.invalid) return;
    let active = true;
    void attempt.automatic().then(async automatic => {
      if (automatic) {
        if (active) { setBusy(true); setConfirmed(true); }
      } else {
        const result = await attempt.preview();
        if (active) setExpires(result.expires_at);
      }
    }).catch(error => { if (active) setError(error); });
    return () => { active = false; };
  }, [attempt]);
  useEffect(() => {
    if (!expires || confirmed) return;
    let timer: number | undefined;
    function syncExpiry() {
      window.clearTimeout(timer);
      const remaining = Date.parse(expires) - Date.now();
      setExpired(remaining <= 0);
      if (remaining > 0 && document.visibilityState !== 'hidden') timer = window.setTimeout(syncExpiry, remaining);
    }
    syncExpiry();
    document.addEventListener('visibilitychange', syncExpiry);
    return () => { window.clearTimeout(timer); document.removeEventListener('visibilitychange', syncExpiry); };
  }, [expires, confirmed]);
  useEffect(() => {
    if (attempt.invalid || !confirmed) return;
    let active = true;
    void attempt.redeem().then(async ({ result }) => {
      if (!active) return;
      setTarget(result.target_path);
      if (result.challenge) { setChallenge(result.challenge); setBusy(false); return; }
      await session.reload();
      if (active) finishLoginLink(result.target_path);
    }).catch(error => { if (active) { setError(error); setBusy(false); } });
    return () => { active = false; };
  // A captured attempt owns its single request; session rerenders must not start another exchange.
  }, [attempt, confirmed]);
  const invalid = attempt.invalid || expired || (!challenge && error instanceof APIError && error.status === 401);
  useErrorToast(!busy && !invalid ? error : null);
  return <main className="login-page"><section className="login-card">
    <Logo animation="daily" interactive variant={theme === 'dark' ? 'light' : 'dark'} />
    {!invalid && !error && !confirmed && (expires ? <Tabs className="login-link-modes" label={t.loginLinks.loginMethod} value={mode} onValueChange={setMode} items={[
      { value: 'device', label: t.loginLinks.thisDevice, content: <div className="login-link-mode-content"><p>{t.loginLinks.confirmHelp(new Date(expires).toLocaleString(locale))}</p><Button onClick={() => {
        if (Date.now() >= Date.parse(expires)) { setExpired(true); return; }
        notices?.rearm(); setBusy(true); setConfirmed(true);
      }}>{t.loginLinks.confirm}</Button></div> },
      { value: 'qr', label: t.loginLinks.qrMode, content: mode === 'qr' ? <div className="login-link-mode-content"><LocalQRCode value={attempt.qrURL()} label={t.loginLinks.qrCode} /><p>{t.loginLinks.qrHelp}</p><p className="muted">{t.loginLinks.expires}: <time dateTime={expires}>{new Date(expires).toLocaleString(locale)}</time></p></div> : null },
    ]} /> : <p role="status">{t.loading}</p>)}
    {busy ? <p role="status">{t.loginLinks.signingIn}</p> : invalid ? <Alert tone="warning" title={t.loginLinks.invalid} /> : error && !challenge ? <><p className="muted">{t.loginLinks.interrupted}</p></> : null}
    {challenge && <form className="editor" onSubmit={event => {
      event.preventDefault(); notices?.rearm(); setBusy(true); setError(null);
      void attempt.complete(code).then(async () => { await session.reload(); finishLoginLink(target); }).catch(setError).finally(() => { setBusy(false); setCode(''); });
    }}><p>{t.loginLinks.mfa}</p><TextField label={t.mfa} value={code} autoFocus name="verification-code" autoComplete="one-time-code" required disabled={busy} onChange={event => setCode(event.target.value)} /><Button type="submit" loading={busy}>{t.verify}</Button></form>}
    {!busy && (mode === 'device' || invalid || !!error) && <Button variant="secondary" onClick={() => finishLoginLink()}>{t.loginLinks.passwordLogin}</Button>}
  </section></main>;
}
