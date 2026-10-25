import { mutationKey } from '@molr/sdk';
import { useMutation } from '../../../use-mutation';
import { useEffect, useState } from 'react';
import { Alert, Button, TextField, useToast } from '@molr/ui';
import { api, APIError } from '@molr/sdk';
import type { Resource } from '@molr/sdk';
import { useCopy } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { useAction } from '../components/common';

export type ChatGPTConnectionInfo = { ClientID: string; Subject: string; Email: string };
type Connection = { Resource: Resource; ChatGPT?: ChatGPTConnectionInfo; CredentialID: string };
export function ChatGPTConnection({ initial, name, onConnected, onClose }: { initial?: Connection; name: string; onConnected: (resource: Resource) => void; onClose: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', action = useAction(), verification = useReauthentication(), { toast } = useToast();
  const [connection, setConnection] = useState(initial), [label, setLabel] = useState(name || 'OpenAI · ChatGPT');
  const native = window.molrApp?.oauth;
  useErrorToast(action.error);
  useEffect(() => () => { void native?.cancel().catch(() => {}); }, [native]);
  async function connect() {
    if (!native) return;
    const proof = await verification.verify('credential.put');
    const result = await native.authorize('chatgpt', { name: label, verification: proof, ...(connection ? { connectionId: connection.Resource.ID, expectedVersion: connection.Resource.Version } : {}) });
    let current = connection;
    if (result.connectionId) {
      current = await api.get<Connection>(`/api/resources/${result.connectionId}/connection`);
      setConnection(current);
      onConnected(current.Resource);
    }
    if (result.status === 'cancelled') return;
    if (result.status !== 'connected' || !current) throw new APIError(result.status === 'unknown' ? 'unavailable' : 'failed_precondition');
    try { await api.mutate('/api/resources/refresh-models', { ID: current.Resource.ID }, { idempotencyKey: mutationKey(), expectedVersion: current.Resource.Version }); }
    catch { toast({ title: zh ? '已连接，请在资源列表中刷新模型' : 'Connected. Refresh models in Resources.', tone: 'neutral' }); }
    onClose();
  }
  const disconnectChatGPT = useMutation([connection?.Resource.ID]);
  async function disconnect() {
    if (!connection) return;
    const proof = await verification.verify('credential.delete');
    const result = await disconnectChatGPT<{ Revoked: boolean }>('/api/chatgpt/disconnect', { ID: connection.Resource.ID, verification: proof }, connection.Resource.Version);
    setConnection(await api.get<Connection>(`/api/resources/${connection.Resource.ID}/connection`));
    toast({ title: zh ? '已断开 ChatGPT 连接' : 'ChatGPT disconnected', ...(!result.Revoked ? { description: zh ? '未能确认远端撤销，请在 ChatGPT 用量设置中检查并断开应用。' : 'Remote revocation was not confirmed. Disconnect this app in ChatGPT settings.', tone: 'neutral' as const } : {}) });
  }
  return <section className="provider-connect-fields" aria-label="ChatGPT">
    <p>{connection?.CredentialID ? `${zh ? '已连接' : 'Connected'} · ${connection.ChatGPT?.Email || connection.Resource.Name}` : zh ? '连接 ChatGPT，使用账号的订阅套餐。' : 'Connect ChatGPT to use your plan.'}</p>
    {!connection && <TextField label={zh ? '连接名称' : 'Connection name'} value={label} maxLength={256} onChange={e => setLabel(e.target.value)} />}
    <p className="muted">{zh ? '调用仍受 Molr 预算与限流控制。金额按参考单价计算，用于内部预算，不代表 OpenAI 实际扣费或套餐剩余额度。' : 'Molr budgets and limits still apply. Reference prices measure internal budget usage, not OpenAI charges or remaining plan allowance.'}</p>
    {!native && <Alert title={zh ? '请在 Molr 桌面 App 中授权' : 'Authorize in the Molr desktop app'}>{zh ? '在 App 中连接此 Runtime 完成授权后，可以回到浏览器使用。' : 'Connect to this Runtime in the app to authorize, then continue in your browser.'}</Alert>}
    <div className="setup-actions">
      <Button type="button" disabled={!native || action.busy} loading={action.busy} onClick={() => void action.run(connect, undefined, false)}>Continue with ChatGPT</Button>
      {action.busy && <Button type="button" variant="secondary" onClick={() => void native?.cancel()}>{zh ? '取消授权' : 'Cancel authorization'}</Button>}
      {connection?.CredentialID && <Button type="button" variant="secondary" disabled={action.busy} onClick={() => void action.run(disconnect, undefined, false)}>{zh ? '断开连接' : 'Disconnect'}</Button>}
      <a href="https://chatgpt.com/settings/usage" target="_blank" rel="noopener noreferrer">{zh ? '管理套餐用量' : 'Manage usage'}</a>
    </div>
    {verification.dialog}
  </section>;
}
