import { useErrorToast } from '../../../error-toasts';
import { useRef, useState } from 'react';
import { Alert, Badge, Button, SecretField, TextField } from '@molr/ui';
import type { SandboxDiagnostic, SandboxView } from '../../../sandbox-types';
import { useCopy } from '../../../state';
import { useSandboxCopy } from '../../../sandbox-copy';
import { api, mutationKey } from '../../../api/client';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { EditDialog, useAction } from '../components/common';

export function connectionError(copy: ReturnType<typeof useSandboxCopy>, reason?: string) {
  return copy.connectionErrors[reason as keyof typeof copy.connectionErrors] ?? copy.connectionErrors.sandbox_unreachable;
}
export function SandboxConnection({ sandbox, onClose, onConnected }: { sandbox?: SandboxView; onClose: () => void; onConnected: (id: string) => void }) {
  const { t } = useCopy(), copy = useSandboxCopy(), verification = useReauthentication(), test = useAction();
  const [name, setName] = useState(sandbox?.Name ?? ''), [endpoint, setEndpoint] = useState(sandbox?.Endpoint ?? ''), [connectionCode, setConnectionCode] = useState('');
  const [diagnostic, setDiagnostic] = useState<SandboxDiagnostic | null>(null), [key] = useState(mutationKey);
  const generation = useRef(0);
  const [showDiagnostic, setShowDiagnostic] = useState(true);
  const parameters = () => ({ Endpoint: endpoint.trim(), ConnectionCode: connectionCode.trim() });
  function update(set: (value: string) => void, value: string) { generation.current++; set(value); setDiagnostic(null); test.reset(); }
  const fieldError = (field: string) => diagnostic && !diagnostic.Connected && diagnostic.Field === field ? connectionError(copy, diagnostic.Reason) : undefined;
  useErrorToast(test.error);
  return <><EditDialog title={sandbox ? copy.credentials : copy.connect} description={sandbox ? copy.connectionCodeHelp : copy.connectHelp} onClose={onClose} submitLabel={sandbox ? t.save : copy.connect} submitDisabled={!diagnostic?.Connected || test.busy} onSubmit={async () => {
    const proof = await verification.verify('credential.put');
    const result = await api.post<SandboxView>('/api/sandboxes/connect', { ID: sandbox?.ID, Name: name.trim(), ...parameters(), Verification: proof, Mutation: { IdempotencyKey: key, ExpectedVersion: sandbox?.Version ?? 0 } });
    onConnected(result.ID);
  }}>
    {!sandbox && <TextField label={t.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} />}
    <TextField label={copy.endpoint} description={copy.endpointHelp} error={fieldError('Endpoint')} value={endpoint} onChange={event => update(setEndpoint, event.target.value)} type="url" required placeholder="https://sandbox.example.com:8443" disabled={!!sandbox} />
    <SecretField name={`sandbox-${sandbox?.ID ?? 'new'}-connection-code`} label={copy.connectionCode} description={copy.connectionCodeHelp} error={fieldError('ConnectionCode')} value={connectionCode} onChange={event => update(setConnectionCode, event.target.value)} required maxLength={8192} />
    <div className="sandbox-connection-test"><Button variant="secondary" loading={test.busy} disabled={!endpoint.trim() || !connectionCode.trim()} onClick={() => void test.run(async () => { const revision = generation.current; const result = await api.post<SandboxDiagnostic>('/api/sandboxes/test', parameters()); if (revision === generation.current) { setDiagnostic(result); setShowDiagnostic(true); } }, undefined, false)}>{copy.test}</Button><span className="muted">{!diagnostic && copy.testFirst}</span></div>
    {diagnostic && showDiagnostic && (diagnostic.Connected && diagnostic.Info ? <Alert tone="positive" title={copy.testSuccess} onDismiss={() => setShowDiagnostic(false)}><div className="sandbox-status"><Badge>{diagnostic.Info.Capabilities.OS}</Badge><Badge tone={diagnostic.Info.Enabled && diagnostic.Info.Storage ? 'positive' : 'warning'}>{diagnostic.Info.Enabled && diagnostic.Info.Storage ? copy.storageReady : copy.storageUnavailable}</Badge><Badge tone={diagnostic.Info.Enabled && (diagnostic.Info.Capabilities.Shell || diagnostic.Info.Capabilities.FullAccess) ? 'positive' : 'warning'}>{diagnostic.Info.Enabled && (diagnostic.Info.Capabilities.Shell || diagnostic.Info.Capabilities.FullAccess) ? copy.computeReady : copy.computeUnavailable}</Badge></div></Alert> : <Alert tone="danger" title={connectionError(copy, diagnostic.Reason)} onDismiss={() => setShowDiagnostic(false)} />)}
  </EditDialog>{verification.dialog}</>;
}
