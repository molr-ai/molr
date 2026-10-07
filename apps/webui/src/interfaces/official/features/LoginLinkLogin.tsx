import { useEffect, useState } from 'react';
import { Alert, Button, Logo, TextField, useTheme } from '@molr/ui';
import { api, APIError } from '../../../api/client';
import { useCopy, useSession } from '../../../state';
import { finishLoginLink } from '../../../login-link';
import type { LoginLinkAttempt } from '../../../login-link';
import { ErrorNotice } from '../components/common';

export function LoginLinkLogin({ attempt }: { attempt: LoginLinkAttempt }) {
  const { t, locale } = useCopy(), { theme } = useTheme(), session = useSession();
  const [confirmed, setConfirmed] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>(null);
  const [expires, setExpires] = useState('');
  const [challenge, setChallenge] = useState(''), [csrf, setCSRF] = useState(''), [code, setCode] = useState(''), [target, setTarget] = useState('/assistants');
  useEffect(() => {
    if (attempt.invalid) return;
    let active = true;
    void attempt.preview().then(result => { if (active) setExpires(result.expires_at); }).catch(error => { if (active) setError(error); });
    return () => { active = false; };
  }, [attempt]);
  useEffect(() => {
    if (attempt.invalid || !confirmed) return;
    let active = true;
    void attempt.redeem().then(async ({ result, csrf }) => {
      if (!active) return;
      setTarget(result.target_path);
      if (result.challenge) { setChallenge(result.challenge); setCSRF(csrf); setBusy(false); return; }
      await session.reload();
      if (active) finishLoginLink(result.target_path);
    }).catch(error => { if (active) { setError(error); setBusy(false); } });
    return () => { active = false; };
  // A captured attempt owns its single request; session rerenders must not start another exchange.
  }, [attempt, confirmed]);
  const invalid = attempt.invalid || (!challenge && error instanceof APIError && error.status === 401);
  return <main className="login-page"><section className="login-card">
    <Logo animation="daily" interactive variant={theme === 'dark' ? 'light' : 'dark'} />
    <h1>{t.loginLinks.signIn}</h1>
    {!invalid && !error && !confirmed && (expires ? <>
      <p>{t.loginLinks.confirmHelp(new Date(expires).toLocaleString(locale))}</p>
      <Button onClick={() => { setBusy(true); setConfirmed(true); }}>{t.loginLinks.confirm}</Button>
    </> : <p role="status">{t.loading}</p>)}
    {busy ? <p role="status">{t.loginLinks.signingIn}</p> : invalid ? <Alert tone="warning" title={t.loginLinks.invalid} /> : error && !challenge ? <><ErrorNotice error={error} /><p className="muted">{t.loginLinks.interrupted}</p></> : null}
    {challenge && <form className="editor" onSubmit={event => {
      event.preventDefault(); setBusy(true); setError(null);
      void api.post('/api/auth/mfa/complete', { challenge, code, device_name: 'Molr Web · Login link' }, csrf).then(async () => { await session.reload(); finishLoginLink(target); }).catch(error => setError(error instanceof APIError && error.status === 401 ? new APIError('verification_failed', error.requestID, error.status) : error)).finally(() => { setBusy(false); setCode(''); });
    }}><p>{t.loginLinks.mfa}</p><TextField label={t.mfa} value={code} autoFocus name="verification-code" autoComplete="one-time-code" required disabled={busy} onChange={event => setCode(event.target.value)} /><ErrorNotice error={error} /><Button type="submit" loading={busy}>{t.verify}</Button></form>}
    {!busy && <Button variant="secondary" onClick={() => finishLoginLink()}>{t.loginLinks.passwordLogin}</Button>}
  </section></main>;
}
