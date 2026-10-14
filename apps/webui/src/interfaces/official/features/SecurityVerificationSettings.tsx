import { useErrorToast } from '../../../error-toasts';
import { useState } from 'react';
import { ModalPresence, Alert, Badge, Button, Dialog, Textarea, TextField } from '@molr/ui';
import { api, APIError } from '../../../api/client';
import type { SecurityStatus } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { EditDialog, Loading, useAction } from '../components/common';
import { SettingsHelp, SettingsSwitch } from '../components/SettingsHelp';

type Enrollment = { id: string; provisioning_uri: string; expires_at: string };

export function SecurityVerificationSettings({ onChangePassword }: { onChangePassword: () => void }) {
  const { t } = useCopy(), query = useQuery<SecurityStatus>('/api/auth/security'), action = useAction(), verification = useReauthentication();
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null), [codes, setCodes] = useState<string[] | null>(null);
  const status = query.data;
  async function change(enabled: boolean) {
    const proof = await verification.verify('security.update');
    // A recovery code consumes an account revision during reauthentication.
    const current = await api.get<SecurityStatus>('/api/auth/security');
    if (current.ReauthenticationEnabled !== status?.ReauthenticationEnabled) throw new APIError('conflict');
    await api.post('/api/auth/security', { enabled, expected_version: current.Version, verification: proof });
    query.reload();
  }
  useErrorToast(query.error || action.error, query.reload);
  return <section className="security-verification settings-group" aria-label={t.verification.settingsTitle}>
    <header className="security-section-heading"><h3 className="settings-group-title">{t.verification.settingsTitle}</h3></header>
    {!status ? query.loading && <Loading /> : <>
      <div className="settings-list">
        <div className="settings-row verification-method-row"><span className="settings-label">{t.password}</span><Button size="sm" variant="secondary" onClick={onChangePassword}>{t.settings.changePassword}</Button></div>
        <div className="settings-row verification-method-row"><div className="security-method-label"><span className="settings-label">{t.verification.authenticator}<SettingsHelp label={t.verification.authenticator} content={t.verification.authenticatorHelp} /></span><Badge tone={status.MFAEnabled ? 'positive' : 'neutral'}>{status.MFAEnabled ? t.verification.statusEnabled : t.verification.statusDisabled}</Badge></div><div className="form-actions">
          <Button size="sm" variant="secondary" disabled={action.busy} onClick={() => void action.run(async () => {
            if (status.MFAEnabled) {
              const proof = await verification.verify('mfa.disable');
              await api.post('/api/auth/mfa/disable', { verification: proof });
            } else {
              const proof = await verification.verify('mfa.enroll');
              setEnrollment(await api.post<Enrollment>('/api/auth/mfa/enroll', { verification: proof }));
            }
            query.reload();
          }, undefined, false)}>{status.MFAEnabled ? t.verification.unbind : t.verification.bind}</Button>
        </div></div>
        <SettingsSwitch label={t.verification.sensitiveOperations} help={t.verification.sensitiveHelp} checked={status.ReauthenticationEnabled} disabled={action.busy} onChange={event => void action.run(() => change(event.target.checked), undefined, false)} />
      </div>
    </>}
    {verification.dialog}
    <ModalPresence>{enrollment && <EnrollmentDialog enrollment={enrollment} onClose={() => setEnrollment(null)} onConfirmed={codes => { setEnrollment(null); setCodes(codes); query.reload(); }} />}</ModalPresence>
    <ModalPresence>{codes && <RecoveryCodesDialog codes={codes} onClose={() => setCodes(null)} />}</ModalPresence>
  </section>;
}

function EnrollmentDialog({ enrollment, onClose, onConfirmed }: { enrollment: Enrollment; onClose: () => void; onConfirmed: (codes: string[]) => void }) {
  const { t } = useCopy(), [code, setCode] = useState('');
  const secret = new URL(enrollment.provisioning_uri).searchParams.get('secret') ?? '';
  return <EditDialog title={t.verification.bind} submitLabel={t.verification.enableAuthenticator} onClose={onClose} onSubmit={async () => {
    try {
      const result = await api.post<{ recovery_codes: string[] }>('/api/auth/mfa/confirm', { enrollment_id: enrollment.id, code });
      onConfirmed(result.recovery_codes);
    } catch (error) {
      if (error instanceof APIError && error.status === 401) {
        await api.get('/api/auth/security');
        throw new APIError('verification_failed', error.requestID, error.status);
      }
      throw error;
    } finally { setCode(''); }
  }}><p className="muted">{t.verification.setupHelp}</p>
    <TextField label={t.verification.setupKey} readOnly value={secret} autoComplete="off" />
    <TextField label={t.verification.code} inputMode="numeric" name="verification-code" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} required value={code} onChange={event => setCode(event.target.value)} />
  </EditDialog>;
}

function RecoveryCodesDialog({ codes, onClose }: { codes: string[]; onClose: () => void }) {
  const { t } = useCopy();
  function download() {
    const url = URL.createObjectURL(new Blob([codes.join('\n') + '\n'], { type: 'text/plain;charset=utf-8' }));
    const link = document.createElement('a'); link.href = url; link.download = 'molr-recovery-codes.txt'; link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  return <Dialog open title={t.verification.recoveryCodes} onOpenChange={open => { if (!open) onClose(); }} footer={<>
    <Button variant="secondary" onClick={download}>{t.verification.downloadCodes}</Button><Button onClick={onClose}>{t.verification.codesSaved}</Button>
  </>}><Alert tone="warning" title={t.verification.keepCodes}>{t.verification.recoveryHelp}</Alert>
    <Textarea label={t.verification.recoveryCodes} readOnly value={codes.join('\n')} rows={6} />
  </Dialog>;
}
