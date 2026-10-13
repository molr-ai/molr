import { useErrorToast } from '../../../error-toasts';
import { useEffect, useId, useRef, useState } from 'react';
import { ModalPresence, Button, Dialog, TextField } from '@molr/ui';
import type { SecurityStatus } from '../../../api/types';
import { api, APIError } from '../../../api/client';
import { useCopy } from '../../../state';
import { ActionCancelled, Loading, useAction } from './common';

type Request = { purpose: string; resolve: (proof: string) => void; reject: (error: Error) => void };

export function useReauthentication() {
  const [request, setRequest] = useState<Request | null>(null);
  const pending = useRef<Request | null>(null);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; pending.current?.reject(new ActionCancelled()); pending.current = null; }; }, []);
  async function verify(purpose: string) {
    const status = await api.get<SecurityStatus>('/api/auth/security');
    if (!mounted.current) throw new ActionCancelled();
    if (status.UnprotectedPurposes.includes(purpose)) return '';
    return new Promise<string>((resolve, reject) => {
      pending.current?.reject(new ActionCancelled());
      pending.current = { purpose, resolve, reject };
      setRequest(pending.current);
    });
  }
  function finish(proof?: string) {
    if (proof) pending.current?.resolve(proof);
    else pending.current?.reject(new ActionCancelled());
    pending.current = null;
    setRequest(null);
  }
  return { verify, dialog: <ModalPresence>{request && <ReauthenticationDialog purpose={request.purpose} onVerified={finish} onClose={() => finish()} />}</ModalPresence> };
}

function ReauthenticationDialog({ purpose, onVerified, onClose }: { purpose: string; onVerified: (proof: string) => void; onClose: () => void }) {
  const { t } = useCopy(), action = useAction(), formID = useId();
  const [password, setPassword] = useState(''), [code, setCode] = useState('');
  const [status, setStatus] = useState<SecurityStatus | null>(null), [error, setError] = useState<unknown>(null), [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setStatus(null); setError(null);
    void api.get<SecurityStatus>('/api/auth/security', controller.signal).then(setStatus).catch(error => { if (!controller.signal.aborted) setError(error); });
    return () => controller.abort();
  }, [attempt]);
  useErrorToast(error, () => setAttempt(value => value + 1));
  useErrorToast(action.error);
  return <Dialog open title={t.verification.title} description={t.verification.help} dismissible={!action.busy}
    onOpenChange={open => { if (!open && !action.busy) onClose(); }}
    footer={<><Button variant="secondary" disabled={action.busy} onClick={onClose}>{t.cancel}</Button><Button type="submit" form={formID} disabled={!status} loading={action.busy}>{t.verify}</Button></>}>
    {!status ? !error && <Loading /> : <form id={formID} className="editor" onSubmit={event => {
      event.preventDefault();
      void action.run(async () => {
        try {
          const proof = await api.post<{ verification: string }>('/api/auth/reauthenticate', { password, mfa_code: status.MFAEnabled ? code : '', purpose });
          onVerified(proof.verification);
        } catch (error) {
          if (error instanceof APIError && error.status === 401) {
            // A rejected password must not sign out a valid session. A separate
            // authenticated read still handles genuinely expired sessions.
            setStatus(await api.get<SecurityStatus>('/api/auth/security'));
            throw new APIError('verification_failed', error.requestID, error.status);
          }
          throw error;
        } finally { setPassword(''); setCode(''); }
      }, undefined, false);
    }}>
      <TextField label={t.verification.password} type="password" name="password" autoComplete="current-password" autoFocus value={password} onChange={event => setPassword(event.target.value)} required disabled={action.busy} />
      {status.MFAEnabled && <TextField label={t.mfa} name="verification-code" autoComplete="one-time-code" value={code} onChange={event => setCode(event.target.value)} required disabled={action.busy} />}
    </form>}
  </Dialog>;
}
