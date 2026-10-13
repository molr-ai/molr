import type { BrowserStatus } from '@molr/sdk';
import { useCopy } from '../state';

export function BrowserNetworkStatus({ value }: { value?: BrowserStatus }) {
 const { locale } = useCopy(), zh = locale === 'zh', proxy = value?.Proxy;
 if (!proxy) return null;
 const reasons: Record<string, [string,string]> = {
  proxy_authentication_failed:['代理认证失败','Proxy authentication failed'],
  proxy_connection_failed:['代理连接失败','Proxy connection failed'],
  proxy_tunnel_failed:['代理隧道建立失败','Proxy tunnel failed'],
  network_certificate_failed:['网站证书校验失败','Website certificate verification failed'],
  network_tls_failed:['TLS 连接失败','TLS connection failed'],
  network_dns_failed:['域名解析失败','DNS lookup failed'],
  network_connection_refused:['连接被拒绝','Connection refused'],
  network_timeout:['连接超时','Connection timed out'],
  network_authorization_changed:['网络授权已撤销或变更','Network authorization was revoked or changed'],
  network_destination_denied:['沙盒拒绝了网络目标','Sandbox denied a network destination'],
  network_bytes_exhausted:['网络流量额度耗尽','Network traffic quota exhausted'],
  network_connection_limit:['网络连接数达到上限','Network connection limit reached'],
  network_dns_limit:['域名解析并发达到上限','DNS concurrency limit reached'],
  network_connection_failed:['网络连接失败','Network connection failed'],
  network_offline:['无法联网','Network is offline'],
 };
 const reason=(code: string)=>reasons[code]?.[zh?0:1] ?? (zh?'原因未确定':'Cause undetermined');
 const failure=value.LastNetworkFailure;
 return <details className="browser-network-status">
  <summary>{zh?'网络设置':'Network settings'} · {proxy.Mode==='proxy'?(zh?'代理':'Proxy'):(zh?'直连':'Direct')}{value.RestartRequired && (zh?' · 新设置待重启生效':' · Restart to apply new settings')}</summary>
  <p>{value.ProxyAppliesTo==='current_session'?(zh?'当前浏览器使用此配置。':'This configuration is active for the current browser.'):(zh?'下次启动浏览器将使用此配置。':'This configuration applies on the next browser start.')}</p>
  {proxy.Mode==='proxy' && <><p>{proxy.Server} · {proxy.Authentication?(zh?'已配置认证':'Authentication configured'):(zh?'无认证':'No authentication')}</p>{proxy.Bypass.length>0 && <p>{zh?'绕过代理：':'Bypass: '}{proxy.Bypass.join(', ')}</p>}</>}
  {failure && <p>{zh?'最近导航失败：':'Last navigation failure: '}{reason(failure.Reason)} · {new Date(failure.At).toLocaleString(locale)}{failure.Sandbox && <><br/>{zh?'同期沙盒事件（不一定是本次失败原因）：':'Sandbox event during the action (may be unrelated): '}{reason(failure.Sandbox.Reason)}</>}</p>}
 </details>;
}
