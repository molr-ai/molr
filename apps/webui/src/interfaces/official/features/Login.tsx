import { useErrorToast, useErrorToasts } from '../../../error-toasts';
import { useEffect, useState } from 'react';
import { Button, Checkbox, Logo, TextField, useTheme } from '@molr/ui';
import { BrowserLogin } from '../../../auth-device';
import { APIError } from '@molr/sdk';
import { useCopy, useSession } from '../../../state';

export function Login() {
  const { t } = useCopy(), session = useSession(), { theme } = useTheme(), notices = useErrorToasts();
  const [username, setUsername] = useState(''), [password, setPassword] = useState(''), [code, setCode] = useState('');
  const [rememberMe, setRememberMe] = useState(true);
  const [challenge, setChallenge] = useState('');
  const [login] = useState(() => new BrowserLogin());
  useEffect(() => () => { void login.close(); }, [login]);
  const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>(null);
  useErrorToast(error);
  async function submit() {
    notices?.rearm();
    setBusy(true); setError(null);
    try {
      if (challenge) await login.complete(code);
      else {
        const result = await login.start('/api/auth/login', { username, password, remember_me: rememberMe });
        setPassword('');
        if (result.challenge) { setChallenge(result.challenge); return; }
      }
      await session.reload();
    } catch (error) { if (error instanceof APIError && error.code === 'login_interrupted') { setChallenge(''); setCode(''); } setError(error); } finally { setBusy(false); }
  }
  return <main className="login-page"><section className="login-card"><Logo animation="daily" interactive variant={theme === 'dark' ? 'light' : 'dark'} /><p>{t.subtitle}</p>
    <form className="editor" onSubmit={event => { event.preventDefault(); void submit(); }}>
      {challenge ? <TextField label={t.mfa} autoFocus name="verification-code" autoComplete="one-time-code" value={code} onChange={event => setCode(event.target.value)} required maxLength={128} /> : <><TextField label={t.username} name="username" autoComplete="username" value={username} onChange={event => setUsername(event.target.value)} required maxLength={128} /><TextField label={t.password} type="password" name="password" autoComplete="current-password" value={password} onChange={event => setPassword(event.target.value)} required /></>}
      {!challenge && <Checkbox label={t.keepSignedIn} checked={rememberMe} disabled={busy} onChange={event => setRememberMe(event.target.checked)} />}
      <Button type="submit" loading={busy}>{challenge ? t.verify : t.signIn}</Button>
      {challenge && <Button variant="ghost" disabled={busy} onClick={() => { void login.close(); setChallenge(''); setCode(''); setError(null); }}>{t.back}</Button>}
    </form>
  </section></main>;
}
