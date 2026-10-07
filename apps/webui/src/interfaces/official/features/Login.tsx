import { useState } from 'react';
import { Button, Logo, Select, TextField, useTheme } from '@molr/ui';
import { api } from '../../../api/client';
import { useCopy, useSession } from '../../../state';
import { ErrorNotice } from '../components/common';

export function Login() {
  const { t, locale, setLocale } = useCopy(), session = useSession(), { theme } = useTheme();
  const [username, setUsername] = useState(''), [password, setPassword] = useState(''), [code, setCode] = useState('');
  const [challenge, setChallenge] = useState(''), [csrf, setCSRF] = useState('');
  const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>(null);
  async function submit() {
    setBusy(true); setError(null);
    try {
      if (challenge) await api.post('/api/auth/mfa/complete', { challenge, code, device_name: 'Molr Web' }, csrf);
      else {
        const proof = await api.get<{ csrf_token: string }>('/api/auth/csrf'); setCSRF(proof.csrf_token);
        const result = await api.post<{ challenge?: string }>('/api/auth/login', { username, password, device_name: 'Molr Web' }, proof.csrf_token);
        setPassword('');
        if (result.challenge) { setChallenge(result.challenge); return; }
      }
      await session.reload();
    } catch (error) { setError(error); } finally { setBusy(false); }
  }
  return <main className="login-page"><section className="login-card"><Logo animation="daily" interactive variant={theme === 'dark' ? 'light' : 'dark'} /><h1>{t.welcome}</h1><p>{t.subtitle}</p>
    <form className="editor" onSubmit={event => { event.preventDefault(); void submit(); }}>
      {challenge ? <TextField label={t.mfa} autoFocus name="verification-code" autoComplete="one-time-code" value={code} onChange={event => setCode(event.target.value)} required maxLength={128} /> : <><TextField label={t.username} name="username" autoComplete="username" value={username} onChange={event => setUsername(event.target.value)} required maxLength={128} /><TextField label={t.password} type="password" name="password" autoComplete="current-password" value={password} onChange={event => setPassword(event.target.value)} required /></>}
      <ErrorNotice error={error} /><Button type="submit" loading={busy}>{challenge ? t.verify : t.signIn}</Button>
      {challenge && <Button variant="ghost" disabled={busy} onClick={() => { setChallenge(''); setCode(''); setError(null); }}>{t.back}</Button>}
    </form><small>{t.bootstrap}</small><Select label={t.settings.language} value={locale} onValueChange={setLocale} options={[{ value: 'zh', label: '简体中文' }, { value: 'en', label: 'English' }]} />
  </section></main>;
}
