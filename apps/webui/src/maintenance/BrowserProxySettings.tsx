import { useState } from 'react';
import { Button, Dialog, SegmentedControl, TextField, useModalSnapshot } from '@molr/ui';
import type { BrowserProxySettings as Settings } from '@molr/sdk';
import { api } from '@molr/sdk';
import { useCopy, useQuery } from '../state';
import { useErrorToast } from '../error-toasts';
import { useReauthentication } from '../interfaces/official/components/ReauthenticationDialog';
import { EditForm, Loading, useAction } from '../interfaces/official/components/common';

export function BrowserProxySettings({ onClose }: { onClose: () => void }) {
 const { locale } = useCopy(), zh = locale === 'zh';
 const query = useQuery<Settings>('/api/components/browser/settings'), action = useAction();
 useErrorToast(query.error, query.reload);
 const settings = useModalSnapshot(query.data);
 const title = zh ? '浏览器设置' : 'Browser settings';
 const description = zh ? '代理设置应用于本实例的浏览器，保存后下次启动时生效。' : 'Proxy settings apply to all browsers on this instance, starting with their next launch.';
 if (settings) return <ProxyForm initial={settings} title={title} description={description} action={action} onClose={onClose}/>;
 return <Dialog open title={title} description={description} onOpenChange={open => { if (!open && !action.busy) onClose(); }} dismissible={!action.busy} closeLabel={zh ? '关闭' : 'Close'}>
  {query.loading ? <Loading/> : <Button variant="secondary" onClick={() => void query.reload()}>{zh ? '重新加载' : 'Reload'}</Button>}
 </Dialog>;
}
function ProxyForm({ initial, title, description, action, onClose }: { initial: Settings; title: string; description: string; action: ReturnType<typeof useAction>; onClose: () => void }) {
 const { locale } = useCopy(), zh = locale === 'zh', verification = useReauthentication();
 const [mode, setMode] = useState(initial.Mode), [server, setServer] = useState(initial.Server), [bypass, setBypass] = useState(initial.Bypass.join(', '));
 const [authenticate, setAuthenticate] = useState(initial.Authentication), [username, setUsername] = useState(initial.Username), [password, setPassword] = useState('');
 const socks = server.trim().startsWith('socks5://');
 const credentialsChanged = server.trim() !== initial.Server || username !== initial.Username;
 async function save() {
   const proof = await verification.verify('credential.put');
   const auth = mode === 'proxy' && authenticate && !socks;
   await api.put<Settings>('/api/components/browser/settings', {
    Mode: mode, Server: mode === 'proxy' ? server.trim() : '', Bypass: mode === 'proxy' ? bypass.split(/[,\n]/).map(v => v.trim()).filter(Boolean) : [],
    Username: auth ? username : '', Password: auth ? password : '', PasswordAction: !auth ? 'clear' : password ? 'replace' : 'keep', Version: initial.Version, Verification: proof,
   });
   setPassword('');
 }
 return <><Dialog open title={title} description={description} onOpenChange={open => { if (!open && !action.busy) onClose(); }} dismissible={!action.busy} closeLabel={zh ? '关闭' : 'Close'}><EditForm action={action} onClose={onClose} onSubmit={save}>
  <fieldset className="browser-proxy-form" disabled={action.busy}>
   <SegmentedControl label={zh ? '连接方式' : 'Connection mode'} value={mode} onValueChange={v => setMode(v as Settings['Mode'])} options={[{value:'direct',label:zh?'直连':'Direct'},{value:'proxy',label:zh?'使用代理':'Use proxy'}]}/>
   {mode === 'proxy' && <>
    <TextField label={zh ? '代理服务器' : 'Proxy server'} value={server} onChange={e => setServer(e.target.value)} placeholder="http://host.molr.internal:7890" required maxLength={2048}/>
    <p className="muted">{zh ? '支持 HTTP 和无认证 SOCKS5。访问宿主代理使用 host.molr.internal，仍需为助理授权对应网络地址和端口。' : 'Supports HTTP and unauthenticated SOCKS5. Use host.molr.internal for a host proxy and grant the assistant access to its address and port.'}</p>
    {!socks && <SegmentedControl label={zh ? '代理认证' : 'Proxy authentication'} value={authenticate?'password':'none'} onValueChange={v => setAuthenticate(v === 'password')} options={[{value:'none',label:zh?'无':'None'},{value:'password',label:zh?'用户名和密码':'Username and password'}]}/>}
    {!socks && authenticate && <div className="browser-proxy-credentials">
     <TextField label={zh?'用户名':'Username'} value={username} onChange={e=>setUsername(e.target.value)} required maxLength={256} autoComplete="off"/>
     <TextField label={zh?'密码':'Password'} type="password" value={password} onChange={e=>setPassword(e.target.value)} required={!initial.PasswordConfigured || credentialsChanged} maxLength={1024} autoComplete="new-password" placeholder={initial.PasswordConfigured && !credentialsChanged ? (zh?'已配置，留空保留':'Configured; leave blank to keep') : undefined}/>
    </div>}
    <TextField label={zh?'绕过代理的地址':'Proxy bypass addresses'} value={bypass} onChange={e=>setBypass(e.target.value)} placeholder="*.example.test, 192.0.2.0/24" maxLength={16384}/>
    <p className="muted">{zh?'多个地址用逗号分隔。绕过代理的连接仍按目标地址检查沙盒网络权限；通过代理访问的网站由代理控制。':'Separate entries with commas. Direct connections still require destination network grants; sites reached through the proxy are controlled by the proxy.'}</p>
   </>}
  </fieldset>
 </EditForm></Dialog>{verification.dialog}</>;
}
